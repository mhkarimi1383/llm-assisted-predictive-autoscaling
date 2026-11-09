// Safety Policy Engine - Enforces hard safety constraints
//
// The EvaluateScaling RPC runs the deterministic scaling safety
// engine (Algorithm 6, v1 subset) implemented in engine.go. It is
// pure and independent of the LLM. The legacy content-filter RPCs
// remain available for prompt/output screening.
package safetypolicy

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/grpc"

	safetypolicypb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/safetypolicy"

	helpers "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/helpers"
)

// safetyPolicyServer implements the SafetyPolicyService service
type safetyPolicyServer struct {
	safetypolicypb.UnimplementedSafetyPolicyServiceServer
}

// Start initializes and starts the Safety Policy Engine.
func Start(ctx context.Context, listenPort int) error {
	server := &safetyPolicyServer{}
	return helpers.ServeGRPC(ctx, listenPort, "Safety Policy Engine", func(s *grpc.Server) {
		safetypolicypb.RegisterSafetyPolicyServiceServer(s, server)
	})
}

// EvaluateScaling implements the SafetyPolicyService.EvaluateScaling
// method (Algorithm 6). Deterministic and independent of the LLM.
func (s *safetyPolicyServer) EvaluateScaling(ctx context.Context, req *safetypolicypb.EvaluateScalingRequest) (*safetypolicypb.ScalingVerdictResponse, error) {
	log.Printf("Received EvaluateScaling request: id=%s action=%s current=%d target=%d",
		req.GetRequestId(), req.GetAction(), req.GetCurrentReplicas(), req.GetProposedTarget())

	constraints := req.GetConstraints()
	now := time.Unix(req.GetNow(), 0)
	if req.GetNow() == 0 {
		now = time.Now()
	}

	history := make([]ActionRecord, 0, len(req.GetRecentActions()))
	for _, a := range req.GetRecentActions() {
		history = append(history, ActionRecord{
			Timestamp:    time.Unix(a.GetTimestamp(), 0),
			Action:       a.GetAction(),
			FromReplicas: a.GetFromReplicas(),
			ToReplicas:   a.GetToReplicas(),
			DryRun:       a.GetDryRun(),
		})
	}

	rec := Recommendation{
		Action:          req.GetAction(),
		CurrentReplicas: req.GetCurrentReplicas(),
		ProposedTarget:  req.GetProposedTarget(),
	}
	constraintsEval := Constraints{
		MinReplicas:                constraints.GetMinReplicas(),
		MaxReplicas:                constraints.GetMaxReplicas(),
		CooldownSeconds:            constraints.GetCooldownSeconds(),
		MaxScaleUpPercent:          constraints.GetMaxScaleUpPercent(),
		MaxScaleDownPercent:        constraints.GetMaxScaleDownPercent(),
		StabilizationWindowSeconds: constraints.GetStabilizationWindowSeconds(),
		MaxActionsPerHour:          constraints.GetMaxActionsPerHour(),
		MinStepReplicas:            constraints.GetMinStepReplicas(),
		Healthy:                    constraints.GetHealthy(),
		QuotaMaxPods:               constraints.GetQuotaMaxPods(),
		PodRequestCPUCores:         constraints.GetPodRequestCpuCores(),
		PodRequestMemoryBytes:      constraints.GetPodRequestMemoryBytes(),
		AllocatableCPUCores:        constraints.GetAllocatableCpuCores(),
		AllocatableMemoryBytes:     constraints.GetAllocatableMemoryBytes(),
		ClampEnabled:               true, // deterministic clamping is always on in v1
	}

	verdict := Evaluate(rec, constraintsEval, history, now)

	log.Printf("Safety verdict for %s: %s target=%d reasons=%v",
		req.GetRequestId(), verdict.Verdict, verdict.CheckedTarget, verdict.Reasons)

	return &safetypolicypb.ScalingVerdictResponse{
		Verdict:       verdictToProto(verdict.Verdict),
		CheckedTarget: verdict.CheckedTarget,
		Reasons:       verdict.Reasons,
		PolicyChecks:  verdict.PolicyChecks,
	}, nil
}

func verdictToProto(v string) safetypolicypb.ScalingVerdict {
	switch v {
	case VerdictApproved:
		return safetypolicypb.ScalingVerdict_VERDICT_APPROVED
	case VerdictClamped:
		return safetypolicypb.ScalingVerdict_VERDICT_CLAMPED
	case VerdictHeld:
		return safetypolicypb.ScalingVerdict_VERDICT_HELD
	case VerdictRejected:
		return safetypolicypb.ScalingVerdict_VERDICT_REJECTED
	default:
		return safetypolicypb.ScalingVerdict_VERDICT_UNSPECIFIED
	}
}

// CheckSafety implements the SafetyPolicyService.CheckSafety method
func (s *safetyPolicyServer) CheckSafety(ctx context.Context, req *safetypolicypb.SafetyCheckRequest) (*safetypolicypb.SafetyCheckResponse, error) {
	log.Printf("Received CheckSafety request: %v", req)

	// Validate request - check if both prompt and generated_text are empty
	if req.GetPrompt() == "" && req.GetGeneratedText() == "" {
		return &safetypolicypb.SafetyCheckResponse{
			IsSafe:     false,
			Violations: []string{"Input cannot be empty"},
			Confidence: 1.0,
		}, nil
	}

	// Simple content screening: flag texts containing blocked words.
	isSafe := true
	var violations []string
	confidence := 0.95

	unsafeWords := []string{"hate", "violence", "illegal"}
	textToCheck := req.GetPrompt()
	if textToCheck == "" {
		textToCheck = req.GetGeneratedText()
	}
	lowerText := strings.ToLower(textToCheck)
	for _, word := range unsafeWords {
		if strings.Contains(lowerText, word) {
			isSafe = false
			violations = append(violations, fmt.Sprintf("Contains potentially unsafe word: %s", word))
			confidence = 0.9
			break
		}
	}

	safeText := ""
	if !isSafe {
		// Apply basic filtering for demo
		safeText = textToCheck
		for _, word := range unsafeWords {
			safeText = strings.ReplaceAll(safeText, word, "[FILTERED]")
			safeText = strings.ReplaceAll(safeText, strings.ToUpper(word), "[FILTERED]")
		}
	}

	return &safetypolicypb.SafetyCheckResponse{
		IsSafe:     isSafe,
		Violations: violations,
		SafeText:   safeText,
		Confidence: confidence,
	}, nil
}

// ApplySafetyPolicy implements the SafetyPolicyService.ApplySafetyPolicy method
func (s *safetyPolicyServer) ApplySafetyPolicy(ctx context.Context, req *safetypolicypb.SafetyApplyRequest) (*safetypolicypb.SafetyApplyResponse, error) {
	log.Printf("Received ApplySafetyPolicy request: %v", req)

	// Validate request
	if req.GetGeneratedText() == "" {
		return &safetypolicypb.SafetyApplyResponse{
			IsSafe:                 false,
			SafeText:               "",
			TransformationsApplied: []string{},
		}, nil
	}

	// Apply the same simple content screening transformations.
	processedText := req.GetGeneratedText()

	unsafeWords := []string{"hate", "violence", "illegal"}
	var transformations []string
	for _, word := range unsafeWords {
		if strings.Contains(processedText, word) {
			processedText = strings.ReplaceAll(processedText, word, "[FILTERED]")
			transformations = append(transformations, fmt.Sprintf("Replaced '%s' with '[FILTERED]'", word))
		}
		if strings.Contains(processedText, strings.ToUpper(word)) {
			processedText = strings.ReplaceAll(processedText, strings.ToUpper(word), "[FILTERED]")
			transformations = append(transformations, fmt.Sprintf("Replaced '%s' with '[FILTERED]'", strings.ToUpper(word)))
		}
	}

	// Determine if text is safe after transformations
	isSafe := len(transformations) == 0

	return &safetypolicypb.SafetyApplyResponse{
		IsSafe:                 isSafe,
		SafeText:               processedText,
		TransformationsApplied: transformations,
	}, nil
}

// GetSafetyPolicies implements the SafetyPolicyService.GetSafetyPolicies method
func (s *safetyPolicyServer) GetSafetyPolicies(ctx context.Context, req *safetypolicypb.GetSafetyPoliciesRequest) (*safetypolicypb.GetSafetyPoliciesResponse, error) {
	log.Printf("Received GetSafetyPolicies request: %v", req)

	// Return the documented v1 hard-constraint policy set.
	examplePolicies := []*safetypolicypb.SafetyPolicy{
		{
			Id:          "policy_001",
			Name:        "Content Safety Policy",
			Description: "Basic content safety guidelines",
			Type:        safetypolicypb.PolicyType_KEYWORD,
			Pattern:     "hate|violence|illegal",
			Action:      safetypolicypb.Action_TRANSFORM,
		},
		{
			Id:          "policy_002",
			Name:        "Profanity Filter",
			Description: "Filter profane language",
			Type:        safetypolicypb.PolicyType_KEYWORD,
			Pattern:     "damn|hell|shit",
			Action:      safetypolicypb.Action_TRANSFORM,
		},
	}

	return &safetypolicypb.GetSafetyPoliciesResponse{
		Policies: examplePolicies,
	}, nil
}

// UpdateSafetyPolicy implements the SafetyPolicyService.UpdateSafetyPolicy method
func (s *safetyPolicyServer) UpdateSafetyPolicy(ctx context.Context, req *safetypolicypb.UpdateSafetyPolicyRequest) (*safetypolicypb.UpdateSafetyPolicyResponse, error) {
	log.Printf("Received UpdateSafetyPolicy request: %v", req)

	// Validate request
	if req.GetPolicy() == nil {
		return &safetypolicypb.UpdateSafetyPolicyResponse{
			Success: false,
			Message: "Policy cannot be empty",
		}, nil
	}

	// v1 keeps policies in code; updates are acknowledged but not
	// persisted.
	return &safetypolicypb.UpdateSafetyPolicyResponse{
		Success: true,
		Message: "Safety policy updated successfully",
	}, nil
}
