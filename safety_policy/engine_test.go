package safetypolicy

import (
	"strings"
	"testing"
	"time"
)

var baseConstraints = Constraints{
	MinReplicas:                2,
	MaxReplicas:                20,
	CooldownSeconds:            120,
	MaxScaleUpPercent:          100,
	MaxScaleDownPercent:        25,
	StabilizationWindowSeconds: 300,
	MaxActionsPerHour:          20,
	ClampEnabled:               true,
	Healthy:                    true,
}

func nowAt(secondsAgo int) (time.Time, []ActionRecord) {
	now := time.Unix(1_700_000_000, 0)
	return now, nil
}

func TestEvaluateApproval(t *testing.T) {
	now, _ := nowAt(0)
	v := Evaluate(
		Recommendation{Action: ActionScaleUp, CurrentReplicas: 4, ProposedTarget: 6},
		baseConstraints, nil, now)
	if v.Verdict != VerdictApproved || v.CheckedTarget != 6 {
		t.Fatalf("want APPROVED/6, got %s/%d (%v)", v.Verdict, v.CheckedTarget, v.Reasons)
	}
}

func TestEvaluateHoldAndNoop(t *testing.T) {
	now, _ := nowAt(0)

	v := Evaluate(
		Recommendation{Action: ActionHold, CurrentReplicas: 4, ProposedTarget: 4},
		baseConstraints, nil, now)
	if v.Verdict != VerdictHeld {
		t.Fatalf("HOLD must produce HELD, got %s", v.Verdict)
	}

	v = Evaluate(
		Recommendation{Action: ActionScaleUp, CurrentReplicas: 4, ProposedTarget: 4},
		baseConstraints, nil, now)
	if v.Verdict != VerdictHeld {
		t.Fatalf("noop target must produce HELD, got %s", v.Verdict)
	}
}

func TestEvaluateValidationFailures(t *testing.T) {
	now, _ := nowAt(0)

	cases := []struct {
		name string
		rec  Recommendation
		c    Constraints
	}{
		{"unknown action", Recommendation{Action: "GROW", CurrentReplicas: 4, ProposedTarget: 5}, baseConstraints},
		{"negative target", Recommendation{Action: ActionScaleUp, CurrentReplicas: 4, ProposedTarget: -1}, baseConstraints},
		{"min below one", Recommendation{Action: ActionScaleUp, CurrentReplicas: 4, ProposedTarget: 5},
			Constraints{MinReplicas: 0, MaxReplicas: 10, ClampEnabled: true}},
		{"max below min", Recommendation{Action: ActionScaleUp, CurrentReplicas: 4, ProposedTarget: 5},
			Constraints{MinReplicas: 10, MaxReplicas: 5, ClampEnabled: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := Evaluate(tc.rec, tc.c, nil, now)
			if v.Verdict != VerdictRejected {
				t.Fatalf("want REJECTED, got %s (%v)", v.Verdict, v.Reasons)
			}
		})
	}
}

func TestEvaluateCooldown(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	history := []ActionRecord{
		{Timestamp: now.Add(-60 * time.Second), Action: ActionScaleUp, FromReplicas: 3, ToReplicas: 4},
	}
	v := Evaluate(
		Recommendation{Action: ActionScaleUp, CurrentReplicas: 4, ProposedTarget: 5},
		baseConstraints, history, now)
	if v.Verdict != VerdictHeld {
		t.Fatalf("action 60s ago must hit 120s cooldown, got %s (%v)", v.Verdict, v.Reasons)
	}

	// After the cooldown passes the same proposal is approved.
	history[0].Timestamp = now.Add(-121 * time.Second)
	v = Evaluate(
		Recommendation{Action: ActionScaleUp, CurrentReplicas: 4, ProposedTarget: 5},
		baseConstraints, history, now)
	if v.Verdict != VerdictApproved {
		t.Fatalf("action 121s ago must pass cooldown, got %s (%v)", v.Verdict, v.Reasons)
	}
}

func TestEvaluateDryRunAndHoldDoNotConsumeCooldown(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	history := []ActionRecord{
		{Timestamp: now.Add(-10 * time.Second), Action: ActionScaleUp, FromReplicas: 3, ToReplicas: 4, DryRun: true},
		{Timestamp: now.Add(-5 * time.Second), Action: ActionHold, ToReplicas: 4},
	}
	v := Evaluate(
		Recommendation{Action: ActionScaleUp, CurrentReplicas: 4, ProposedTarget: 5},
		baseConstraints, history, now)
	if v.Verdict != VerdictApproved {
		t.Fatalf("dry-run and HOLD records must not trigger cooldown, got %s (%v)", v.Verdict, v.Reasons)
	}
}

func TestEvaluateRateLimit(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := baseConstraints
	c.MaxActionsPerHour = 2
	history := []ActionRecord{
		{Timestamp: now.Add(-50 * time.Minute), Action: ActionScaleUp, ToReplicas: 4},
		{Timestamp: now.Add(-20 * time.Minute), Action: ActionScaleDown, ToReplicas: 3},
	}
	v := Evaluate(
		Recommendation{Action: ActionScaleUp, CurrentReplicas: 3, ProposedTarget: 4},
		c, history, now)
	if v.Verdict != VerdictHeld {
		t.Fatalf("third action within the hour must be rate limited, got %s (%v)", v.Verdict, v.Reasons)
	}
}

func TestEvaluateDownscaleStabilization(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	history := []ActionRecord{
		{Timestamp: now.Add(-60 * time.Second), Action: ActionScaleUp, FromReplicas: 4, ToReplicas: 8},
	}
	v := Evaluate(
		Recommendation{Action: ActionScaleDown, CurrentReplicas: 8, ProposedTarget: 6},
		baseConstraints, history, now)
	if v.Verdict != VerdictHeld {
		t.Fatalf("downscale below the highest recent target must stabilize, got %s (%v)", v.Verdict, v.Reasons)
	}

	// Scaling down to that recorded level is allowed.
	v = Evaluate(
		Recommendation{Action: ActionScaleDown, CurrentReplicas: 8, ProposedTarget: 8},
		baseConstraints, history, now)
	if v.Verdict != VerdictHeld {
		t.Fatalf("equal target is a noop (HELD expected), got %s", v.Verdict)
	}
}

func TestEvaluatePercentageLimits(t *testing.T) {
	now, _ := nowAt(0)

	// Scale-down limited to 25% of 8 = 2 replicas.
	v := Evaluate(
		Recommendation{Action: ActionScaleDown, CurrentReplicas: 8, ProposedTarget: 5},
		baseConstraints, nil, now)
	if v.Verdict != VerdictClamped || v.CheckedTarget != 6 {
		t.Fatalf("want CLAMPED/6, got %s/%d (%v)", v.Verdict, v.CheckedTarget, v.Reasons)
	}

	// Zero allowance blocks movement (floor rounding).
	c := baseConstraints
	c.MaxScaleDownPercent = 10 // 10% of 5 -> floor(0.5)=0
	v = Evaluate(
		Recommendation{Action: ActionScaleDown, CurrentReplicas: 5, ProposedTarget: 4},
		c, nil, now)
	if v.Verdict != VerdictHeld {
		t.Fatalf("zero allowance must block, got %s (%v)", v.Verdict, v.Reasons)
	}

	// MinStepReplicas funds movement when the strict floor blocks it
	// (the explicit policy choice for reaching minReplicas).
	c = baseConstraints
	c.MaxScaleDownPercent = 25 // 25% of 3 -> floor(0.75)=0
	c.MinStepReplicas = 1
	v = Evaluate(
		Recommendation{Action: ActionScaleDown, CurrentReplicas: 3, ProposedTarget: 2},
		c, nil, now)
	if v.Verdict != VerdictApproved || v.CheckedTarget != 2 {
		t.Fatalf("min step must fund 3->2, got %s/%d (%v)", v.Verdict, v.CheckedTarget, v.Reasons)
	}

	// Without the min-step policy the same proposal stays blocked.
	c = baseConstraints
	c.MaxScaleDownPercent = 25
	v = Evaluate(
		Recommendation{Action: ActionScaleDown, CurrentReplicas: 3, ProposedTarget: 2},
		c, nil, now)
	if v.Verdict != VerdictHeld {
		t.Fatalf("strict floor must block 3->2, got %s (%v)", v.Verdict, v.Reasons)
	}

	// Unlimited when the percentage is 0.
	c = baseConstraints
	c.MaxScaleUpPercent = 0
	v = Evaluate(
		Recommendation{Action: ActionScaleUp, CurrentReplicas: 2, ProposedTarget: 20},
		c, nil, now)
	if v.Verdict != VerdictApproved || v.CheckedTarget != 20 {
		t.Fatalf("0%% must mean unlimited, got %s/%d (%v)", v.Verdict, v.CheckedTarget, v.Reasons)
	}
}

func TestEvaluateClampingRules(t *testing.T) {
	now, _ := nowAt(0)

	// Bounds clamp: below min. The 25% scale-down limit from 5 gives
	// a feasible floor of 4 (5 - floor(1.25)), so 1 clamps to 4.
	v := Evaluate(
		Recommendation{Action: ActionScaleDown, CurrentReplicas: 5, ProposedTarget: 1},
		baseConstraints, nil, now)
	if v.Verdict != VerdictClamped || v.CheckedTarget != 4 {
		t.Fatalf("want CLAMPED/4 at the scale-down floor, got %s/%d", v.Verdict, v.CheckedTarget)
	}

	// With the percentage limit disabled, the min-replica bound is
	// what clamps: 1 -> 2.
	c := baseConstraints
	c.MaxScaleDownPercent = 0
	v = Evaluate(
		Recommendation{Action: ActionScaleDown, CurrentReplicas: 5, ProposedTarget: 1},
		c, nil, now)
	if v.Verdict != VerdictClamped || v.CheckedTarget != 2 {
		t.Fatalf("want CLAMPED/2 at min replicas, got %s/%d", v.Verdict, v.CheckedTarget)
	}

	// Bounds clamp: above max.
	v = Evaluate(
		Recommendation{Action: ActionScaleUp, CurrentReplicas: 18, ProposedTarget: 40},
		baseConstraints, nil, now)
	if v.Verdict != VerdictClamped || v.CheckedTarget != 20 {
		t.Fatalf("want CLAMPED/20 at max replicas, got %s/%d", v.Verdict, v.CheckedTarget)
	}

	// Clamping disabled -> reject.
	c = baseConstraints
	c.ClampEnabled = false
	v = Evaluate(
		Recommendation{Action: ActionScaleDown, CurrentReplicas: 5, ProposedTarget: 1},
		c, nil, now)
	if v.Verdict != VerdictRejected {
		t.Fatalf("want REJECTED with clamping disabled, got %s", v.Verdict)
	}

	// Clamp that would reverse direction -> reject. Current 2, min 2,
	// scale-down 1: feasible floor is 2, proposed 1 clamps to 2, which
	// equals current -> HELD (movement vanished), not reversed.
	v = Evaluate(
		Recommendation{Action: ActionScaleDown, CurrentReplicas: 2, ProposedTarget: 1},
		baseConstraints, nil, now)
	if v.Verdict != VerdictHeld {
		t.Fatalf("vanishing movement must be HELD, got %s (%v)", v.Verdict, v.Reasons)
	}
}

func TestEvaluateHealthBlocksDownscale(t *testing.T) {
	now, _ := nowAt(0)
	// Unhealthy workload (ready below desired): reduction held.
	c := baseConstraints
	c.Healthy = false
	v := Evaluate(
		Recommendation{Action: ActionScaleDown, CurrentReplicas: 8, ProposedTarget: 6},
		c, nil, now)
	if v.Verdict != VerdictHeld {
		t.Fatalf("downscale on unhealthy workload must be HELD, got %s (%v)", v.Verdict, v.Reasons)
	}
	// Scale-up on the same unhealthy workload is still permitted
	// (adding capacity to an unhealthy workload is the remedy).
	v = Evaluate(
		Recommendation{Action: ActionScaleUp, CurrentReplicas: 8, ProposedTarget: 10},
		c, nil, now)
	if v.Verdict != VerdictApproved {
		t.Fatalf("scale-up must not require health, got %s (%v)", v.Verdict, v.Reasons)
	}
}

func TestEvaluateQuotaRejects(t *testing.T) {
	now, _ := nowAt(0)
	c := baseConstraints
	c.QuotaMaxPods = 15
	v := Evaluate(
		Recommendation{Action: ActionScaleUp, CurrentReplicas: 14, ProposedTarget: 18},
		c, nil, now)
	if v.Verdict != VerdictRejected {
		t.Fatalf("target above pods quota must be REJECTED, got %s (%v)", v.Verdict, v.Reasons)
	}
	if !contains(v.Reasons, "quota") {
		t.Fatalf("reason must mention quota: %v", v.Reasons)
	}

	// Target within quota passes.
	v = Evaluate(
		Recommendation{Action: ActionScaleUp, CurrentReplicas: 12, ProposedTarget: 14},
		c, nil, now)
	if v.Verdict != VerdictApproved {
		t.Fatalf("target within quota must be APPROVED, got %s (%v)", v.Verdict, v.Reasons)
	}
}

func TestEvaluateCapacityChecks(t *testing.T) {
	now, _ := nowAt(0)
	// 0.1 core + 96Mi per pod; nodes offer 1 core / 1Gi.
	c := baseConstraints
	c.PodRequestCPUCores = 0.1
	c.PodRequestMemoryBytes = 96 << 20
	c.AllocatableCPUCores = 1.0
	c.AllocatableMemoryBytes = 1 << 30

	// Scale 4 -> 8: peak 8 pods = 0.8 core, 768Mi <= 1Gi: fits.
	v := Evaluate(
		Recommendation{Action: ActionScaleUp, CurrentReplicas: 4, ProposedTarget: 8},
		c, nil, now)
	if v.Verdict != VerdictApproved {
		t.Fatalf("peak within capacity must be APPROVED, got %s (%v)", v.Verdict, v.Reasons)
	}

	// Scale 4 -> 12 (unlimited percent): peak 12 pods = 1.2 cores
	// > 1.0: rejected on CPU.
	c0 := c
	c0.MaxScaleUpPercent = 0
	v = Evaluate(
		Recommendation{Action: ActionScaleUp, CurrentReplicas: 4, ProposedTarget: 12},
		c0, nil, now)
	if v.Verdict != VerdictRejected || !contains(v.Reasons, "capacity") {
		t.Fatalf("peak above CPU capacity must be REJECTED, got %s (%v)", v.Verdict, v.Reasons)
	}

	// Overlap rule: peak uses max(current, target), so scaling DOWN
	// never fails capacity.
	v = Evaluate(
		Recommendation{Action: ActionScaleDown, CurrentReplicas: 10, ProposedTarget: 8},
		c, nil, now)
	if v.Verdict != VerdictApproved {
		t.Fatalf("scale-down must not trip capacity, got %s (%v)", v.Verdict, v.Reasons)
	}

	// Unknown capacity dimension: skipped, still approved.
	c2 := c0
	c2.AllocatableCPUCores = 0
	c2.AllocatableMemoryBytes = 0
	v = Evaluate(
		Recommendation{Action: ActionScaleUp, CurrentReplicas: 4, ProposedTarget: 12},
		c2, nil, now)
	if v.Verdict != VerdictApproved || !contains(v.PolicyChecks, "capacity: unknown") {
		t.Fatalf("unknown capacity must be skipped with audit note, got %s (%v)", v.Verdict, v.PolicyChecks)
	}
}

func contains(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
