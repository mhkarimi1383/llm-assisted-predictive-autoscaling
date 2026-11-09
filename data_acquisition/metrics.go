// Pluggable metrics providers (Algorithm 2, cluster observation).
//
// Both providers report raw usage for the workload's pods; the
// snapshot builder computes utilization fractions against the pod
// resource requests uniformly, so providers stay interchangeable.
package dataacquisition

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// UsageSummary is the aggregated usage of one workload's pods.
// Negative values mean unknown.
type UsageSummary struct {
	CPUCores    float64 // cores in use across the workload's containers
	MemoryBytes float64 // working-set bytes in use
	PodCoverage int     // pods covered by the measurement
}

// MetricsProvider reports current usage for a set of pods.
type MetricsProvider interface {
	Name() string
	// Usage returns aggregate usage for the named pods; an error
	// means the provider could not measure (recorded as metrics_error).
	Usage(ctx context.Context, namespace string, podNames []string) (UsageSummary, error)
}

// --- metrics-server provider (Metrics API) ---

type metricsServerProvider struct{}

func newMetricsServerProvider() *metricsServerProvider { return &metricsServerProvider{} }

func (p *metricsServerProvider) Name() string { return "metrics-server" }

func (p *metricsServerProvider) Usage(ctx context.Context, namespace string, podNames []string) (UsageSummary, error) {
	list := podMetrics(ctx, namespace, podNames)
	if list == nil {
		return UsageSummary{}, fmt.Errorf("metrics-server unavailable or not installed")
	}
	summary := UsageSummary{PodCoverage: len(list.Items)}
	for _, pm := range list.Items {
		for _, container := range pm.Containers {
			if cpu := container.Usage.Cpu(); cpu != nil {
				summary.CPUCores += cpu.AsApproximateFloat64()
			}
			if mem := container.Usage.Memory(); mem != nil {
				summary.MemoryBytes += mem.AsApproximateFloat64()
			}
		}
	}
	return summary, nil
}

// --- Prometheus provider ---

type prometheusProvider struct {
	baseURL string
	client  *http.Client
}

func newPrometheusProvider(baseURL string) *prometheusProvider {
	return &prometheusProvider{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *prometheusProvider) Name() string { return "prometheus" }

type promQueryResult struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Value []any `json:"value"` // [timestamp, "value"]
		} `json:"result"`
	} `json:"data"`
	Error string `json:"error,omitempty"`
}

// query runs one instant query and returns the first sample value.
func (p *prometheusProvider) query(ctx context.Context, promql string) (float64, error) {
	endpoint := fmt.Sprintf("%s/api/v1/query?query=%s", p.baseURL, url.QueryEscape(promql))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("prometheus request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("prometheus returned %d", resp.StatusCode)
	}
	var parsed promQueryResult
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0, fmt.Errorf("failed to decode prometheus response: %w", err)
	}
	if parsed.Status != "success" {
		return 0, fmt.Errorf("prometheus query error: %s", parsed.Error)
	}
	if len(parsed.Data.Result) == 0 {
		return 0, fmt.Errorf("prometheus query returned no samples: %s", promql)
	}
	value := parsed.Data.Result[0].Value
	if len(value) < 2 {
		return 0, fmt.Errorf("malformed prometheus sample")
	}
	str, ok := value[1].(string)
	if !ok {
		return 0, fmt.Errorf("malformed prometheus sample value")
	}
	var v float64
	if _, err := fmt.Sscanf(str, "%g", &v); err != nil {
		return 0, fmt.Errorf("malformed prometheus sample value %q", str)
	}
	return v, nil
}

func (p *prometheusProvider) Usage(ctx context.Context, namespace string, podNames []string) (UsageSummary, error) {
	if len(podNames) == 0 {
		return UsageSummary{}, fmt.Errorf("no pods to measure")
	}
	// Pod-name alternatives joined as a regex alternation.
	alternation := ""
	for i, n := range podNames {
		if i > 0 {
			alternation += "|"
		}
		alternation += n
	}

	cpu, cpuErr := p.query(ctx, fmt.Sprintf(
		`sum(rate(container_cpu_usage_seconds_total{namespace=%q,pod=~"%s",container!=""}[5m]))`,
		namespace, alternation))
	mem, memErr := p.query(ctx, fmt.Sprintf(
		`sum(container_memory_working_set_bytes{namespace=%q,pod=~"%s",container!=""})`,
		namespace, alternation))
	if cpuErr != nil || memErr != nil {
		err := cpuErr
		if err == nil {
			err = memErr
		}
		return UsageSummary{}, err
	}
	return UsageSummary{CPUCores: cpu, MemoryBytes: mem, PodCoverage: len(podNames)}, nil
}

// --- shared computation helpers ---

// requestedResources sums the pod-template requests of a Deployment.
func requestedResources(deployment *appsv1.Deployment) (cpuCores, memoryBytes float64) {
	for _, c := range deployment.Spec.Template.Spec.Containers {
		if cpu := c.Resources.Requests.Cpu(); cpu != nil {
			cpuCores += resourceToFloat(cpu)
		}
		if mem := c.Resources.Requests.Memory(); mem != nil {
			memoryBytes += resourceToFloat(mem)
		}
	}
	return cpuCores, memoryBytes
}

func resourceToFloat(q *resource.Quantity) float64 {
	if q == nil {
		return 0
	}
	return q.AsApproximateFloat64()
}

// utilizationFraction converts usage and requests into a fraction in
// [0, inf); -1 when requests are unknown (cannot compute).
func utilizationFraction(usage, requests float64) float64 {
	if requests <= 0 {
		return -1
	}
	return usage / requests
}
