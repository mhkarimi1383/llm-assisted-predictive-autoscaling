package operator

import (
	"testing"
	"time"

	autoscalingaiv1alpha1 "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func op(phase string, from, target int32) *autoscalingaiv1alpha1.PendingOperation {
	now := metav1.NewTime(time.Now().Add(-30 * time.Second))
	deadline := metav1.NewTime(time.Now().Add(5 * time.Minute))
	return &autoscalingaiv1alpha1.PendingOperation{
		OpID: "op-1", RequestID: "req-1", Phase: phase,
		Action: "SCALE_UP", FromReplicas: from, TargetReplicas: target,
		SubmittedAt: now, ObserveDeadline: deadline, DeploymentUID: "uid-1",
	}
}

func TestObserveUncertainResolution(t *testing.T) {
	now := time.Now()
	window := 60 * time.Second

	// The write landed (desired == target): continue observing.
	d := ObserveOperation(op(autoscalingaiv1alpha1.OpPhaseUncertain, 4, 6),
		WorkloadObservation{DesiredReplicas: 6, ReadyReplicas: 2, DeploymentUID: "uid-1"},
		4, now, window)
	if d.Outcome != OutcomePending || d.NewPhase != autoscalingaiv1alpha1.OpPhasePending || !d.ResolvedSubmitted {
		t.Fatalf("uncertain+landed must resolve to PENDING, got %+v", d)
	}

	// The write never landed (desired == from): release.
	d = ObserveOperation(op(autoscalingaiv1alpha1.OpPhaseUncertain, 4, 6),
		WorkloadObservation{DesiredReplicas: 4, ReadyReplicas: 4, DeploymentUID: "uid-1"},
		4, now, window)
	if d.Outcome != OutcomeDegraded || !d.ClearPending || d.ResolvedSubmitted {
		t.Fatalf("uncertain+not-landed must release, got %+v", d)
	}

	// Ambiguous (another actor moved the workload): degraded.
	d = ObserveOperation(op(autoscalingaiv1alpha1.OpPhaseUncertain, 4, 6),
		WorkloadObservation{DesiredReplicas: 9, ReadyReplicas: 0, DeploymentUID: "uid-1"},
		4, now, window)
	if d.Outcome != OutcomeDegraded || !d.ClearPending {
		t.Fatalf("ambiguous must be degraded, got %+v", d)
	}
}

func TestObserveIdentityConflict(t *testing.T) {
	d := ObserveOperation(op(autoscalingaiv1alpha1.OpPhasePending, 4, 6),
		WorkloadObservation{DesiredReplicas: 6, ReadyReplicas: 6, DeploymentUID: "uid-CHANGED"},
		4, time.Now(), 60*time.Second)
	if d.Outcome != OutcomeDegraded || !d.ClearPending || d.RecoveryRequested {
		t.Fatalf("replaced workload must be terminal without recovery, got %+v", d)
	}
}

func TestObserveVerificationWindow(t *testing.T) {
	window := 60 * time.Second
	now := time.Now()
	o := op(autoscalingaiv1alpha1.OpPhasePending, 4, 6)

	// First healthy observation: interval starts, keep pending.
	d := ObserveOperation(o,
		WorkloadObservation{DesiredReplicas: 6, ReadyReplicas: 6, DeploymentUID: "uid-1"},
		4, now, window)
	if d.Outcome != OutcomePending || d.VerifiedSince == nil {
		t.Fatalf("first healthy observation must start the interval, got %+v", d)
	}

	// Window elapsed since the first healthy observation: applied.
	verified := metav1.NewTime(now.Add(-window - time.Second))
	o.VerifiedSince = &verified
	d = ObserveOperation(o,
		WorkloadObservation{DesiredReplicas: 6, ReadyReplicas: 6, DeploymentUID: "uid-1"},
		4, now, window)
	if d.Outcome != OutcomeApplied || !d.ClearPending {
		t.Fatalf("verified window complete must APPLY, got %+v", d)
	}
}

func TestObserveHealthResetsInterval(t *testing.T) {
	window := 60 * time.Second
	now := time.Now()
	o := op(autoscalingaiv1alpha1.OpPhasePending, 4, 6)
	vs := metav1.NewTime(now.Add(-30 * time.Second)) // 30s healthy so far
	o.VerifiedSince = &vs

	// A pod crash resets the continuous interval.
	d := ObserveOperation(o,
		WorkloadObservation{DesiredReplicas: 6, ReadyReplicas: 3, DeploymentUID: "uid-1"},
		4, now, window)
	if d.Outcome != OutcomePending {
		t.Fatalf("unhealthy mid-window stays PENDING, got %+v", d)
	}
	if o.VerifiedSince != nil {
		t.Fatal("health regression must reset the continuous interval")
	}
}

func TestObserveDeadlineAndRecovery(t *testing.T) {
	now := time.Now()
	o := op(autoscalingaiv1alpha1.OpPhasePending, 4, 6)
	o.ObserveDeadline = metav1.NewTime(now.Add(-time.Second)) // expired

	// No last verified healthy state: degraded without recovery.
	d := ObserveOperation(o,
		WorkloadObservation{DesiredReplicas: 6, ReadyReplicas: 1, DeploymentUID: "uid-1"},
		0, now, 60*time.Second)
	if d.Outcome != OutcomeDegraded || d.RecoveryRequested {
		t.Fatalf("no recovery without a verified baseline, got %+v", d)
	}

	// Verified baseline 10 > desired 6: recovery SCALE_UP to 10.
	d = ObserveOperation(o,
		WorkloadObservation{DesiredReplicas: 6, ReadyReplicas: 1, DeploymentUID: "uid-1"},
		10, now, 60*time.Second)
	if !d.RecoveryRequested || d.RecoveryAction != "SCALE_UP" || d.RecoveryTarget != 10 {
		t.Fatalf("recovery toward last verified healthy expected, got %+v", d)
	}

	// Recovery operations never chain.
	o2 := op(autoscalingaiv1alpha1.OpPhaseRecovery, 4, 6)
	o2.ObserveDeadline = metav1.NewTime(now.Add(-time.Second))
	d = ObserveOperation(o2,
		WorkloadObservation{DesiredReplicas: 6, ReadyReplicas: 1, DeploymentUID: "uid-1"},
		10, now, 60*time.Second)
	if d.Outcome != OutcomeDegraded || d.RecoveryRequested {
		t.Fatalf("recovery ops must not request further recovery, got %+v", d)
	}
}

func TestObserveRecoveryVerified(t *testing.T) {
	o := op(autoscalingaiv1alpha1.OpPhaseRecovery, 4, 10)
	vs := metav1.NewTime(time.Now().Add(-2 * time.Minute))
	o.VerifiedSince = &vs
	d := ObserveOperation(o,
		WorkloadObservation{DesiredReplicas: 10, ReadyReplicas: 10, DeploymentUID: "uid-1"},
		4, time.Now(), 60*time.Second)
	if d.Outcome != OutcomeRecovered || !d.ClearPending {
		t.Fatalf("verified recovery must be RECOVERED, got %+v", d)
	}
}
