package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Action names used across the system. They match the algorithms document:
// SCALE UP, SCALE DOWN, and HOLD.
const (
	ActionScaleUp   = "SCALE_UP"
	ActionScaleDown = "SCALE_DOWN"
	ActionHold      = "HOLD"
)

// Safety verdicts produced by the deterministic safety evaluation
// (Algorithm 6 of the algorithms document).
const (
	VerdictApproved = "APPROVED"
	VerdictClamped  = "CLAMPED"
	VerdictHeld     = "HELD"
	VerdictRejected = "REJECTED"
)

// Analyst role names used by the multi-agent reasoning engine.
const (
	RoleForecast    = "forecast"
	RoleStatus      = "status"
	RolePolicy      = "policy"
	RoleHistory     = "history"
	RoleSynthesizer = "synthesizer"
)

// SecretKeySource references a key within a Secret in the same namespace
// as the LLMAutoscaler resource.
type SecretKeySource struct {
	// Name of the Secret.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Key inside the Secret that holds the API key.
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// LLMConfig points at an OpenAI-compatible chat-completions API.
// The special endpoint value "mock://" selects the deterministic mock
// provider, which requires no external service and is intended for
// tests, demos, and dry runs.
type LLMConfig struct {
	// Base URL of an OpenAI-compatible API, for example
	// "http://ollama:11434/v1" or "https://api.openai.com/v1".
	// Use "mock://" for the built-in deterministic mock provider.
	// +kubebuilder:validation:MinLength=1
	Endpoint string `json:"endpoint"`

	// Model name passed to the chat-completions API.
	// +kubebuilder:validation:MinLength=1
	Model string `json:"model"`

	// Optional reference to a Secret key holding the API key.
	APIKeySecret *SecretKeySource `json:"apiKeySecret,omitempty"`
}

// AnalystWeights assigns a voting weight to each analyst role.
// Weights are configured, never set by the model (Algorithm 5).
// All values must be zero or greater.
type AnalystWeights struct {
	// +kubebuilder:validation:Minimum=0
	Forecast float64 `json:"forecast,omitempty"`
	// +kubebuilder:validation:Minimum=0
	Status float64 `json:"status,omitempty"`
	// +kubebuilder:validation:Minimum=0
	Policy float64 `json:"policy,omitempty"`
	// +kubebuilder:validation:Minimum=0
	History float64 `json:"history,omitempty"`
}

// SafetySpec holds the deterministic hard constraints enforced by the
// safety policy engine before any scaling write (Algorithm 6).
// These constraints cannot be overridden by LLM output.
type SafetySpec struct {
	// Minimum seconds between two scaling actions (cooldown).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=120
	CooldownSeconds int32 `json:"cooldownSeconds,omitempty"`

	// Maximum replica increase per action, as a percentage of the
	// current replica count. 0 means unlimited.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=100
	MaxScaleUpPercent int32 `json:"maxScaleUpPercent,omitempty"`

	// Maximum replica decrease per action, as a percentage of the
	// current replica count. 0 means unlimited.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=25
	MaxScaleDownPercent int32 `json:"maxScaleDownPercent,omitempty"`

	// Minimum replica movement per action. Percentage limits round
	// downward (Algorithm 6); small counts can round a percentage
	// allowance down to zero, permanently blocking movement near
	// minReplicas. A positive value funds at least this many replicas
	// of movement per action. 0 keeps the strict floor behavior.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=0
	MinStepReplicas int32 `json:"minStepReplicas,omitempty"`

	// Downscale stabilization window (Algorithm 6): scale-down is
	// blocked unless the highest required target recorded within this
	// window is not higher than the proposed target.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=300
	StabilizationWindowSeconds int32 `json:"stabilizationWindowSeconds,omitempty"`

	// Maximum number of executed scaling actions per hour
	// (rate limit). 0 means unlimited.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=20
	MaxActionsPerHour int32 `json:"maxActionsPerHour,omitempty"`

	// Seconds the target must stay continuously healthy before a
	// submitted scaling operation counts as applied (Algorithm 9
	// verification window).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=60
	VerificationWindowSeconds int32 `json:"verificationWindowSeconds,omitempty"`

	// Maximum seconds a submitted operation may stay under
	// observation before it is marked terminal and recovery is
	// considered (Algorithm 9 observation deadline).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=300
	ObservationTimeoutSeconds int32 `json:"observationTimeoutSeconds,omitempty"`
}

// ReasoningSpec configures the multi-agent reasoning engine
// (Algorithms 3 to 5): analyst fanout, voting, and synthesis.
type ReasoningSpec struct {
	// Minimum number of valid analyst results required for a
	// recommendation. The forecast and status roles must always be
	// among the valid results.
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=4
	// +kubebuilder:default=3
	Quorum int32 `json:"quorum,omitempty"`

	// Minimum recommendation confidence (0 to 1) required to act on a
	// recommendation. Below it the cycle holds.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	// +kubebuilder:default=0.6
	ConfidenceThreshold float64 `json:"confidenceThreshold,omitempty"`

	// Per-role voting weights. Defaults are equal weights.
	AnalystWeights AnalystWeights `json:"analystWeights,omitempty"`

	// Deadline for the whole reasoning phase (fanout plus synthesis)
	// in seconds.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=30
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
}

// MetricsSpec selects the provider used for current utilization
// metrics (Algorithm 2, data acquisition).
type MetricsSpec struct {
	// Metrics provider: "metrics-server" reads the Kubernetes Metrics
	// API, "prometheus" queries a Prometheus server.
	// +kubebuilder:validation:Enum=metrics-server;prometheus
	// +kubebuilder:default=metrics-server
	Provider string `json:"provider,omitempty"`

	// Prometheus base URL, used when provider is "prometheus".
	PrometheusURL string `json:"prometheusUrl,omitempty"`
}

// ForecastSpec selects the workload forecast provider used as the
// predictive input of the control cycle.
type ForecastSpec struct {
	// Forecast provider: "stub" generates a synthetic demand curve
	// (useful for demos), "rest" fetches predictions from an external
	// HTTP service.
	// +kubebuilder:validation:Enum=stub;rest
	// +kubebuilder:default=stub
	Provider string `json:"provider,omitempty"`

	// Predictor URL, used when provider is "rest".
	RESTURL string `json:"restUrl,omitempty"`
}

// ScaleTargetRef identifies the workload being scaled. Only
// "Deployment" is supported in v1.
type ScaleTargetRef struct {
	// Kind of the target resource. Only "Deployment" is supported.
	// +kubebuilder:validation:Enum=Deployment
	Kind string `json:"kind"`

	// Name of the target Deployment in the same namespace.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// LLMAutoscalerSpec defines the desired state of an LLMAutoscaler:
// one managed workload, its safety constraints, and the reasoning
// configuration used for every decision cycle.
type LLMAutoscalerSpec struct {
	// Reference to the workload this autoscaler manages.
	ScaleTargetRef ScaleTargetRef `json:"scaleTargetRef"`

	// Minimum replica count. Must be at least 1; scale-to-zero is not
	// supported in v1.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	MinReplicas int32 `json:"minReplicas,omitempty"`

	// Maximum replica count.
	// +kubebuilder:validation:Minimum=1
	MaxReplicas int32 `json:"maxReplicas"`

	// LLM service configuration.
	LLM LLMConfig `json:"llm"`

	// Deterministic safety constraints.
	Safety SafetySpec `json:"safety,omitempty"`

	// Multi-agent reasoning configuration.
	Reasoning ReasoningSpec `json:"reasoning,omitempty"`

	// Utilization metrics provider.
	Metrics MetricsSpec `json:"metrics,omitempty"`

	// Workload forecast provider.
	Forecast ForecastSpec `json:"forecast,omitempty"`

	// When true, decisions are validated and recorded but no scaling
	// write is submitted (Algorithm 7 dry run).
	DryRun bool `json:"dryRun,omitempty"`
}

// AnalystVote records one analyst outcome in the resource status for
// auditability: an action with a target, an abstention, or a failure.
type AnalystVote struct {
	// Analyst role: forecast, status, policy, or history.
	Role string `json:"role"`

	// SCALE_UP, SCALE_DOWN, or HOLD. Empty when abstained or failed.
	Action string `json:"action,omitempty"`

	// Proposed replica count. Zero when abstained or failed.
	Target int32 `json:"target,omitempty"`

	// Self-reported confidence between 0 and 1.
	Confidence float64 `json:"confidence,omitempty"`

	// True when the analyst abstained (for example, the history
	// analyst without usable history).
	Abstained bool `json:"abstained,omitempty"`

	// Reason recorded with an abstention.
	AbstentionReason string `json:"abstentionReason,omitempty"`

	// Reason recorded when the analyst result failed validation.
	Error string `json:"error,omitempty"`

	// Concise rationale for the vote.
	Rationale string `json:"rationale,omitempty"`
}

// RecommendationStatus records the outcome of the latest decision
// cycle: the aggregated recommendation, its supporting votes, and the
// safety verdict.
type RecommendationStatus struct {
	// Identifier of the decision cycle that produced this record.
	RequestID string `json:"requestId,omitempty"`

	// Aggregated action: SCALE_UP, SCALE_DOWN, or HOLD.
	Action string `json:"action,omitempty"`

	// Aggregated target replica count.
	Target int32 `json:"target,omitempty"`

	// Recommendation confidence: the lesser of vote support and mean
	// winning confidence (Algorithm 5).
	Confidence float64 `json:"confidence,omitempty"`

	// Synthesizer explanation or deterministic voting summary.
	Rationale string `json:"rationale,omitempty"`

	// Deterministic summary of the vote (weights per action).
	AggregationSummary string `json:"aggregationSummary,omitempty"`

	// Individual analyst outcomes backing the recommendation.
	Votes []AnalystVote `json:"votes,omitempty"`

	// Safety verdict: APPROVED, CLAMPED, HELD, or REJECTED.
	Verdict string `json:"verdict,omitempty"`

	// Reasons recorded with the safety verdict.
	VerdictReasons []string `json:"verdictReasons,omitempty"`

	// Reason recorded when aggregation produced no usable
	// recommendation.
	NoRecommendationReason string `json:"noRecommendationReason,omitempty"`
}

// ActionRecord is one executed (or dry-run) scaling action kept in
// the status as the recent scaling history consumed by cooldown,
// rate-limit, and stabilization checks.
type ActionRecord struct {
	// Time the action was submitted.
	Time metav1.Time `json:"time"`

	// SCALE_UP or SCALE_DOWN.
	Action string `json:"action"`

	// Replica count before the action.
	FromReplicas int32 `json:"fromReplicas"`

	// Replica count after the action.
	ToReplicas int32 `json:"toReplicas"`

	// Identifier of the decision cycle that produced the action.
	RequestID string `json:"requestId,omitempty"`

	// True when the action was recorded by a dry-run cycle.
	DryRun bool `json:"dryRun,omitempty"`
}

// Pending operation phases (Algorithm 7/9).
const (
	// Submission outcome unknown (transport failure or controller
	// restart before the outcome was recorded): reconcile first.
	OpPhaseUncertain = "UNCERTAIN"
	// Submitted and under observation until the verification window
	// completes or the observation deadline expires.
	OpPhasePending = "PENDING"
	// A recovery step under the same observation protocol; records
	// the failed operation as its parent.
	OpPhaseRecovery = "RECOVERY"
)

// PendingOperation is the persisted intent and observation state of
// a submitted (or to-be-submitted) scaling operation (Algorithm 7
// reservation + Algorithm 9 observation).
type PendingOperation struct {
	// Unique operation identifier.
	OpID string `json:"opId"`

	// Decision cycle that produced the operation.
	RequestID string `json:"requestId,omitempty"`

	// UNCERTAIN, PENDING, or RECOVERY.
	Phase string `json:"phase"`

	// SCALE_UP or SCALE_DOWN.
	Action string `json:"action"`

	// Replica count observed before the write.
	FromReplicas int32 `json:"fromReplicas"`

	// Replica count submitted (or to be submitted).
	TargetReplicas int32 `json:"targetReplicas"`

	// Time the write was submitted (or, in phase UNCERTAIN, the time
	// the submission was attempted).
	SubmittedAt metav1.Time `json:"submittedAt"`

	// Time after which a non-converged operation is marked terminal
	// and recovery is considered.
	ObserveDeadline metav1.Time `json:"observeDeadline,omitempty"`

	// Start of the current continuous healthy interval; nil until the
	// checked target was first observed healthy.
	VerifiedSince *metav1.Time `json:"verifiedSince,omitempty"`

	// UID of the target Deployment at submission time; a mismatch
	// means the workload was replaced (ownership/identity conflict).
	DeploymentUID string `json:"deploymentUid,omitempty"`

	// Operation this recovery step belongs to; empty for normal ops.
	ParentOpID string `json:"parentOpId,omitempty"`
}

// LLMAutoscalerStatus is the most recently observed state of an
// LLMAutoscaler: current replica count, latest recommendation with
// its safety verdict, recent actions, and conditions.
type LLMAutoscalerStatus struct {
	// Current desired replica count of the target workload as
	// observed by the latest cycle.
	CurrentReplicas *int32 `json:"currentReplicas,omitempty"`

	// Latest action applied or held: SCALE_UP, SCALE_DOWN, or HOLD.
	LastAction string `json:"lastAction,omitempty"`

	// Time of the last submitted scaling write.
	LastScaleTime *metav1.Time `json:"lastScaleTime,omitempty"`

	// Outcome of the latest decision cycle.
	LastRecommendation RecommendationStatus `json:"lastRecommendation,omitempty"`

	// Recent scaling actions (bounded), newest last. Used as the
	// scaling history for safety checks.
	RecentActions []ActionRecord `json:"recentActions,omitempty"`

	// Operation under observation or reconciliation, when one is
	// in flight. While set, the control cycle observes instead of
	// starting a new decision (Algorithm 9).
	PendingOperation *PendingOperation `json:"pendingOperation,omitempty"`

	// Last replica count verified healthy across a full verification
	// window; the recovery target of Algorithm 9.
	LastVerifiedHealthyReplicas *int32 `json:"lastVerifiedHealthyReplicas,omitempty"`

	// Conditions of the autoscaler (Ready, Degraded, ...).
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Observed generation of the spec used by the latest cycle.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=llmas,categories=all
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.scaleTargetRef.name`
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.status.currentReplicas`
// +kubebuilder:printcolumn:name="Last Action",type=string,JSONPath=`.status.lastAction`
// +kubebuilder:printcolumn:name="Confidence",type=number,JSONPath=`.status.lastRecommendation.confidence`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// LLMAutoscaler is the Schema for the llmautoscalers API. One
// resource manages the replica count of one target Deployment through
// the multi-agent reasoning and deterministic safety pipeline.
type LLMAutoscaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LLMAutoscalerSpec   `json:"spec,omitempty"`
	Status LLMAutoscalerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LLMAutoscalerList contains a list of LLMAutoscaler.
type LLMAutoscalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LLMAutoscaler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LLMAutoscaler{}, &LLMAutoscalerList{})
}
