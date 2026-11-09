// Pluggable workload forecast providers (Algorithm 2, forecast
// collection).
//
//   - stub: a deterministic synthetic demand curve for demos and
//     tests; no external service required.
//   - rest: fetches predictions from an external HTTP service.
package dataacquisition

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"
)

// ForecastPoint is one predicted-demand sample.
type ForecastPoint struct {
	Timestamp       int64
	PredictedCPU    float64 // fraction of requested CPU; negative = unknown
	PredictedMemory float64
	PredictedRPS    float64 // negative = unknown
}

// ForecastProvider produces the demand forecast for the horizon.
type ForecastProvider interface {
	Name() string
	Forecast(ctx context.Context, namespace, workload string, horizonSeconds int32) ([]ForecastPoint, error)
}

// --- stub provider (synthetic sine-wave demand) ---

type stubForecastProvider struct{}

func newStubForecastProvider() *stubForecastProvider { return &stubForecastProvider{} }

func (p *stubForecastProvider) Name() string { return "stub" }

// Forecast produces a deterministic daily-pattern curve: utilization
// oscillates between 0.2 and 1.0 over a 24h cycle. Values are
// fractions of requests, matching the snapshot contract.
func (p *stubForecastProvider) Forecast(ctx context.Context, namespace, workload string, horizonSeconds int32) ([]ForecastPoint, error) {
	if horizonSeconds <= 0 {
		horizonSeconds = 900
	}
	const stepSeconds = 60
	now := time.Now().Unix()
	points := make([]ForecastPoint, 0, horizonSeconds/stepSeconds)
	for offset := int32(stepSeconds); offset <= horizonSeconds; offset += stepSeconds {
		ts := now + int64(offset)
		// Phase from wall-clock minutes keeps the curve continuous
		// across successive cycles.
		phase := (float64(ts%86400) / 86400) * 2 * math.Pi
		cpu := 0.6 + 0.4*math.Sin(phase) // [0.2, 1.0]
		mem := 0.5 + 0.2*math.Sin(phase+math.Pi/3)
		points = append(points, ForecastPoint{
			Timestamp:       ts,
			PredictedCPU:    math.Round(cpu*1000) / 1000,
			PredictedMemory: math.Round(mem*1000) / 1000,
			PredictedRPS:    -1,
		})
	}
	return points, nil
}

// --- REST provider ---

type restForecastProvider struct {
	url    string
	client *http.Client
}

func newRESTForecastProvider(url string) *restForecastProvider {
	return &restForecastProvider{
		url:    url,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *restForecastProvider) Name() string { return "rest" }

// Forecast GETs the predictor URL with query parameters and expects a
// JSON body: {"points": [{"timestamp": ..., "predicted_cpu": ...,
// "predicted_memory": ..., "predicted_rps": ...}]}.
func (p *restForecastProvider) Forecast(ctx context.Context, namespace, workload string, horizonSeconds int32) ([]ForecastPoint, error) {
	endpoint := fmt.Sprintf("%s?namespace=%s&workload=%s&horizon=%d",
		p.url, namespace, workload, horizonSeconds)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("predictor request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("predictor returned %d", resp.StatusCode)
	}
	var parsed struct {
		Points []struct {
			Timestamp       int64   `json:"timestamp"`
			PredictedCPU    float64 `json:"predicted_cpu"`
			PredictedMemory float64 `json:"predicted_memory"`
			PredictedRPS    float64 `json:"predicted_rps"`
		} `json:"points"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("failed to decode predictor response: %w", err)
	}
	points := make([]ForecastPoint, 0, len(parsed.Points))
	for _, pt := range parsed.Points {
		points = append(points, ForecastPoint{
			Timestamp:       pt.Timestamp,
			PredictedCPU:    pt.PredictedCPU,
			PredictedMemory: pt.PredictedMemory,
			PredictedRPS:    pt.PredictedRPS,
		})
	}
	return points, nil
}
