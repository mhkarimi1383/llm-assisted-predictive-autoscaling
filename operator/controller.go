// LLMAutoscaler reconciler: the control cycle (Algorithm 1).
//
// One reconcile runs one decision cycle for one workload:
//
//	ownership lock (Lease) -> pending-operation observation
//	(Algorithm 9, when one is in flight) -> snapshot (Algorithm 2)
//	-> reasoning (Algorithms 3-5) -> safety evaluation (Algorithm 6,
//	with capacity, quota, and health checks) -> budget reservation
//	(persisted intent) -> one checked execution (Algorithm 7,
//	dry-run aware) -> status and history recording.
package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingaiv1alpha1 "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/operator/api/v1alpha1"

	dataacpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/dataacquisition"
	multianalreasoningpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/multianalreasoning"
	safetypolicypb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/safetypolicy"
	scalingexecutorpb "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/protos/scalingexecutor"

	helpers "github.com/mhkarimi1383/llm-assisted-predictive-autoscaling/helpers"
)

// maxRecentActions bounds the history kept in the status.
const maxRecentActions = 50

// cycleTimeout bounds one whole decision cycle.
const cycleTimeout = 90 * time.Second

// observeRequeue is the cadence of Algorithm 9 observation cycles.
const observeRequeue = 15 * time.Second

// LLMAutoscalerReconciler reconciles an LLMAutoscaler object.
type LLMAutoscalerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Services *helpers.ServiceClients
	// Ownership holds the per-workload scaling Lease (Algorithm 1).
	Ownership *ScalingOwnership
	// RequeueAfter is the control period between decision cycles.
	RequeueAfter time.Duration
}

// +kubebuilder:rbac:groups=autoscaling.karimi.dev,resources=llmautoscalers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=autoscaling.karimi.dev,resources=llmautoscalers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments/scale,verbs=get;update;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;create;update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile runs one decision cycle for the requested workload.
func (r *LLMAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("llmautoscaler", req.NamespacedName)

	// Cycle deadline (Algorithm 1: every stage respects the cycle
	// deadline).
	cycleCtx, cancel := context.WithTimeout(ctx, cycleTimeout)
	defer cancel()

	var scaler autoscalingaiv1alpha1.LLMAutoscaler
	if err := r.Get(ctx, req.NamespacedName, &scaler); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !scaler.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	requeue := r.RequeueAfter
	if requeue <= 0 {
		requeue = 60 * time.Second
	}

	// --- Target workload (Algorithm 1: ownership/identity) ---
	target := scaler.Spec.ScaleTargetRef
	var deployment appsv1.Deployment
	depKey := types.NamespacedName{Namespace: scaler.Namespace, Name: target.Name}
	if err := r.Get(ctx, depKey, &deployment); err != nil {
		logger.Error(err, "target Deployment not readable", "deployment", depKey)
		_ = r.updateCondition(ctx, &scaler, "TargetMissing", metav1.ConditionFalse,
			"TargetNotFound", fmt.Sprintf("target Deployment %q not found", target.Name))
		return ctrl.Result{RequeueAfter: requeue}, client.IgnoreNotFound(err)
	}

	requestID := fmt.Sprintf("%s-%d", scaler.Name, time.Now().Unix())
	status := scaler.Status.DeepCopy()
	status.ObservedGeneration = scaler.Generation
	currentReplicas := deref32(deployment.Spec.Replicas)
	status.CurrentReplicas = &currentReplicas

	// --- Algorithm 9: observe an in-flight operation before any new
	// decision cycle (Algorithm 1: pending operations first) ---
	if op := status.PendingOperation; op != nil {
		return r.observePending(cycleCtx, &scaler, &deployment, status, op, logger)
	}

	// --- Scaling ownership lock (Algorithm 1) ---
	acquired, err := r.Ownership.Acquire(ctx, scaler.Namespace, target.Name)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ownership lease: %w", err)
	}
	if !acquired {
		logger.Info("scaling ownership held by another operator; holding")
		status.LastRecommendation = autoscalingaiv1alpha1.RecommendationStatus{
			RequestID:              requestID,
			Action:                 autoscalingaiv1alpha1.ActionHold,
			Target:                 currentReplicas,
			NoRecommendationReason: "ownership conflict: another operator instance holds the scaling lease",
		}
		status.LastAction = autoscalingaiv1alpha1.ActionHold
		if err := r.commitStatus(ctx, &scaler, status); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	snapshot, snapErr := r.fetchSnapshot(cycleCtx, &scaler, &deployment)

	var recommendation *multianalreasoningpb.ScalingRecommendation
	var votes []autoscalingaiv1alpha1.AnalystVote
	cycleErr := snapErr
	if cycleErr != nil {
		logger.Error(cycleErr, "snapshot collection failed; falling back to HOLD")
		status.LastRecommendation = autoscalingaiv1alpha1.RecommendationStatus{
			RequestID:              requestID,
			Action:                 autoscalingaiv1alpha1.ActionHold,
			Target:                 deref(status.CurrentReplicas),
			NoRecommendationReason: fmt.Sprintf("snapshot unavailable: %v", cycleErr),
		}
		status.LastAction = autoscalingaiv1alpha1.ActionHold
	} else {
		var reasonErr error
		recommendation, votes, reasonErr = r.runReasoning(cycleCtx, &scaler, snapshot)
		if reasonErr != nil {
			cycleErr = reasonErr
			status.LastRecommendation = autoscalingaiv1alpha1.RecommendationStatus{
				RequestID:              requestID,
				Action:                 autoscalingaiv1alpha1.ActionHold,
				Target:                 deref(status.CurrentReplicas),
				NoRecommendationReason: fmt.Sprintf("reasoning unavailable: %v", reasonErr),
			}
			status.LastAction = autoscalingaiv1alpha1.ActionHold
		} else {
			status.LastRecommendation = autoscalingaiv1alpha1.RecommendationStatus{
				RequestID:              requestID,
				Action:                 recommendation.GetAction(),
				Target:                 recommendation.GetTarget(),
				Confidence:             recommendation.GetConfidence(),
				Rationale:              recommendation.GetRationale(),
				AggregationSummary:     recommendation.GetAggregationSummary(),
				Votes:                  votes,
				NoRecommendationReason: recommendation.GetNoRecommendationReason(),
			}
		}
	}

	// --- No usable recommendation -> hold and requeue (Algorithm 5/8) ---
	if cycleErr != nil || !recommendation.GetHasRecommendation() {
		if cycleErr == nil {
			status.LastAction = autoscalingaiv1alpha1.ActionHold
			logger.Info("no usable recommendation; holding",
				"reason", recommendation.GetNoRecommendationReason())
		}
		if err := r.commitStatus(ctx, &scaler, status); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: requeue}, nil
	}

	action := recommendation.GetAction()

	// --- HOLD with a valid recommendation ---
	if action == autoscalingaiv1alpha1.ActionHold {
		status.LastAction = autoscalingaiv1alpha1.ActionHold
		if err := r.commitStatus(ctx, &scaler, status); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("recommendation is HOLD", "target", recommendation.GetTarget())
		return ctrl.Result{RequeueAfter: requeue}, nil
	}

	// --- Deterministic safety evaluation (Algorithm 6) ---
	verdict, err := r.evaluateSafety(cycleCtx, &scaler, requestID, recommendation, deref(status.CurrentReplicas), status.RecentActions, snapshot)
	if err != nil {
		logger.Error(err, "safety evaluation failed; holding")
		status.LastRecommendation.Verdict = "ERROR"
		status.LastRecommendation.VerdictReasons = []string{fmt.Sprintf("safety evaluation unavailable: %v", err)}
		status.LastAction = autoscalingaiv1alpha1.ActionHold
		if commitErr := r.commitStatus(ctx, &scaler, status); commitErr != nil {
			return ctrl.Result{}, commitErr
		}
		return ctrl.Result{RequeueAfter: requeue}, nil
	}
	status.LastRecommendation.Verdict = verdictName(verdict.GetVerdict())
	status.LastRecommendation.VerdictReasons = append(
		append([]string{}, verdict.GetReasons()...), verdict.GetPolicyChecks()...)

	switch verdict.GetVerdict() {
	case safetypolicypb.ScalingVerdict_VERDICT_APPROVED, safetypolicypb.ScalingVerdict_VERDICT_CLAMPED:
		// proceed to execution
	case safetypolicypb.ScalingVerdict_VERDICT_HELD, safetypolicypb.ScalingVerdict_VERDICT_REJECTED:
		logger.Info("safety verdict blocks the recommendation",
			"verdict", verdictName(verdict.GetVerdict()), "reasons", verdict.GetReasons())
		status.LastAction = autoscalingaiv1alpha1.ActionHold
		if err := r.commitStatus(ctx, &scaler, status); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: requeue}, nil
	default:
		logger.Info("unknown safety verdict; holding", "verdict", verdict.GetVerdict())
		status.LastAction = autoscalingaiv1alpha1.ActionHold
		if err := r.commitStatus(ctx, &scaler, status); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: requeue}, nil
	}

	// --- Action budget reservation (Algorithm 7) ---
	// Persist the operation intent in the status BEFORE any write.
	// The status update is atomic and, with the ownership lease held,
	// serializes overlapping cycles: a released reservation never
	// becomes an action record, so cooldown and rate budget are
	// correctly freed on failure.
	now := metav1.Now()
	var pending *autoscalingaiv1alpha1.PendingOperation
	if !scaler.Spec.DryRun {
		observeTimeout := time.Duration(scaler.Spec.Safety.ObservationTimeoutSeconds) * time.Second
		if observeTimeout <= 0 {
			observeTimeout = 300 * time.Second
		}
		pending = &autoscalingaiv1alpha1.PendingOperation{
			OpID:           fmt.Sprintf("op-%s-%d", scaler.Name, now.UnixNano()),
			RequestID:      requestID,
			Phase:          autoscalingaiv1alpha1.OpPhasePending,
			Action:         action,
			FromReplicas:   deref(status.CurrentReplicas),
			TargetReplicas: verdict.GetCheckedTarget(),
			SubmittedAt:    now,
			ObserveDeadline: metav1.NewTime(now.Add(observeTimeout)),
			DeploymentUID:  string(deployment.UID),
		}
		status.PendingOperation = pending
		if err := r.commitStatus(ctx, &scaler, status); err != nil {
			return ctrl.Result{}, err
		}
	}

	// --- One checked execution step (Algorithm 7) ---
	result, err := r.Services.ScalingExecutor.ExecuteScaling(cycleCtx, &scalingexecutorpb.ScalingDecision{
		ResourceType: "deployment",
		ResourceName: target.Name,
		Namespace:    scaler.Namespace,
		ReplicaCount: verdict.GetCheckedTarget(),
		DryRun:       scaler.Spec.DryRun,
		RequestId:    requestID,
	})
	if err != nil {
		// Transport-level uncertainty: the write may or may not have
		// landed. Keep the reservation; the next cycle reconciles it
		// against workload state (Algorithm 9) before anything else.
		logger.Error(err, "scaling submission uncertain; deferring to reconciliation")
		status.PendingOperation.Phase = autoscalingaiv1alpha1.OpPhaseUncertain
		status.LastRecommendation.VerdictReasons = append(status.LastRecommendation.VerdictReasons,
			fmt.Sprintf("submission uncertain: %v", err))
		status.LastAction = autoscalingaiv1alpha1.ActionHold
		if err := r.commitStatus(ctx, &scaler, status); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: observeRequeue}, nil
	}
	if !result.GetSuccess() {
		// Definitive failure: release the unused reservation.
		reason := result.GetMessage()
		logger.Info("scaling execution failed; reservation released", "message", reason)
		status.PendingOperation = nil
		status.LastRecommendation.VerdictReasons = append(status.LastRecommendation.VerdictReasons,
			fmt.Sprintf("execution failed: %s", reason))
		status.LastAction = autoscalingaiv1alpha1.ActionHold
		if err := r.commitStatus(ctx, &scaler, status); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: requeue}, nil
	}

	// Success: consume the reservation (the action record IS the
	// consumed budget) and keep the operation under observation
	// (Algorithm 9), unless this was a dry run.
	status.LastAction = action
	status.LastScaleTime = &now
	status.RecentActions = append(status.RecentActions, autoscalingaiv1alpha1.ActionRecord{
		Time:         now,
		Action:       action,
		FromReplicas: result.GetFromReplicas(),
		ToReplicas:   result.GetToReplicas(),
		RequestID:    requestID,
		DryRun:       scaler.Spec.DryRun,
	})
	if len(status.RecentActions) > maxRecentActions {
		status.RecentActions = status.RecentActions[len(status.RecentActions)-maxRecentActions:]
	}
	if scaler.Spec.DryRun {
		// A dry run never submits: no pending operation to observe.
		status.PendingOperation = nil
	}

	logger.Info("scaling step executed",
		"action", action, "from", result.GetFromReplicas(), "to", result.GetToReplicas(),
		"dry_run", result.GetDryRun(), "verdict", verdictName(verdict.GetVerdict()),
		"op", pending.OpID)

	if err := r.commitStatus(ctx, &scaler, status); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: observeRequeue}, nil
}

// fetchSnapshot implements Algorithm 2 (collect and validate a
// decision snapshot) through the Data Acquisition service.
func (r *LLMAutoscalerReconciler) fetchSnapshot(
	ctx context.Context,
	scaler *autoscalingaiv1alpha1.LLMAutoscaler,
	deployment *appsv1.Deployment,
) (*dataacpb.WorkloadSnapshot, error) {
	req := &dataacpb.WorkloadSnapshotRequest{
		Namespace:              scaler.Namespace,
		ResourceName:           scaler.Spec.ScaleTargetRef.Name,
		ForecastHorizonSeconds: 900,
		MetricsProvider:        scaler.Spec.Metrics.Provider,
		PrometheusUrl:          scaler.Spec.Metrics.PrometheusURL,
		ForecastProvider:       scaler.Spec.Forecast.Provider,
		ForecastUrl:            scaler.Spec.Forecast.RESTURL,
	}
	snap, err := r.Services.DataAcquisition.GetWorkloadSnapshot(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("GetWorkloadSnapshot RPC failed: %w", err)
	}
	// Identity and freshness validation (Algorithm 2): the snapshot
	// must describe the workload we just read.
	if snap.GetNamespace() != scaler.Namespace || snap.GetResourceName() != scaler.Spec.ScaleTargetRef.Name {
		return nil, fmt.Errorf("snapshot identity mismatch: %s/%s", snap.GetNamespace(), snap.GetResourceName())
	}
	if snap.GetDesiredReplicas() != deref32(deployment.Spec.Replicas) {
		return nil, fmt.Errorf("snapshot desired replicas %d does not match deployment %d",
			snap.GetDesiredReplicas(), deref32(deployment.Spec.Replicas))
	}
	if snap.GetMetricsError() != "" && snap.GetForecastError() != "" {
		return nil, fmt.Errorf("all observation sources failed: metrics=%s forecast=%s",
			snap.GetMetricsError(), snap.GetForecastError())
	}
	return snap, nil
}

// runReasoning implements Algorithms 3-5 through the Multi-Agent
// Reasoning service.
func (r *LLMAutoscalerReconciler) runReasoning(
	ctx context.Context,
	scaler *autoscalingaiv1alpha1.LLMAutoscaler,
	snap *dataacpb.WorkloadSnapshot,
) (*multianalreasoningpb.ScalingRecommendation, []autoscalingaiv1alpha1.AnalystVote, error) {
	snapshotJSON, err := snapshotToJSON(scaler, snap)
	if err != nil {
		return nil, nil, err
	}

	apiKey, err := r.resolveAPIKey(ctx, scaler)
	if err != nil {
		// A referenced but unreadable secret is a configuration
		// error; reasoning proceeds without the key so the failure is
		// observable in the analyst results.
		apiKey = ""
	}

	spec := scaler.Spec
	cfg := map[string]any{
		"min_replicas":         spec.MinReplicas,
		"max_replicas":         spec.MaxReplicas,
		"quorum":               spec.Reasoning.Quorum,
		"confidence_threshold": spec.Reasoning.ConfidenceThreshold,
		"analyst_weights": map[string]float64{
			"forecast": spec.Reasoning.AnalystWeights.Forecast,
			"status":   spec.Reasoning.AnalystWeights.Status,
			"policy":   spec.Reasoning.AnalystWeights.Policy,
			"history":  spec.Reasoning.AnalystWeights.History,
		},
		"reasoning_timeout_seconds": spec.Reasoning.TimeoutSeconds,
		"llm": map[string]string{
			"endpoint": spec.LLM.Endpoint,
			"model":    spec.LLM.Model,
			"api_key":  apiKey,
		},
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, nil, err
	}

	resp, err := r.Services.MultiAgentReasoning.ReasonScaling(ctx, &multianalreasoningpb.ReasonScalingRequest{
		RequestId:    fmt.Sprintf("%s-%d", scaler.Name, time.Now().Unix()),
		SnapshotJson: string(snapshotJSON),
		ConfigJson:   string(cfgJSON),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("ReasonScaling RPC failed: %w", err)
	}

	votes := make([]autoscalingaiv1alpha1.AnalystVote, 0, len(resp.GetAnalystResults()))
	for _, a := range resp.GetAnalystResults() {
		votes = append(votes, autoscalingaiv1alpha1.AnalystVote{
			Role:             a.GetRole(),
			Action:           a.GetAction(),
			Target:           a.GetTarget(),
			Confidence:       a.GetConfidence(),
			Abstained:        a.GetAbstentionReason() != "",
			AbstentionReason: a.GetAbstentionReason(),
			Error:            a.GetError(),
			Rationale:        a.GetRationale(),
		})
	}
	return resp.GetRecommendation(), votes, nil
}

// evaluateSafety implements Algorithm 6 through the Safety Policy
// service, with the history from the resource status and the
// capacity/quota/health observations from the snapshot.
func (r *LLMAutoscalerReconciler) evaluateSafety(
	ctx context.Context,
	scaler *autoscalingaiv1alpha1.LLMAutoscaler,
	requestID string,
	rec *multianalreasoningpb.ScalingRecommendation,
	currentReplicas int32,
	history []autoscalingaiv1alpha1.ActionRecord,
	snap *dataacpb.WorkloadSnapshot,
) (*safetypolicypb.ScalingVerdictResponse, error) {
	pbHistory := make([]*safetypolicypb.ScalingActionRecord, 0, len(history))
	for _, a := range history {
		pbHistory = append(pbHistory, &safetypolicypb.ScalingActionRecord{
			Timestamp:    a.Time.Unix(),
			Action:       a.Action,
			FromReplicas: a.FromReplicas,
			ToReplicas:   a.ToReplicas,
			DryRun:       a.DryRun,
		})
	}
	s := scaler.Spec
	return r.Services.SafetyPolicy.EvaluateScaling(ctx, &safetypolicypb.EvaluateScalingRequest{
		RequestId:       requestID,
		Action:          rec.GetAction(),
		CurrentReplicas: currentReplicas,
		ProposedTarget:  rec.GetTarget(),
		Constraints: &safetypolicypb.ScalingConstraints{
			MinReplicas:                s.MinReplicas,
			MaxReplicas:                s.MaxReplicas,
			CooldownSeconds:            s.Safety.CooldownSeconds,
			MaxScaleUpPercent:          s.Safety.MaxScaleUpPercent,
			MaxScaleDownPercent:        s.Safety.MaxScaleDownPercent,
			StabilizationWindowSeconds: s.Safety.StabilizationWindowSeconds,
			MaxActionsPerHour:          s.Safety.MaxActionsPerHour,
			MinStepReplicas:            s.Safety.MinStepReplicas,
			Healthy:                    snapshotHealthy(snap),
			QuotaMaxPods:               quotaMaxPods(snap),
			PodRequestCpuCores:         snap.GetPodResources().GetCpuCores(),
			PodRequestMemoryBytes:      snap.GetPodResources().GetMemoryBytes(),
			AllocatableCpuCores:        snap.GetCapacity().GetAllocatableCpuCores(),
			AllocatableMemoryBytes:     snap.GetCapacity().GetAllocatableMemoryBytes(),
		},
		RecentActions: pbHistory,
		Now:           time.Now().Unix(),
	})
}

// resolveAPIKey reads the optional LLM API key secret.
func (r *LLMAutoscalerReconciler) resolveAPIKey(
	ctx context.Context, scaler *autoscalingaiv1alpha1.LLMAutoscaler,
) (string, error) {
	ref := scaler.Spec.LLM.APIKeySecret
	if ref == nil {
		return "", nil
	}
	var secret corev1.Secret
	key := types.NamespacedName{Namespace: scaler.Namespace, Name: ref.Name}
	if err := r.Get(ctx, key, &secret); err != nil {
		return "", fmt.Errorf("secret %q not readable: %w", ref.Name, err)
	}
	value, ok := secret.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("secret %q lacks key %q", ref.Name, ref.Key)
	}
	return string(value), nil
}

// snapshotToJSON maps the proto snapshot onto the JSON contract the
// reasoning engine consumes.
func snapshotToJSON(
	scaler *autoscalingaiv1alpha1.LLMAutoscaler,
	snap *dataacpb.WorkloadSnapshot,
) ([]byte, error) {
	history := make([]map[string]any, 0, len(scaler.Status.RecentActions))
	for _, a := range scaler.Status.RecentActions {
		history = append(history, map[string]any{
			"timestamp":     a.Time.Unix(),
			"action":        a.Action,
			"from_replicas": a.FromReplicas,
			"to_replicas":   a.ToReplicas,
			"dry_run":       a.DryRun,
		})
	}

	forecast := make([]map[string]any, 0, len(snap.GetForecast()))
	for _, f := range snap.GetForecast() {
		forecast = append(forecast, map[string]any{
			"timestamp":        f.GetTimestamp(),
			"predicted_cpu":    f.GetPredictedCpu(),
			"predicted_memory": f.GetPredictedMemory(),
			"predicted_rps":    f.GetPredictedRps(),
		})
	}

	payload := map[string]any{
		"namespace":           snap.GetNamespace(),
		"resource_name":       snap.GetResourceName(),
		"observed_at":         snap.GetObservedAt(),
		"resource_version":    snap.GetResourceVersion(),
		"desired_replicas":    snap.GetDesiredReplicas(),
		"ready_replicas":      snap.GetReadyReplicas(),
		"observed_generation": snap.GetObservedGeneration(),
		"pods": map[string]any{
			"total_pods":       snap.GetPods().GetTotalPods(),
			"ready_pods":       snap.GetPods().GetReadyPods(),
			"pending_pods":     snap.GetPods().GetPendingPods(),
			"terminating_pods": snap.GetPods().GetTerminatingPods(),
		},
		"cpu_utilization":    snap.GetCpuUtilization(),
		"memory_utilization": snap.GetMemoryUtilization(),
		"forecast":           forecast,
		"forecast_provider":  snap.GetForecastProvider(),
		"metrics_provider":   snap.GetMetricsProvider(),
		"metrics_error":      snap.GetMetricsError(),
		"forecast_error":     snap.GetForecastError(),
		"recent_actions":     history,
		"dry_run":            scaler.Spec.DryRun,
	}
	return json.Marshal(payload)
}

// commitStatus writes the accumulated status and keeps the Ready
// condition in sync with the latest verdict. No-op updates are
// skipped: a status write re-triggers reconciliation, and every
// cycle produces a fresh request ID, so without the skip the
// reconciler would loop on itself. The request ID is excluded from
// the comparison for that reason.
func (r *LLMAutoscalerReconciler) commitStatus(
	ctx context.Context,
	scaler *autoscalingaiv1alpha1.LLMAutoscaler,
	status *autoscalingaiv1alpha1.LLMAutoscalerStatus,
) error {
	old := scaler.Status.DeepCopy()
	old.LastRecommendation.RequestID = ""

	scaler.Status = *status
	ready := metav1.ConditionTrue
	reason := "CycleCompleted"
	message := "latest decision cycle recorded"
	if status.LastRecommendation.NoRecommendationReason != "" {
		ready = metav1.ConditionFalse
		reason = "NoRecommendation"
		message = status.LastRecommendation.NoRecommendationReason
	}
	meta.SetStatusCondition(&scaler.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             ready,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: scaler.Generation,
	})

	status.LastRecommendation.RequestID = ""

	if reflect.DeepEqual(*old, scaler.Status) {
		return nil
	}
	return r.Status().Update(ctx, scaler)
}

func (r *LLMAutoscalerReconciler) updateCondition(
	ctx context.Context,
	scaler *autoscalingaiv1alpha1.LLMAutoscaler,
	condType string,
	status metav1.ConditionStatus,
	reason, message string,
) error {
	meta.SetStatusCondition(&scaler.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: scaler.Generation,
	})
	return r.Status().Update(ctx, scaler)
}

// SetupWithManager registers the controller with the manager and
// watches the target Deployments of LLMAutoscaler resources.
func (r *LLMAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingaiv1alpha1.LLMAutoscaler{}).
		Complete(r)
}

// --- small helpers ---

func deref(p *int32) int32 { return deref32(p) }

func deref32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

func verdictName(v safetypolicypb.ScalingVerdict) string {
	switch v {
	case safetypolicypb.ScalingVerdict_VERDICT_APPROVED:
		return autoscalingaiv1alpha1.VerdictApproved
	case safetypolicypb.ScalingVerdict_VERDICT_CLAMPED:
		return autoscalingaiv1alpha1.VerdictClamped
	case safetypolicypb.ScalingVerdict_VERDICT_HELD:
		return autoscalingaiv1alpha1.VerdictHeld
	case safetypolicypb.ScalingVerdict_VERDICT_REJECTED:
		return autoscalingaiv1alpha1.VerdictRejected
	default:
		return "UNKNOWN"
	}
}

func errMessage(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}
