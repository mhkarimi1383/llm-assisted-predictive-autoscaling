// Kubernetes reads for the Data Acquisition layer.
//
// One lazily-initialized clientset serves Deployment, Pod, and
// pod-metrics reads. Configuration follows the usual precedence:
// in-cluster service account first, then KUBECONFIG / ~/.kube/config
// so the service also runs against a local kind cluster during
// development.
package dataacquisition

// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list
// +kubebuilder:rbac:groups="",resources=resourcequotas,verbs=get;list
// +kubebuilder:rbac:groups=metrics.k8s.io,resources=pods,verbs=get;list

import (
	"context"
	"fmt"
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsversioned "k8s.io/metrics/pkg/client/clientset/versioned"

	dataacpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/dataacquisition"
)

var (
	clientOnce sync.Once
	clientErr  error
	clientset  *kubernetes.Clientset
	metricsCLI *metricsversioned.Clientset
)

func kubernetesClients() (*kubernetes.Clientset, *metricsversioned.Clientset, error) {
	clientOnce.Do(func() {
		cfg, err := restConfig()
		if err != nil {
			clientErr = err
			return
		}
		clientset, err = kubernetes.NewForConfig(cfg)
		if err != nil {
			clientErr = fmt.Errorf("failed to build kubernetes clientset: %w", err)
			return
		}
		metricsCLI, err = metricsversioned.NewForConfig(cfg)
		if err != nil {
			clientErr = fmt.Errorf("failed to build metrics client: %w", err)
			return
		}
	})
	return clientset, metricsCLI, clientErr
}

func restConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules, &clientcmd.ConfigOverrides{}).ClientConfig()
}

// deploymentState reads the target Deployment and its pods.
func deploymentState(
	ctx context.Context,
	namespace, name string,
) (*appsv1.Deployment, *corev1.PodList, error) {
	kube, _, err := kubernetesClients()
	if err != nil {
		return nil, nil, err
	}

	deployment, err := kube.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read Deployment %s/%s: %w", namespace, name, err)
	}

	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid selector on Deployment %s/%s: %w", namespace, name, err)
	}

	pods, err := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list pods for Deployment %s/%s: %w", namespace, name, err)
	}
	return deployment, pods, nil
}

// podMetrics reads the Metrics API for the given pods. Returns nil
// when the metrics server is unavailable; callers record that as a
// metrics error rather than a snapshot failure.
func podMetrics(ctx context.Context, namespace string, podNames []string) *metricsv1beta1.PodMetricsList {
	_, metricsClient, err := kubernetesClients()
	if err != nil {
		return nil
	}
	list, err := metricsClient.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	// Filter by the requested pod names; the Metrics API has no
	// server-side label selector for pod names.
	if len(podNames) == 0 {
		return list
	}
	wanted := make(map[string]bool, len(podNames))
	for _, n := range podNames {
		wanted[n] = true
	}
	filtered := make([]metricsv1beta1.PodMetrics, 0, len(list.Items))
	for _, item := range list.Items {
		if wanted[item.Name] {
			filtered = append(filtered, item)
		}
	}
	list.Items = filtered
	return list
}

// clusterCapacity sums allocatable CPU and memory over ready,
// schedulable nodes (Algorithm 6 capacity checks). v1.1 skips
// taints/affinity; the estimate is conservative for dedicated nodes.
func clusterCapacity(ctx context.Context) *dataacpb.ClusterCapacity {
	kube, _, err := kubernetesClients()
	if err != nil {
		return nil
	}
	nodes, err := kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	cap := &dataacpb.ClusterCapacity{}
	for i := range nodes.Items {
		node := &nodes.Items[i]
		ready := false
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		if !ready || node.Spec.Unschedulable {
			continue
		}
		cap.NodeCount++
		cap.AllocatableCpuCores += node.Status.Allocatable.Cpu().AsApproximateFloat64()
		cap.AllocatableMemoryBytes += node.Status.Allocatable.Memory().AsApproximateFloat64()
	}
	if cap.NodeCount == 0 {
		return nil
	}
	return cap
}

// namespaceQuota finds the most restrictive pods quota in the
// namespace (Algorithm 6 quota check).
func namespaceQuota(ctx context.Context, namespace string) *dataacpb.QuotaUsage {
	kube, _, err := kubernetesClients()
	if err != nil {
		return nil
	}
	quotas, err := kube.CoreV1().ResourceQuotas(namespace).List(ctx, metav1.ListOptions{})
	if err != nil || len(quotas.Items) == 0 {
		return nil
	}
	usage := &dataacpb.QuotaUsage{}
	for i := range quotas.Items {
		q := &quotas.Items[i]
		if hard, ok := q.Spec.Hard[corev1.ResourcePods]; ok {
			max := int32(hard.AsApproximateFloat64())
			if !usage.HasQuota || (max > 0 && max < usage.MaxPods) || usage.MaxPods == 0 {
				usage.HasQuota = true
				usage.MaxPods = max
			}
		}
	}
	return usage
}

// podResourcesOf sums the declared container requests of the
// deployment's pod template (Algorithm 6 capacity basis).
func podResourcesOf(deployment *appsv1.Deployment) *dataacpb.PodResources {
	res := &dataacpb.PodResources{}
	for _, c := range deployment.Spec.Template.Spec.Containers {
		if cpu := c.Resources.Requests.Cpu(); cpu != nil {
			res.CpuCores += cpu.AsApproximateFloat64()
		}
		if mem := c.Resources.Requests.Memory(); mem != nil {
			res.MemoryBytes += mem.AsApproximateFloat64()
		}
	}
	return res
}
