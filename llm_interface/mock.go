// Deterministic mock chat provider for tests, demos, and dry runs.
//
// The mock inspects the request prompt for the analyst role and for
// the snapshot values embedded by the reasoning engine, then returns
// a fixed decision policy:
//
//   - history analyst: abstains (no history store in v1)
//   - cpu utilization above 70% (or ready below desired): SCALE_UP
//   - cpu utilization below 30%: SCALE_DOWN
//   - otherwise: HOLD
//
// It is deterministic for a given input and requires no network.
package llminterface

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	llminterfacepb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/llminterface"
)

// mockProvider implements ChatProvider without any external service.
type mockProvider struct{}

func newMockProvider() *mockProvider { return &mockProvider{} }

// mockSnapshot mirrors the snapshot fields the reasoning engine embeds
// in prompts. Only the fields the mock reacts to are declared.
type mockSnapshot struct {
	DesiredReplicas   int32   `json:"desired_replicas"`
	ReadyReplicas     int32   `json:"ready_replicas"`
	CPUUtilization    float64 `json:"cpu_utilization"`
	MemoryUtilization float64 `json:"memory_utilization"`
	MinReplicas       int32   // from the prompt's configured range
	MaxReplicas       int32
}

func (m *mockProvider) Chat(ctx context.Context, req *llminterfacepb.ChatRequest) (*llminterfacepb.ChatResponse, error) {
	if len(req.GetMessages()) == 0 {
		return nil, fmt.Errorf("chat request has no messages")
	}

	prompt := lastUserContent(req.GetMessages())
	role := extractStringField(prompt, "role")
	snapshot := extractSnapshot(prompt)

	content, err := mockDecision(role, snapshot, prompt)
	if err != nil {
		return nil, err
	}

	return &llminterfacepb.ChatResponse{
		Content:          content,
		PromptTokens:     int32(len(prompt) / 4),
		CompletionTokens: int32(len(content) / 4),
		FinishReason:     "stop",
		Model:            "mock",
	}, nil
}

// mockDecision implements the deterministic mock policy. The decision
// function is shared so tests can assert on it directly.
func mockDecision(role string, snapshot mockSnapshot, prompt string) (string, error) {
	switch role {
	case "history":
		// Algorithm 4: the history analyst compares recent outcomes and
		// abstains when the evidence is insufficient. It only votes when
		// it has something to say: a recent direction reversal (thrashing
		// signal), where a HOLD vote dampens further flips. Otherwise it
		// stays neutral so the specialist analysts decide.
		tos := extractAllNumberFields(prompt, "to_replicas")
		froms := extractAllNumberFields(prompt, "from_replicas")
		if len(tos) < 2 || len(froms) < 2 || snapshot.DesiredReplicas <= 0 {
			return mustJSON(map[string]any{
				"abstention_reason": "no scaling history available",
			}), nil
		}
		n := len(tos)
		last := sign(tos[n-1] - froms[n-1])
		prev := sign(tos[n-2] - froms[n-2])
		if last != 0 && prev != 0 && last != prev {
			return mustJSON(map[string]any{
				"action":     "HOLD",
				"target":     snapshot.DesiredReplicas,
				"confidence": 0.7,
				"rationale":  "mock history analyst: recent direction reversal detected; vote HOLD to dampen thrashing",
			}), nil
		}
		return mustJSON(map[string]any{
			"abstention_reason": "no oscillation pattern in recent scaling history",
		}), nil

	case "policy":
		// The policy analyst explains constraints and votes HOLD; hard
		// enforcement happens in the safety engine (Algorithm 6).
		return mustJSON(map[string]any{
			"action":     "HOLD",
			"target":     snapshot.DesiredReplicas,
			"confidence": 0.8,
			"rationale":  "mock policy analyst: configured constraints permit the current state",
		}), nil

	case "forecast", "status", "synthesizer", "":
		// fall through to the utilization-based policy below
	default:
		return "", fmt.Errorf("mock provider: unknown analyst role %q", role)
	}

	if snapshot.DesiredReplicas <= 0 {
		return "", fmt.Errorf("mock provider: snapshot lacks desired_replicas")
	}

	action, target := mockScaleDecision(snapshot)
	return mustJSON(map[string]any{
		"action":     action,
		"target":     target,
		"confidence": 0.85,
		"rationale": fmt.Sprintf(
			"mock %s analyst: cpu=%.2f ready=%d desired=%d -> %s",
			role, snapshot.CPUUtilization, snapshot.ReadyReplicas, snapshot.DesiredReplicas, action,
		),
	}), nil
}

// mockScaleDecision maps snapshot pressure to action and target.
// Proposals are clamped into the configured replica range (Algorithm
// 3: analysts propose within bounds); a proposal whose direction
// collapses after clamping becomes HOLD at the current count.
func mockScaleDecision(s mockSnapshot) (string, int32) {
	current := s.DesiredReplicas
	switch {
	case s.CPUUtilization > 0.70 || s.ReadyReplicas < s.DesiredReplicas:
		step := int32(math.Max(1, math.Ceil(float64(current)*0.25)))
		target := current + step
		if s.MaxReplicas > 0 && target > s.MaxReplicas {
			target = s.MaxReplicas
		}
		if target <= current {
			return "HOLD", current
		}
		return "SCALE_UP", target
	case s.CPUUtilization >= 0 && s.CPUUtilization < 0.30:
		step := int32(math.Max(1, math.Floor(float64(current)*0.25)))
		target := current - step
		if s.MinReplicas > 0 && target < s.MinReplicas {
			target = s.MinReplicas
		}
		if target >= current {
			return "HOLD", current
		}
		return "SCALE_DOWN", target
	default:
		return "HOLD", current
	}
}

// lastUserContent returns the content of the last user message, which
// carries the snapshot and role for analyst prompts.
func lastUserContent(msgs []*llminterfacepb.ChatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].GetRole() == "user" {
			return msgs[i].GetContent()
		}
	}
	if len(msgs) > 0 {
		return msgs[len(msgs)-1].GetContent()
	}
	return ""
}

// extractStringField finds `"field": "value"` in a prompt without
// requiring full JSON parsing (prompts mix prose and JSON).
func extractStringField(prompt, field string) string {
	marker := "\"" + field + "\""
	idx := strings.Index(prompt, marker)
	if idx < 0 {
		return ""
	}
	rest := prompt[idx+len(marker):]
	rest = strings.TrimLeft(rest, " \t\r\n:")
	if !strings.HasPrefix(rest, "\"") {
		return ""
	}
	rest = rest[1:]
	if end := strings.IndexByte(rest, '"'); end >= 0 {
		return rest[:end]
	}
	return ""
}

// extractNumberField finds `"field": number` in a prompt.
func extractNumberField(prompt string, field string) (float64, bool) {
	marker := "\"" + field + "\""
	idx := strings.Index(prompt, marker)
	if idx < 0 {
		return 0, false
	}
	rest := strings.TrimLeft(prompt[idx+len(marker):], " \t\r\n:")
	end := 0
	for end < len(rest) && (rest[end] == '-' || rest[end] == '.' || (rest[end] >= '0' && rest[end] <= '9') || rest[end] == 'e' || rest[end] == 'E' || rest[end] == '+') {
		end++
	}
	if end == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(rest[:end], 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// numberFieldRe matches `"field": <number>` occurrences.
var numberFieldRe = regexp.MustCompile(`"` + `([^"]+)` + `":\s*(-?[0-9]+(?:\.[0-9]+)?)`)

// extractAllNumberFields returns every numeric value of `"field":`
// in prompt order.
func extractAllNumberFields(prompt string, field string) []float64 {
	var values []float64
	for _, m := range numberFieldRe.FindAllStringSubmatch(prompt, -1) {
		if m[1] != field {
			continue
		}
		if v, err := strconv.ParseFloat(m[2], 64); err == nil {
			values = append(values, v)
		}
	}
	return values
}

// sign returns -1, 0, or 1 for the difference of two counts.
func sign(d float64) int {
	switch {
	case d > 0:
		return 1
	case d < 0:
		return -1
	default:
		return 0
	}
}

// extractSnapshot pulls the observed snapshot values out of the
// prompt. Missing fields keep their zero values; utilization 0 means
// unknown to the mock policy.
func extractSnapshot(prompt string) mockSnapshot {
	s := mockSnapshot{}
	if v, ok := extractNumberField(prompt, "desired_replicas"); ok {
		s.DesiredReplicas = int32(v)
	}
	if v, ok := extractNumberField(prompt, "ready_replicas"); ok {
		s.ReadyReplicas = int32(v)
	}
	if v, ok := extractNumberField(prompt, "cpu_utilization"); ok {
		// Utilization arrives as a fraction; negative means unknown.
		// Zero is a known, genuinely idle measurement — keep it.
		if v >= 0 {
			s.CPUUtilization = v
		}
	}
	if v, ok := extractNumberField(prompt, "memory_utilization"); ok {
		if v >= 0 {
			s.MemoryUtilization = v
		}
	}
	if m := replicaRangeRe.FindStringSubmatch(prompt); m != nil {
		minV, _ := strconv.Atoi(m[1])
		maxV, _ := strconv.Atoi(m[2])
		s.MinReplicas = int32(minV)
		s.MaxReplicas = int32(maxV)
	}
	return s
}

// replicaRangeRe matches the configured range line of analyst prompts.
var replicaRangeRe = regexp.MustCompile(`allowed replica range: \[(-?[0-9]+), (-?[0-9]+)\]`)

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// Compile-time interface check.
var _ ChatProvider = (*mockProvider)(nil)
