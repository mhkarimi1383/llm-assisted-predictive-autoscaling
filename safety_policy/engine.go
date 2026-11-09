// Deterministic scaling safety evaluation (Algorithm 6 of the
// algorithms document, v1 subset).
//
// The engine is a pure function: it never talks to the LLM, the
// cluster, or any other service. An APPROVED or CLAMPED verdict is
// the only way a scaling write may proceed.
//
// v1 subset of Algorithm 6: replica bounds, cooldown, rate limit,
// downscale stabilization, per-action percentage limits, and
// deterministic clamping. Capacity, quota, and health checks require
// richer observations and are deferred to v2.
package safetypolicy

import (
	"fmt"
	"math"
	"time"
)

// Scaling action names (match the reasoning engine and CRD).
const (
	ActionScaleUp   = "SCALE_UP"
	ActionScaleDown = "SCALE_DOWN"
	ActionHold      = "HOLD"
)

// Verdict names.
const (
	VerdictApproved = "APPROVED"
	VerdictClamped  = "CLAMPED"
	VerdictHeld     = "HELD"
	VerdictRejected = "REJECTED"
)

// Constraints are the hard limits enforced by the engine. They are
// sourced from the LLMAutoscaler spec by the operator; the LLM cannot
// change them.
type Constraints struct {
	MinReplicas                int32
	MaxReplicas                int32
	CooldownSeconds            int32
	MaxScaleUpPercent          int32 // 0 = unlimited
	MaxScaleDownPercent        int32 // 0 = unlimited
	StabilizationWindowSeconds int32
	MaxActionsPerHour          int32 // 0 = unlimited
	// MinStepReplicas funds at least this many replicas of movement
	// when a percentage allowance rounds down to zero (0 = strict
	// floor rounding).
	MinStepReplicas int32
	// ClampEnabled moves an out-of-range target to the nearest
	// feasible integer instead of rejecting it.
	ClampEnabled bool

	// --- Capacity, quota, and health observations (Algorithm 6) ---

	// Workload health: ready pods cover the desired count and no
	// pending/terminating pods exist. Reductions on an unhealthy
	// workload are held.
	Healthy bool
	// Declared requests per pod of the target workload (capacity
	// basis). Zero values skip that dimension.
	PodRequestCPUCores    float64
	PodRequestMemoryBytes float64
	// Eligible node allocatable capacity; zero values skip that
	// dimension.
	AllocatableCPUCores    float64
	AllocatableMemoryBytes float64
	// Namespace pods quota (0 = no quota known).
	QuotaMaxPods int32
}

// ActionRecord is one recorded scaling action from the workload's
// scaling history (Algorithm 6 consumes this history; v1 keeps it in
// the LLMAutoscaler status).
type ActionRecord struct {
	Timestamp    time.Time
	Action       string
	FromReplicas int32
	ToReplicas   int32
	DryRun       bool
}

// Recommendation is a proposal from the reasoning engine.
type Recommendation struct {
	Action         string
	CurrentReplicas int32
	ProposedTarget int32
}

// Verdict is the deterministic result of the safety evaluation,
// bound to the checked target and explicit reasons.
type Verdict struct {
	Verdict      string
	CheckedTarget int32
	Reasons      []string
	PolicyChecks []string // audit lines, one per check
}

// Evaluate applies every hard constraint to the recommendation and
// returns the verdict. Order follows Algorithm 6: validation, then
// no-op, then temporal checks, then numeric feasibility.
func Evaluate(rec Recommendation, c Constraints, history []ActionRecord, now time.Time) Verdict {
	v := Verdict{}

	// --- Prerequisite validation ---
	if rec.Action != ActionScaleUp && rec.Action != ActionScaleDown && rec.Action != ActionHold {
		v.Verdict = VerdictRejected
		v.Reasons = append(v.Reasons, fmt.Sprintf("unknown action %q", rec.Action))
		return v
	}
	if rec.ProposedTarget < 0 {
		v.Verdict = VerdictRejected
		v.Reasons = append(v.Reasons, fmt.Sprintf("negative proposed target %d", rec.ProposedTarget))
		return v
	}
	if c.MinReplicas < 1 {
		v.Verdict = VerdictRejected
		v.Reasons = append(v.Reasons, "min_replicas must be at least 1; scale-to-zero is not supported")
		return v
	}
	if c.MaxReplicas < c.MinReplicas {
		v.Verdict = VerdictRejected
		v.Reasons = append(v.Reasons, fmt.Sprintf("max_replicas %d is below min_replicas %d", c.MaxReplicas, c.MinReplicas))
		return v
	}
	v.PolicyChecks = append(v.PolicyChecks, "prerequisites: pass")

	// --- HOLD never mutates ---
	if rec.Action == ActionHold {
		v.Verdict = VerdictHeld
		v.CheckedTarget = rec.CurrentReplicas
		v.Reasons = append(v.Reasons, "HOLD recommendation: no scaling write")
		return v
	}

	// --- No-op proposals are held, not approved ---
	if rec.ProposedTarget == rec.CurrentReplicas {
		v.Verdict = VerdictHeld
		v.CheckedTarget = rec.CurrentReplicas
		v.Reasons = append(v.Reasons, "target equals current replica count; nothing to change")
		return v
	}

	// --- Temporal checks over the recorded history ---
	// Only executed, non-HOLD actions consume cooldown and rate
	// budget (Algorithm 7: dry-run records no budget).
	var lastAction *ActionRecord
	mutationsInHour := 0
	windowStart := now.Add(-time.Duration(c.StabilizationWindowSeconds) * time.Second)
	highestInWindow := int32(0)
	for i := range history {
		a := &history[i]
		if a.DryRun || a.Action == ActionHold {
			continue
		}
		if lastAction == nil || a.Timestamp.After(lastAction.Timestamp) {
			lastAction = a
		}
		if c.MaxActionsPerHour > 0 && now.Sub(a.Timestamp) <= time.Hour {
			mutationsInHour++
		}
		if c.StabilizationWindowSeconds > 0 && a.Timestamp.After(windowStart) && a.ToReplicas > highestInWindow {
			highestInWindow = a.ToReplicas
		}
	}

	if c.CooldownSeconds > 0 && lastAction != nil {
		elapsed := now.Sub(lastAction.Timestamp)
		if elapsed < time.Duration(c.CooldownSeconds)*time.Second {
			v.Verdict = VerdictHeld
			v.CheckedTarget = rec.CurrentReplicas
			v.Reasons = append(v.Reasons, fmt.Sprintf(
				"cooldown: last action %.0fs ago, cooldown requires %ds",
				elapsed.Seconds(), c.CooldownSeconds))
			v.PolicyChecks = append(v.PolicyChecks, "cooldown: blocked")
			return v
		}
	}
	v.PolicyChecks = append(v.PolicyChecks, "cooldown: pass")

	if c.MaxActionsPerHour > 0 && mutationsInHour >= int(c.MaxActionsPerHour) {
		v.Verdict = VerdictHeld
		v.CheckedTarget = rec.CurrentReplicas
		v.Reasons = append(v.Reasons, fmt.Sprintf(
			"rate limit: %d actions in the last hour, limit is %d",
			mutationsInHour, c.MaxActionsPerHour))
		v.PolicyChecks = append(v.PolicyChecks, "rate limit: blocked")
		return v
	}
	v.PolicyChecks = append(v.PolicyChecks, "rate limit: pass")

	if rec.Action == ActionScaleDown && c.StabilizationWindowSeconds > 0 && highestInWindow > rec.ProposedTarget {
		v.Verdict = VerdictHeld
		v.CheckedTarget = rec.CurrentReplicas
		v.Reasons = append(v.Reasons, fmt.Sprintf(
			"downscale stabilization: highest recorded target %d in the last %ds exceeds proposed %d",
			highestInWindow, c.StabilizationWindowSeconds, rec.ProposedTarget))
		v.PolicyChecks = append(v.PolicyChecks, "stabilization: blocked")
		return v
	}
	v.PolicyChecks = append(v.PolicyChecks, "stabilization: pass")

	// --- Health and capacity checks (Algorithm 6) ---
	// A reduction on an unhealthy workload (ready below desired, or
	// pods pending/terminating) is held until the workload settles.
	if rec.Action == ActionScaleDown && !c.Healthy {
		v.Verdict = VerdictHeld
		v.CheckedTarget = rec.CurrentReplicas
		v.Reasons = append(v.Reasons,
			"downscale health: workload not fully healthy (ready below desired or pods pending/terminating)")
		v.PolicyChecks = append(v.PolicyChecks, "health: blocked")
		return v
	}
	v.PolicyChecks = append(v.PolicyChecks, "health: pass")

	// Quota: the checked target's pods must fit the namespace quota.
	if c.QuotaMaxPods > 0 && rec.ProposedTarget > c.QuotaMaxPods {
		v.Verdict = VerdictRejected
		v.CheckedTarget = rec.CurrentReplicas
		v.Reasons = append(v.Reasons, fmt.Sprintf(
			"quota: target %d exceeds the namespace pods quota of %d",
			rec.ProposedTarget, c.QuotaMaxPods))
		v.PolicyChecks = append(v.PolicyChecks, "quota: rejected")
		return v
	}
	v.PolicyChecks = append(v.PolicyChecks, "quota: pass")

	// Capacity: during a rollout old and new pods overlap, so the
	// peak demand is max(current, target) pods times the declared
	// per-pod requests; it must fit the eligible node allocatable
	// capacity. Unknown dimensions are skipped with an audit note.
	if rec.Action == ActionScaleUp {
		peak := int64(math.Max(float64(rec.CurrentReplicas), float64(rec.ProposedTarget)))
		if c.PodRequestCPUCores > 0 && c.AllocatableCPUCores > 0 {
			if float64(peak)*c.PodRequestCPUCores > c.AllocatableCPUCores {
				v.Verdict = VerdictRejected
				v.CheckedTarget = rec.CurrentReplicas
				v.Reasons = append(v.Reasons, fmt.Sprintf(
					"capacity: peak %d pods x %.3f CPU cores exceeds allocatable %.3f cores",
					peak, c.PodRequestCPUCores, c.AllocatableCPUCores))
				v.PolicyChecks = append(v.PolicyChecks, "capacity: rejected")
				return v
			}
		}
		if c.PodRequestMemoryBytes > 0 && c.AllocatableMemoryBytes > 0 {
			if float64(peak)*c.PodRequestMemoryBytes > c.AllocatableMemoryBytes {
				v.Verdict = VerdictRejected
				v.CheckedTarget = rec.CurrentReplicas
				v.Reasons = append(v.Reasons, fmt.Sprintf(
					"capacity: peak %d pods x %.0f bytes memory exceeds allocatable %.0f bytes",
					peak, c.PodRequestMemoryBytes, c.AllocatableMemoryBytes))
				v.PolicyChecks = append(v.PolicyChecks, "capacity: rejected")
				return v
			}
		}
		if c.PodRequestCPUCores <= 0 || c.AllocatableCPUCores <= 0 ||
			c.PodRequestMemoryBytes <= 0 || c.AllocatableMemoryBytes <= 0 {
			v.PolicyChecks = append(v.PolicyChecks, "capacity: unknown (skipped)")
		} else {
			v.PolicyChecks = append(v.PolicyChecks, "capacity: pass")
		}
	}

	// --- Numeric feasibility ---
	// Percentage allowances round downward (Algorithm 6); a zero
	// allowance blocks movement rather than silently permitting more.
	// MinStepReplicas is the explicit policy choice that funds a
	// minimum step when the strict floor would round to zero.
	minStep := int32(0)
	if c.MinStepReplicas > 0 {
		minStep = c.MinStepReplicas
	}
	lo, hi := c.MinReplicas, c.MaxReplicas
	if rec.Action == ActionScaleUp && c.MaxScaleUpPercent > 0 {
		allowance := int32(math.Floor(float64(rec.CurrentReplicas) * float64(c.MaxScaleUpPercent) / 100))
		if allowance < minStep {
			allowance = minStep
		}
		if allowance == 0 {
			v.Verdict = VerdictHeld
			v.CheckedTarget = rec.CurrentReplicas
			v.Reasons = append(v.Reasons, fmt.Sprintf(
				"scale-up limit %d%% of %d rounds down to zero replicas of movement",
				c.MaxScaleUpPercent, rec.CurrentReplicas))
			return v
		}
		if cap := rec.CurrentReplicas + allowance; cap < hi {
			hi = cap
		}
	}
	if rec.Action == ActionScaleDown && c.MaxScaleDownPercent > 0 {
		allowance := int32(math.Floor(float64(rec.CurrentReplicas) * float64(c.MaxScaleDownPercent) / 100))
		if allowance < minStep {
			allowance = minStep
		}
		if allowance == 0 {
			v.Verdict = VerdictHeld
			v.CheckedTarget = rec.CurrentReplicas
			v.Reasons = append(v.Reasons, fmt.Sprintf(
				"scale-down limit %d%% of %d rounds down to zero replicas of movement",
				c.MaxScaleDownPercent, rec.CurrentReplicas))
			return v
		}
		if floor := rec.CurrentReplicas - allowance; floor > lo {
			lo = floor
		}
	}
	v.PolicyChecks = append(v.PolicyChecks, fmt.Sprintf("feasible range: [%d, %d]", lo, hi))

	target := rec.ProposedTarget
	clamped := false
	if target < lo || target > hi {
		if !c.ClampEnabled {
			v.Verdict = VerdictRejected
			v.CheckedTarget = rec.CurrentReplicas
			v.Reasons = append(v.Reasons, fmt.Sprintf(
				"target %d outside feasible range [%d, %d] and clamping is disabled",
				target, lo, hi))
			v.PolicyChecks = append(v.PolicyChecks, "bounds: rejected")
			return v
		}
		// Clamp to the nearest feasible integer (Algorithm 6).
		if target < lo {
			target = lo
		} else {
			target = hi
		}
		clamped = true
		v.PolicyChecks = append(v.PolicyChecks, fmt.Sprintf("bounds: clamped to %d", target))

		// Clamping must not reverse the proposed direction.
		if (rec.Action == ActionScaleUp && target < rec.CurrentReplicas) ||
			(rec.Action == ActionScaleDown && target > rec.CurrentReplicas) {
			v.Verdict = VerdictRejected
			v.CheckedTarget = rec.CurrentReplicas
			v.Reasons = append(v.Reasons, fmt.Sprintf(
				"clamping target %d would reverse the proposed %s",
				target, rec.Action))
			return v
		}
	} else {
		v.PolicyChecks = append(v.PolicyChecks, "bounds: pass")
	}

	// After clamping the movement may have vanished.
	if target == rec.CurrentReplicas {
		v.Verdict = VerdictHeld
		v.CheckedTarget = rec.CurrentReplicas
		v.Reasons = append(v.Reasons, "feasible range prevents any movement from the current count")
		return v
	}

	if clamped {
		v.Verdict = VerdictClamped
	} else {
		v.Verdict = VerdictApproved
	}
	v.CheckedTarget = target
	v.Reasons = append(v.Reasons, fmt.Sprintf(
		"%s %d -> %d permitted by all hard constraints",
		rec.Action, rec.CurrentReplicas, target))
	return v
}

