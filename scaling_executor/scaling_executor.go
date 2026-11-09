// Scaling Executor - implements scaling decisions in Kubernetes
//
// ExecuteScaling submits one scale write to the target Deployment's
// Scale subresource (Algorithm 7). Dry-run records the checked step
// without submitting. Executed actions emit Kubernetes Events on the
// target for auditability. The executor owns no other services.
package scalingexecutor

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"google.golang.org/grpc"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	scalingexecutorpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/scalingexecutor"

	helpers "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/helpers"
)

// scalingExecutorServer implements the ScalingExecutorService service
type scalingExecutorServer struct {
	scalingexecutorpb.UnimplementedScalingExecutorServiceServer
	mu       sync.Mutex
	kube     kubernetes.Interface
	err      error
	kubeOnce sync.Once
}

// client lazily initializes the Kubernetes client (in-cluster config
// first, then kubeconfig, matching the Data Acquisition layer).
func (s *scalingExecutorServer) client() (kubernetes.Interface, error) {
	s.kubeOnce.Do(func() {
		cfg, err := rest.InClusterConfig()
		if err != nil {
			cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
				clientcmd.NewDefaultClientConfigLoadingRules(),
				&clientcmd.ConfigOverrides{}).ClientConfig()
			if err != nil {
				s.err = fmt.Errorf("no Kubernetes configuration available: %w", err)
				return
			}
		}
		s.kube, s.err = kubernetes.NewForConfig(cfg)
	})
	return s.kube, s.err
}

// Start initializes and starts the Scaling Executor.
func Start(ctx context.Context, listenPort int) error {
	server := &scalingExecutorServer{}
	return helpers.ServeGRPC(ctx, listenPort, "Scaling Executor", func(s *grpc.Server) {
		scalingexecutorpb.RegisterScalingExecutorServiceServer(s, server)
	})
}

// ExecuteScaling implements the ScalingExecutorService.ExecuteScaling
// method (Algorithm 7, v1: one checked step, dry-run aware).
func (s *scalingExecutorServer) ExecuteScaling(ctx context.Context, req *scalingexecutorpb.ScalingDecision) (*scalingexecutorpb.ScalingResult, error) {
	log.Printf("Received ExecuteScaling request: id=%s %s/%s/%s -> %d replicas (dry_run=%t)",
		req.GetRequestId(), req.GetResourceType(), req.GetNamespace(), req.GetResourceName(),
		req.GetReplicaCount(), req.GetDryRun())

	if req.GetResourceType() != "deployment" {
		return failureResult(fmt.Sprintf("unsupported resource type %q; only deployment is supported in v1", req.GetResourceType())), nil
	}
	if req.GetNamespace() == "" || req.GetResourceName() == "" {
		return failureResult("resource type, name, and namespace are required"), nil
	}
	if req.GetReplicaCount() < 0 {
		return failureResult("replica count must be non-negative"), nil
	}

	kube, err := s.client()
	if err != nil {
		return failureResult(err.Error()), nil
	}

	deployments := kube.AppsV1().Deployments(req.GetNamespace())

	// Read the observed current count and version condition
	// (Algorithm 7: submit with a version condition).
	deployment, err := deployments.Get(ctx, req.GetResourceName(), metav1.GetOptions{})
	if err != nil {
		return failureResult(fmt.Sprintf("failed to read target Deployment: %v", err)), nil
	}
	fromReplicas := derefInt32(deployment.Spec.Replicas)
	resourceVersion := deployment.ResourceVersion

	if req.GetDryRun() {
		log.Printf("Dry-run: would scale %s/%s from %d to %d (not submitted)",
			req.GetNamespace(), req.GetResourceName(), fromReplicas, req.GetReplicaCount())
		return &scalingexecutorpb.ScalingResult{
			Success: true,
			Message: fmt.Sprintf("dry-run: checked scale %s/%s/%s from %d to %d without submitting",
				req.GetResourceType(), req.GetNamespace(), req.GetResourceName(), fromReplicas, req.GetReplicaCount()),
			Timestamp:    time.Now().Unix(),
			FromReplicas: fromReplicas,
			ToReplicas:   req.GetReplicaCount(),
			DryRun:       true,
		}, nil
	}

	// Submit the scale write conditioned on the observed version
	// (Algorithm 7: version conflict -> held, fresh cycle).
	scale := scaleFor(deployment, req.GetReplicaCount())
	_, err = deployments.UpdateScale(ctx, req.GetResourceName(), scale, metav1.UpdateOptions{})
	if err != nil {
		if apierrors.IsConflict(err) {
			return failureResult(fmt.Sprintf(
				"version conflict on %s/%s (observed rv %s); request a fresh decision cycle",
				req.GetNamespace(), req.GetResourceName(), resourceVersion)), nil
		}
		return failureResult(fmt.Sprintf("scale submission failed: %v", err)), nil
	}

	log.Printf("Scaled %s/%s/%s from %d to %d replicas",
		req.GetResourceType(), req.GetNamespace(), req.GetResourceName(), fromReplicas, req.GetReplicaCount())
	s.recordEvent(ctx, kube, req, fromReplicas, req.GetReplicaCount())

	return &scalingexecutorpb.ScalingResult{
		Success: true,
		Message: fmt.Sprintf("scaled %s/%s/%s from %d to %d replicas",
			req.GetResourceType(), req.GetNamespace(), req.GetResourceName(), fromReplicas, req.GetReplicaCount()),
		Timestamp:    time.Now().Unix(),
		FromReplicas: fromReplicas,
		ToReplicas:   req.GetReplicaCount(),
		DryRun:       false,
	}, nil
}

// scaleFor builds the Scale object for the submission.
func scaleFor(deployment *appsv1.Deployment, replicas int32) *autoscalingv1.Scale {
	return &autoscalingv1.Scale{
		ObjectMeta: metav1.ObjectMeta{
			Name:            deployment.Name,
			Namespace:       deployment.Namespace,
			ResourceVersion: deployment.ResourceVersion,
		},
		Spec: autoscalingv1.ScaleSpec{Replicas: replicas},
	}
}

// recordEvent emits a K8s Event on the target Deployment; event
// failures are logged, never fatal to the scaling result.
func (s *scalingExecutorServer) recordEvent(
	ctx context.Context,
	kube kubernetes.Interface,
	req *scalingexecutorpb.ScalingDecision,
	from, to int32,
) {
	eventType := corev1.EventTypeNormal
	verb := "ScalingUp"
	if to < from {
		verb = "ScalingDown"
	}
	_, err := kube.CoreV1().Events(req.GetNamespace()).Create(ctx, &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "llm-autoscaler-",
			Namespace:    req.GetNamespace(),
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      "Deployment",
			Name:      req.GetResourceName(),
			Namespace: req.GetNamespace(),
		},
		Type:    eventType,
		Reason:  verb,
		Message: fmt.Sprintf("LLM autoscaler scaled replicas from %d to %d (request %s)", from, to, req.GetRequestId()),
		Source:  corev1.EventSource{Component: "llm-autoscaler"},
	}, metav1.CreateOptions{})
	if err != nil {
		log.Printf("failed to record scaling event: %v", err)
	}
}

// GetScalingStatus implements the ScalingExecutorService.GetScalingStatus method
func (s *scalingExecutorServer) GetScalingStatus(ctx context.Context, req *scalingexecutorpb.ScalingStatusRequest) (*scalingexecutorpb.ScalingStatusResponse, error) {
	log.Printf("Received GetScalingStatus request: %v", req)

	if req.GetResourceType() != "deployment" || req.GetNamespace() == "" || req.GetResourceName() == "" {
		return nil, fmt.Errorf("resource type (deployment), name, and namespace are required")
	}

	kube, err := s.client()
	if err != nil {
		return nil, err
	}
	deployment, err := kube.AppsV1().Deployments(req.GetNamespace()).Get(ctx, req.GetResourceName(), metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to read Deployment %s/%s: %w", req.GetNamespace(), req.GetResourceName(), err)
	}

	status := "Ready"
	if deployment.Generation > deployment.Status.ObservedGeneration {
		status = "Progressing"
	}
	return &scalingexecutorpb.ScalingStatusResponse{
		ResourceType:    "deployment",
		ResourceName:    deployment.Name,
		Namespace:       deployment.Namespace,
		CurrentReplicas: derefInt32(deployment.Spec.Replicas),
		DesiredReplicas: deployment.Status.ReadyReplicas,
		LastUpdated:     time.Now().Unix(),
		Status:          status,
	}, nil
}

// WatchScalingEvents implements the ScalingExecutorService
// .WatchScalingEvents method (server-streaming heartbeat in v1).
func (s *scalingExecutorServer) WatchScalingEvents(req *scalingexecutorpb.WatchScalingEventsRequest, stream scalingexecutorpb.ScalingExecutorService_WatchScalingEventsServer) error {
	log.Printf("Received WatchScalingEvents request: %v", req)

	ctx := stream.Context()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("WatchScalingEvents stream cancelled by client")
			return ctx.Err()
		case <-ticker.C:
			event := &scalingexecutorpb.ScalingEvent{
				ResourceType: "heartbeat",
				ResourceName: "scaling-executor",
				Namespace:    "default",
				Timestamp:    time.Now().Unix(),
				Type:         scalingexecutorpb.EventType_UNKNOWN,
				Message:      "Heartbeat from scaling executor",
			}
			if err := stream.Send(event); err != nil {
				log.Printf("Error sending scaling event: %v", err)
				return err
			}
		}
	}
}

// ListScalableResources implements the ScalingExecutorService
// .ListScalableResources method.
func (s *scalingExecutorServer) ListScalableResources(ctx context.Context, req *scalingexecutorpb.ListScalableResourcesRequest) (*scalingexecutorpb.ListScalableResourcesResponse, error) {
	log.Printf("Received ListScalableResources request: %v", req)

	kube, err := s.client()
	if err != nil {
		return nil, err
	}
	deployments, err := kube.AppsV1().Deployments(req.GetNamespace()).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list Deployments: %w", err)
	}

	resources := make([]*scalingexecutorpb.ScalableResource, 0, len(deployments.Items))
	for i := range deployments.Items {
		dep := &deployments.Items[i]
		resources = append(resources, &scalingexecutorpb.ScalableResource{
			ResourceType:    "deployment",
			ResourceName:    dep.Name,
			Namespace:       dep.Namespace,
			CurrentReplicas: derefInt32(dep.Spec.Replicas),
			MinReplicas:     1, // v1: scale-to-zero unsupported
			MaxReplicas:     0, // unknown to the executor; CR-level bounds govern
		})
	}
	return &scalingexecutorpb.ListScalableResourcesResponse{Resources: resources}, nil
}

func failureResult(message string) *scalingexecutorpb.ScalingResult {
	return &scalingexecutorpb.ScalingResult{
		Success:   false,
		Message:   message,
		Timestamp: time.Now().Unix(),
	}
}

func derefInt32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}
