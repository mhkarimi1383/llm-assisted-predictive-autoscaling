// Observation and recovery (Algorithm 9).
//
// A submitted operation is observed across cycles: the checked
// target must stay continuously healthy for the verification
// window before it counts as applied. Uncertain submissions are
// reconciled against workload state before any other write. A
// non-converged operation is marked terminal at the observation
// deadline and one recovery step toward the last verified healthy
// count is considered — re-evaluated through the deterministic
// safety engine (Algorithm 6) like any other proposal.
package operator

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	logr "github.com/go-logr/logr"

	autoscalingaiv1alpha1 "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/operator/api/v1alpha1"
	scalingexecutorpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/scalingexecutor"
	dataacpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/dataacquisition"
	multianalreasoningpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/multianalreasoning"
	safetypolicypb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/safetypolicy"
)

// Observation outcomes (Algorithm 9).
const (
	OutcomePending   = "PENDING"   // keep observing
	OutcomeApplied   = "APPLIED"   // verified healthy; operation complete
	OutcomeRecovered = "RECOVERED" // recovery step verified healthy
	OutcomeDegraded  = "DEGRADED"  // terminal failure or blocked recovery
)

// WorkloadObservation is what a cycle sees about the target.
type WorkloadObservation struct {
	DesiredReplicas  int32
	ReadyReplicas    int32
	PendingPods      int32
	TerminatingPods  int32
	DeploymentUID    string
}

// ObserveDecision is the pure outcome of one Algorithm 9 step.
type ObserveDecision struct {
	// Outcome of this observation step.
	Outcome string

	// New phase for the kept pending operation (UNCERTAIN
	// resolution); empty when the pending op is cleared.
	NewPhase string

	// Set VerifiedSince on the pending op (continuous healthy
	// interval started/updated).
	VerifiedSince *time.Time

	// ClearPending removes the pending operation (terminal outcome
	// or uncertain-resolution failure).
	ClearPending bool

	// Resolution of an uncertain submission: true = the write landed
	// (continue observing), false = it never landed (release).
	ResolvedSubmitted bool

	// Degraded reason recorded with a terminal outcome.
	Reason string

	// Recovery proposal: when non-nil, the controller evaluates it
	// through the safety engine and executes one recovery step.
	RecoveryTarget    int32
	RecoveryAction    string
	RecoveryRequested bool
}

// ObserveOperation runs one Algorithm 9 step for a pending
// operation. It is a pure function of the observation inputs so the
// reconciliation rules stay unit-testable.
func ObserveOperation(
	op *autoscalingaiv1alpha1.PendingOperation,
	obs WorkloadObservation,
	lastVerifiedHealthy int32,
	now time.Time,
	verificationWindow time.Duration,
) ObserveDecision {
	// --- Identity/ownership conflict: workload replaced ---
	if op.DeploymentUID != "" && obs.DeploymentUID != "" && op.DeploymentUID != obs.DeploymentUID {
		return ObserveDecision{
			Outcome:      OutcomeDegraded,
			ClearPending: true,
			Reason:       "workload was replaced: deployment UID no longer matches the submitted operation",
		}
	}

	// --- Uncertain submission: reconcile before anything else ---
	if op.Phase == autoscalingaiv1alpha1.OpPhaseUncertain {
		switch {
		case obs.DesiredReplicas == op.TargetReplicas:
			// The write landed: continue under normal observation.
			return ObserveDecision{Outcome: OutcomePending, NewPhase: autoscalingaiv1alpha1.OpPhasePending, ResolvedSubmitted: true}
		case obs.DesiredReplicas == op.FromReplicas:
			// Definitively not submitted: release the reservation.
			return ObserveDecision{
				Outcome: OutcomeDegraded, ClearPending: true, ResolvedSubmitted: false,
				Reason: "uncertain submission resolved: the write never landed; reservation released",
			}
		default:
			// Another actor moved the workload: stop automatic handling.
			return ObserveDecision{
				Outcome: OutcomeDegraded, ClearPending: true, ResolvedSubmitted: false,
				Reason: "uncertain submission ambiguous: desired replicas match neither the operation nor its starting point",
			}
		}
	}

	// --- Healthy verification (Algorithm 9: continuous interval) ---
	healthy := obs.DesiredReplicas == op.TargetReplicas &&
		obs.ReadyReplicas >= op.TargetReplicas &&
		obs.PendingPods == 0 && obs.TerminatingPods == 0

	if healthy {
		verifiedSince := now
		if op.VerifiedSince != nil {
			verifiedSince = op.VerifiedSince.Time
		}
		if now.Sub(verifiedSince) >= verificationWindow {
			outcome := OutcomeApplied
			if op.Phase == autoscalingaiv1alpha1.OpPhaseRecovery {
				outcome = OutcomeRecovered
			}
			return ObserveDecision{
				Outcome:      outcome,
				ClearPending: true,
				Reason:       "checked target reached and stayed healthy across the verification window",
			}
		}
		return ObserveDecision{Outcome: OutcomePending, VerifiedSince: &verifiedSince}
	}

	// Not healthy: reset the continuous interval.
	op.VerifiedSince = nil

	// --- Observation deadline: terminal, consider recovery ---
	if !op.ObserveDeadline.IsZero() && now.After(op.ObserveDeadline.Time) {
		decision := ObserveDecision{
			Outcome:      OutcomeDegraded,
			ClearPending: true,
			Reason:       "observation deadline expired without verified convergence",
		}
		// One recovery attempt per failed operation, only for normal
		// operations with a known previous healthy state (Algorithm 9:
		// recovery steps never chain).
		if op.Phase != autoscalingaiv1alpha1.OpPhaseRecovery &&
			lastVerifiedHealthy > 0 &&
			lastVerifiedHealthy != obs.DesiredReplicas {
			decision.RecoveryRequested = true
			decision.RecoveryTarget = lastVerifiedHealthy
			if lastVerifiedHealthy > obs.DesiredReplicas {
				decision.RecoveryAction = "SCALE_UP"
			} else {
				decision.RecoveryAction = "SCALE_DOWN"
			}
		}
		return decision
	}

	return ObserveDecision{Outcome: OutcomePending}
}

// observePending runs one Algorithm 9 cycle for an in-flight
// operation: reconcile uncertain submissions, track the continuous
// healthy interval, and consider one recovery step at the deadline.
func (r *LLMAutoscalerReconciler) observePending(
	ctx context.Context,
	scaler *autoscalingaiv1alpha1.LLMAutoscaler,
	deployment *appsv1.Deployment,
	status *autoscalingaiv1alpha1.LLMAutoscalerStatus,
	op *autoscalingaiv1alpha1.PendingOperation,
	logger logr.Logger,
) (ctrl.Result, error) {
	requeue := r.RequeueAfter
	if requeue <= 0 {
		requeue = 60 * time.Second
	}

	// Unavailable pods stand in for pending+terminating ones; the
	// full pod breakdown arrives with the next snapshot anyway.
	// Keep the reported replica count fresh during observation.
	current := deref32(deployment.Spec.Replicas)
	status.CurrentReplicas = &current

	unavailable := deployment.Status.UnavailableReplicas
	obs := WorkloadObservation{
		DesiredReplicas: deref32(deployment.Spec.Replicas),
		ReadyReplicas:   deployment.Status.ReadyReplicas,
		PendingPods:     unavailable,
		TerminatingPods: 0,
		DeploymentUID:   string(deployment.UID),
	}

	decision := ObserveOperation(op, obs, deref32(status.LastVerifiedHealthyReplicas),
		time.Now(), time.Duration(scaler.Spec.Safety.VerificationWindowSeconds)*time.Second)

	// Apply mutations to the persisted pending operation.
	if decision.VerifiedSince != nil {
		t := metav1.NewTime(*decision.VerifiedSince)
		op.VerifiedSince = &t
	}
	if decision.NewPhase != "" {
		op.Phase = decision.NewPhase
	}

	switch decision.Outcome {
	case OutcomePending:
		if err := r.commitStatus(ctx, scaler, status); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: observeRequeue}, nil

	case OutcomeApplied, OutcomeRecovered:
		status.PendingOperation = nil
		verified := op.TargetReplicas
		status.LastVerifiedHealthyReplicas = &verified
		logger.Info("operation verified healthy",
			"op", op.OpID, "outcome", decision.Outcome, "replicas", verified,
			"recovery", op.Phase == autoscalingaiv1alpha1.OpPhaseRecovery)
		meta.SetStatusCondition(&status.Conditions, metav1.Condition{
			Type: "Degraded", Status: metav1.ConditionFalse,
			Reason: "OperationApplied", Message: decision.Reason,
			ObservedGeneration: scaler.Generation,
		})
		if err := r.commitStatus(ctx, scaler, status); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: requeue}, nil

	default: // OutcomeDegraded
		status.PendingOperation = nil
		logger.Info("operation degraded", "op", op.OpID, "reason", decision.Reason)
		meta.SetStatusCondition(&status.Conditions, metav1.Condition{
			Type: "Degraded", Status: metav1.ConditionTrue,
			Reason: "ObservationFailed", Message: decision.Reason,
			ObservedGeneration: scaler.Generation,
		})

		// --- One recovery step (Algorithm 9) ---
		if decision.RecoveryRequested {
			rec := &multianalreasoningpb.ScalingRecommendation{
				Action: decision.RecoveryAction,
				Target: decision.RecoveryTarget,
			}
			verdict, err := r.evaluateSafety(ctx, scaler, op.RequestID+":recovery",
				rec, deref32(deployment.Spec.Replicas), status.RecentActions, nil)
			if err != nil {
				logger.Error(err, "recovery safety evaluation failed; staying degraded")
			} else if v := verdict.GetVerdict(); v == safetypolicypb.ScalingVerdict_VERDICT_APPROVED ||
				v == safetypolicypb.ScalingVerdict_VERDICT_CLAMPED {
				recoveryOp := &autoscalingaiv1alpha1.PendingOperation{
					OpID:            fmt.Sprintf("op-%s-%d", scaler.Name, time.Now().UnixNano()),
					RequestID:       op.RequestID + ":recovery",
					Phase:           autoscalingaiv1alpha1.OpPhaseRecovery,
					Action:          decision.RecoveryAction,
					FromReplicas:    deref32(deployment.Spec.Replicas),
					TargetReplicas:  verdict.GetCheckedTarget(),
					SubmittedAt:     metav1.Now(),
					ObserveDeadline: metav1.NewTime(time.Now().Add(300 * time.Second)),
					DeploymentUID:   string(deployment.UID),
					ParentOpID:      op.OpID,
				}
				// Persist the recovery reservation before the write.
				status.PendingOperation = recoveryOp
				if err := r.commitStatus(ctx, scaler, status); err != nil {
					return ctrl.Result{}, err
				}
				result, execErr := r.Services.ScalingExecutor.ExecuteScaling(ctx, &scalingexecutorpb.ScalingDecision{
					ResourceType: "deployment",
					ResourceName: scaler.Spec.ScaleTargetRef.Name,
					Namespace:    scaler.Namespace,
					ReplicaCount: verdict.GetCheckedTarget(),
					RequestId:    recoveryOp.RequestID,
				})
				now := metav1.Now()
				if execErr != nil {
					recoveryOp.Phase = autoscalingaiv1alpha1.OpPhaseUncertain
					logger.Error(execErr, "recovery submission uncertain")
				} else if result.GetSuccess() {
					status.LastAction = decision.RecoveryAction
					status.LastScaleTime = &now
					status.RecentActions = append(status.RecentActions, autoscalingaiv1alpha1.ActionRecord{
						Time: now, Action: decision.RecoveryAction,
						FromReplicas: result.GetFromReplicas(), ToReplicas: result.GetToReplicas(),
						RequestID: recoveryOp.RequestID,
					})
					logger.Info("recovery step submitted", "op", recoveryOp.OpID,
						"to", result.GetToReplicas())
				} else {
					status.PendingOperation = nil
					logger.Info("recovery execution failed; reservation released", "message", result.GetMessage())
				}
				if len(status.RecentActions) > maxRecentActions {
					status.RecentActions = status.RecentActions[len(status.RecentActions)-maxRecentActions:]
				}
				if err := r.commitStatus(ctx, scaler, status); err != nil {
					return ctrl.Result{}, err
				}
				return ctrl.Result{RequeueAfter: observeRequeue}, nil
			} else {
				logger.Info("recovery blocked by safety", "reasons", verdict.GetReasons())
			}
		}

		if err := r.commitStatus(ctx, scaler, status); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: requeue}, nil
	}
}

// snapshotHealthy reports whether the workload was fully healthy at
// snapshot time (Algorithm 6 health check).
func snapshotHealthy(snap *dataacpb.WorkloadSnapshot) bool {
	return snap.GetMetricsError() == "" &&
		snap.GetPods().GetPendingPods() == 0 &&
		snap.GetPods().GetTerminatingPods() == 0 &&
		snap.GetReadyReplicas() >= snap.GetDesiredReplicas()
}

// quotaMaxPods returns the pods quota, or 0 when none applies.
func quotaMaxPods(snap *dataacpb.WorkloadSnapshot) int32 {
	if snap.GetQuota().GetHasQuota() {
		return snap.GetQuota().GetMaxPods()
	}
	return 0
}
