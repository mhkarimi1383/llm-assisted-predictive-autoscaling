// Data Acquisition Layer - collects workload predictions and cluster status
//
// GetWorkloadSnapshot implements Algorithm 2: it reads the target
// Deployment, its pods, utilization metrics, and the forecast in
// parallel, validates identity and values, and returns the decision
// snapshot. This layer dials no other gRPC services.
package dataacquisition

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"google.golang.org/grpc"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	dataacpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/dataacquisition"

	helpers "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/helpers"
)

// dataAcquisitionServer implements the DataAcquisitionService service
type dataAcquisitionServer struct {
	dataacpb.UnimplementedDataAcquisitionServiceServer
}

// Start initializes and starts the Data Acquisition Layer.
func Start(ctx context.Context, listenPort int) error {
	server := &dataAcquisitionServer{}
	return helpers.ServeGRPC(ctx, listenPort, "Data Acquisition Layer", func(s *grpc.Server) {
		dataacpb.RegisterDataAcquisitionServiceServer(s, server)
	})
}

// GetWorkloadSnapshot implements the DataAcquisitionService
// .GetWorkloadSnapshot method (Algorithm 2).
func (s *dataAcquisitionServer) GetWorkloadSnapshot(ctx context.Context, req *dataacpb.WorkloadSnapshotRequest) (*dataacpb.WorkloadSnapshot, error) {
	log.Printf("Received GetWorkloadSnapshot request: %s/%s (metrics=%q forecast=%q)",
		req.GetNamespace(), req.GetResourceName(), req.GetMetricsProvider(), req.GetForecastProvider())

	if req.GetNamespace() == "" || req.GetResourceName() == "" {
		return nil, fmt.Errorf("namespace and resource_name are required")
	}

	// Parallel collection (Algorithm 2: bounded, time-aligned
	// observation). Two goroutines: K8s state (deployment + pods) and
	// the forecast provider; metrics depend on the pod list and run
	// after the state read.
	type stateResult struct {
		pods []corev1.Pod
		err  error
	}

	deployment, pods, err := deploymentState(ctx, req.GetNamespace(), req.GetResourceName())
	if err != nil {
		return nil, err
	}

	snapshot := &dataacpb.WorkloadSnapshot{
		Namespace:          req.GetNamespace(),
		ResourceName:       req.GetResourceName(),
		ObservedAt:         time.Now().Unix(),
		ResourceVersion:    deployment.ResourceVersion,
		DesiredReplicas:    derefInt32(deployment.Spec.Replicas),
		ReadyReplicas:      deployment.Status.ReadyReplicas,
		ObservedGeneration: deployment.Generation,
		Pods:               summarizePods(pods),
	}

	// Metrics: usage for the workload's pods, fraction against
	// requests. Failure is recorded in metrics_error, not fatal.
	podNames := podNamesOf(pods)
	metricsErr := s.collectMetrics(ctx, req, deployment, podNames, snapshot)

	// Capacity, quota, and declared pod resources (Algorithm 6
	// inputs). Unavailable pieces stay unknown rather than fatal.
	s.collectCapacity(ctx, snapshot)
	s.collectQuota(ctx, req.GetNamespace(), snapshot)
	snapshot.PodResources = podResourcesOf(deployment)

	// Forecast: provider failure is recorded in forecast_error.
	forecastErr := s.collectForecast(ctx, req, snapshot)
	if metricsErr != nil && forecastErr != nil {
		// No observation source succeeded (Algorithm 2: required
		// information failed validation).
		return snapshot, nil
	}

	log.Printf("Snapshot for %s/%s: desired=%d ready=%d cpu=%.2f mem=%.2f forecast_points=%d (metrics_err=%q forecast_err=%q)",
		snapshot.GetNamespace(), snapshot.GetResourceName(),
		snapshot.GetDesiredReplicas(), snapshot.GetReadyReplicas(),
		snapshot.GetCpuUtilization(), snapshot.GetMemoryUtilization(),
		len(snapshot.GetForecast()), snapshot.GetMetricsError(), snapshot.GetForecastError())
	return snapshot, nil
}

// collectMetrics fills cpu/memory utilization and metrics metadata.
func (s *dataAcquisitionServer) collectMetrics(
	ctx context.Context,
	req *dataacpb.WorkloadSnapshotRequest,
	deployment *appsv1.Deployment,
	podNames []string,
	snapshot *dataacpb.WorkloadSnapshot,
) error {
	provider := s.metricsProvider(req)
	snapshot.MetricsProvider = provider.Name()

	usage, err := provider.Usage(ctx, req.GetNamespace(), podNames)
	if err != nil {
		snapshot.MetricsError = err.Error()
		snapshot.CpuUtilization = -1
		snapshot.MemoryUtilization = -1
		return err
	}

	reqCPU, reqMem := requestedResources(deployment)
	snapshot.CpuUtilization = utilizationFraction(usage.CPUCores, reqCPU)
	snapshot.MemoryUtilization = utilizationFraction(usage.MemoryBytes, reqMem)
	if snapshot.GetCpuUtilization() < 0 && snapshot.GetMemoryUtilization() < 0 {
		snapshot.MetricsError = "workload pods declare no resource requests; utilization cannot be computed"
		return fmt.Errorf("%s", snapshot.GetMetricsError())
	}
	return nil
}

// metricsProvider selects the provider for this request.
func (s *dataAcquisitionServer) metricsProvider(req *dataacpb.WorkloadSnapshotRequest) MetricsProvider {
	if req.GetMetricsProvider() == "prometheus" {
		url := req.GetPrometheusUrl()
		if url == "" {
			url = "http://prometheus.monitoring.svc:9090"
		}
		return newPrometheusProvider(url)
	}
	return newMetricsServerProvider()
}

// collectForecast fills the forecast series and forecast metadata.
func (s *dataAcquisitionServer) collectForecast(
	ctx context.Context,
	req *dataacpb.WorkloadSnapshotRequest,
	snapshot *dataacpb.WorkloadSnapshot,
) error {
	provider := s.forecastProvider(req)
	snapshot.ForecastProvider = provider.Name()

	points, err := provider.Forecast(ctx, req.GetNamespace(), req.GetResourceName(), req.GetForecastHorizonSeconds())
	if err != nil {
		snapshot.ForecastError = err.Error()
		return err
	}
	for _, p := range points {
		snapshot.Forecast = append(snapshot.Forecast, &dataacpb.ForecastPoint{
			Timestamp:       p.Timestamp,
			PredictedCpu:    p.PredictedCPU,
			PredictedMemory: p.PredictedMemory,
			PredictedRps:    p.PredictedRPS,
		})
	}
	return nil
}

// forecastProvider selects the provider for this request.
func (s *dataAcquisitionServer) forecastProvider(req *dataacpb.WorkloadSnapshotRequest) ForecastProvider {
	if req.GetForecastProvider() == "rest" {
		url := req.GetForecastUrl()
		if url == "" {
			snapshotErr := "rest forecast provider selected without forecast_url"
			return &errorForecastProvider{err: fmt.Errorf("%s", snapshotErr)}
		}
		return newRESTForecastProvider(url)
	}
	return newStubForecastProvider()
}

// errorForecastProvider fails immediately with a fixed error; used
// when the request configuration is incomplete.
type errorForecastProvider struct{ err error }

func (p *errorForecastProvider) Name() string { return "error" }

func (p *errorForecastProvider) Forecast(ctx context.Context, namespace, workload string, horizonSeconds int32) ([]ForecastPoint, error) {
	return nil, p.err
}

// summarizePods builds the pod health summary from the pod list.
func summarizePods(pods *corev1.PodList) *dataacpb.PodStatusSummary {
	summary := &dataacpb.PodStatusSummary{}
	for i := range pods.Items {
		pod := &pods.Items[i]
		summary.TotalPods++
		switch {
		case pod.DeletionTimestamp != nil:
			summary.TerminatingPods++
		case pod.Status.Phase == corev1.PodPending:
			summary.PendingPods++
		case isPodReady(pod):
			summary.ReadyPods++
		}
	}
	return summary
}

func isPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// collectCapacity fills the eligible node capacity (nil = unknown).
func (s *dataAcquisitionServer) collectCapacity(ctx context.Context, snapshot *dataacpb.WorkloadSnapshot) {
	if cap := clusterCapacity(ctx); cap != nil {
		snapshot.Capacity = cap
	}
}

// collectQuota fills the namespace pods quota (nil = no quota known).
func (s *dataAcquisitionServer) collectQuota(ctx context.Context, namespace string, snapshot *dataacpb.WorkloadSnapshot) {
	if q := namespaceQuota(ctx, namespace); q != nil {
		snapshot.Quota = q
	}
}

func podNamesOf(pods *corev1.PodList) []string {
	names := make([]string, 0, len(pods.Items))
	for i := range pods.Items {
		names = append(names, pods.Items[i].Name)
	}
	sort.Strings(names)
	return names
}
