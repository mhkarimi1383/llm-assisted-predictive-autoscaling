# Architecture

This document describes the implemented architecture of the LLM-Based
Intelligent Autoscaling System as of v1.1: the components, their
responsibilities, the gRPC contracts between them, the decision data flow, and
the concurrency mechanisms that make the controller safe. The behavioral
specification (flowcharts and pseudocode) lives in
[ALGORITHMS.md](ALGORITHMS.md); this document focuses on how the system is
actually built.

## System overview

```
                       ┌──────────────────────────────┐
                       │  LLMAutoscaler CR            │
                       │  autoscaling.karimi.dev      │
                       │  /v1alpha1 (shortName llmas) │
                       └──────────────┬───────────────┘
                                      │ watch
                                      ▼
┌─────────────────────────────────────────────────────────────────┐
│ Operator (Go, controller-runtime)                               │
│  Algorithm 1 control cycle:                                     │
│   ownership Lease → pending-op observation (9) → snapshot (2)   │
│   → reasoning (3–5) → safety (6) → reservation + execute (7)    │
│   → status/history recording                                    │
└───┬──────────────┬──────────────┬──────────────┬────────────────┘
    │ gRPC         │ gRPC         │ gRPC         │ gRPC
    ▼              ▼              ▼              ▼
┌──────────┐ ┌─────────────┐ ┌──────────┐ ┌──────────────┐
│ Data     │ │ Multi-Agent │ │ Safety   │ │ Scaling      │
│ Acquisi- │ │ Reasoning   │ │ Policy   │ │ Executor     │
│ tion     │ │ (Python)    │ │ (Go)     │ │ (Go)         │
│ :50051   │ │ :50055      │ │ :50053   │ │ :50054       │
└────┬─────┘ └──────┬──────┘ └──────────┘ └──────┬───────┘
     │              │ Chat                        │ Scale API
     ▼              ▼                             ▼
 Prometheus /   LLM Interface (Go :50052)    Deployment
 metrics-       ├─ OpenAI-compatible API     Scale
 server, nodes, └─ mock:// (deterministic)   subresource
 quotas,        ─────────────────────────    + K8s Events
 forecast API
```

The dependency graph is deliberately not a full mesh: the Operator dials all
five services; Multi-Agent Reasoning dials only the LLM Interface; Data
Acquisition talks to Kubernetes/Prometheus/the forecast API (no gRPC out);
Safety Policy and Scaling Executor are ingress-only.

## Components

### Operator (`operator/`, Go)

The controller-runtime reconciler hosting the control cycle
(`controller.go`). Per reconcile of one `LLMAutoscaler`:

1. **Target read** — the referenced Deployment must exist and be readable;
   otherwise a `TargetMissing` condition is recorded.
2. **Pending-operation observation** (Algorithm 9, `observe.go`) — if a
   scaling operation is in flight, the cycle observes it instead of making a
   new decision (see below).
3. **Scaling ownership** (`ownership.go`) — a `coordination.k8s.io/v1` Lease
   per workload (`llm-autoscaler-own-<deployment>`) is acquired and renewed.
   Overlapping reconciles serialize on it; an expired lease from a dead
   operator is taken over (recovery protocol). Lease reads use the manager's
   non-caching `APIReader`, so no Lease informer or cluster-wide list
   permission is needed.
4. **Decision cycle** — snapshot → reasoning → safety → reservation →
   execution (details below).
5. **Status commit** — votes, verdict, reasons, and action history are written
   to the resource status; no-op updates are skipped because a status write
   re-triggers reconciliation.

### Data Acquisition (`data_acquisition/`, Go, port 50051)

Serves `GetWorkloadSnapshot` (Algorithm 2): reads the target Deployment and
its pods, collects utilization metrics, forecast points, node capacity,
namespace quota, and declared pod requests, validates identity and the desired
replica count against the live read, and freezes the validated inputs as the
shared decision snapshot. Provider seams:

| Seam | Implementations | Selection |
|---|---|---|
| Metrics | `metrics-server` (Metrics API), `prometheus` (instant PromQL queries for CPU rate + working set) | `spec.metrics.provider`, per request |
| Forecast | `stub` (deterministic sine-wave demand curve, no external service), `rest` (external predictor returning `{"points": [...]}`) | `spec.forecast.provider`, per request |

Metric failures are recorded in `metrics_error` and forecast failures in
`forecast_error` — both are advisory; only when *all* observation sources fail
does the snapshot fail and the cycle hold (fallback, Algorithm 8).

### LLM Interface (`llm_interface/`, Go, port 50052)

Exposes the `Chat` RPC consumed by the reasoning engine (Algorithm 3).
Requests are routed per call (`providerFor`): no endpoint override uses the
service default provider (from `--llm-provider-endpoint`), `mock://` forces
the deterministic mock provider, any other URL goes to the OpenAI-compatible
provider. The OpenAI provider implements `/chat/completions` with
request-scoped endpoint/model/API-key overrides (the CR's `llm` section is
forwarded by the operator), bounded retries for transient failures, strict
JSON mode (`response_format`), and response-size caps. The mock provider
returns deterministic analyst decisions derived from the snapshot embedded in
the prompt — it powers tests, demos, and dry runs without any external
service. Because routing is per request, one LLM Interface instance serves
workloads with different LLM backends simultaneously.

### Multi-Agent Reasoning (`multi_agent_reasoning/`, Python, port 50055)

Implements Algorithms 3–5 (`agents.py`, `validation.py`, `aggregation.py`,
`prompts.py`). `ReasonScaling` receives the snapshot and configuration as
JSON and:

1. runs the four analysts (forecast, status, policy, history) **in parallel**
   via a thread pool, each with the remaining cycle deadline as its gRPC
   timeout;
2. validates every response against the JSON contract (known action, integer
   target consistent with the action and current count, confidence in [0,1],
   target within configured bounds, truncation rejection, in-band transport
   errors, abstention records);
3. abstains role-appropriately (missing forecast → forecast analyst abstains;
   missing metrics → status analyst abstains; no direction reversal in the
   scaling history → history analyst abstains);
4. aggregates with weighted voting: the winner needs strictly more than half
   of the valid voting weight; the target is the weighted median of the
   winning analysts' targets; confidence is `min(support, mean winning
   confidence)`; a quorum of validated votes is required (forecast and status
   roles mandatory);
5. asks the synthesizer to explain the **fixed** action/target; if synthesis
   fails or tampers with the decision, the deterministic vote summary is
   attached instead.

The component dials only the LLM Interface service (via the shared
`helpers.clients.ServiceClients` registry).

### Safety Policy (`safety_policy/`, Go, port 50053)

`EvaluateScaling` runs the deterministic engine (`engine.go`) — a pure
function with no LLM, cluster, or storage access. Check order follows
Algorithm 6:

1. prerequisite validation (known action, non-negative target,
   `minReplicas ≥ 1`, `maxReplicas ≥ minReplicas`);
2. HOLD short-circuits to `HELD` (no mutation);
3. no-op proposals (`target == current`) are `HELD`;
4. temporal checks over the recorded history: cooldown since the last executed
   action (dry-run and HOLD records do not consume budget), hourly rate
   limit, downscale stabilization (a reduction is held while the highest
   target recorded inside the stabilization window exceeds the proposal);
5. health check: a reduction on an unhealthy workload (ready below desired or
   pods pending/terminating) is held;
6. quota: the proposed target must fit the namespace pods quota;
7. numeric feasibility: bounds plus percentage allowances rounded **down**
   (a zero allowance blocks movement; `minStepReplicas` is the explicit
   policy that funds a minimum step);
8. clamping: an out-of-range target moves to the nearest feasible integer
   (`CLAMPED`) unless clamping would reverse the proposed direction
   (`REJECTED`);
9. capacity: for scale-up, the peak pod count — `max(current, target)`,
   because old and new pods overlap during a rollout — times the declared
   per-pod requests must fit the eligible node allocatable capacity; unknown
   dimensions are skipped with an audit note.

The verdict (`APPROVED` / `CLAMPED` / `HELD` / `REJECTED`) is bound to the
checked target and carries human-readable reasons plus one audit line per
check.

### Scaling Executor (`scaling_executor/`, Go, port 50054)

Implements one checked write (Algorithm 7): reads the target Deployment for
the observed replica count and resource version, then updates the `Scale`
subresource. A version conflict means the state moved underneath the decision
→ the result reports it and the operator starts a fresh cycle rather than
writing blindly. `dry_run` records the checked step without submitting.
Executed actions emit Kubernetes Events on the target for auditability.

## The control cycle in detail (data flow)

```
Reconcile(LLMAutoscaler)
  │
  ├─ pending op? ──yes──> observe (Algorithm 9) ──> resolved/verified?
  │                                            yes → clear pending, continue
  │                                            no  → requeue (15 s)
  ├─ acquire ownership Lease ──denied──> hold, requeue 30 s
  │
  ├─ GetWorkloadSnapshot ──fail──> HOLD fallback, record reason
  ├─ ReasonScaling (4 analysts, vote, synthesize)
  │      ├─ no recommendation ──> HOLD, record reason (quorum/disagreement/confidence)
  │      └─ HOLD ──> record, done
  ├─ EvaluateScaling (deterministic engine)
  │      ├─ HELD / REJECTED ──> record verdict + reasons, done
  │      └─ APPROVED / CLAMPED ──> proceed
  ├─ persist reservation (status.pendingOperation)      ← before any write
  ├─ ExecuteScaling (Scale API, version condition)
  │      ├─ transport error ──> phase = UNCERTAIN, requeue 15 s (reconcile first)
  │      ├─ definitive failure ──> release reservation, record failure
  │      └─ success ──> action record (consumes budget) + keep pending op
  └─ commit status, requeue (60 s; observation cycles: 15 s)
```

### Concurrency and consistency mechanisms

Three mechanisms make overlapping cycles and crashes safe:

| Mechanism | What it prevents | How |
|---|---|---|
| **Ownership Lease** (Algorithm 1) | Two reconciles writing concurrently (a real v1 defect: a duplicated action record) | Per-workload Lease, renewed per cycle; expired leases are taken over |
| **Action budget reservation** (Algorithm 7) | Spending cooldown/rate budget for a write that never landed, or double-executing after a crash | The operation intent is persisted in `status.pendingOperation` **before** the write; released on definitive failure; on transport uncertainty it enters `UNCERTAIN` |
| **Version-conditioned write** (Algorithm 7) | Writing over a state that changed since the decision | The executor reads the resource version and the Scale update conflicts if it moved |

### Observation and recovery (Algorithm 9, `observe.go`)

A pending operation carries its phase, target, observation deadline,
verification-window state, the Deployment UID at submission time, and (for
recovery steps) the parent operation. Each observation cycle:

- reconciles `UNCERTAIN` submissions against workload state first — desired
  replicas equal to the target ⇒ the write landed (continue observing);
  equal to the starting count ⇒ never landed (release the reservation);
  anything else ⇒ another actor moved the workload (degraded, stop);
- rejects on identity conflict (Deployment UID changed ⇒ workload replaced);
- tracks the **continuous healthy interval**: the checked target must be
  reached with ready replicas covering it and no pods pending/terminating;
  a health regression resets the interval;
- marks the operation **applied** (or `recovered`) once the interval spans the
  verification window, recording `lastVerifiedHealthyReplicas`;
- at the observation deadline without convergence: marks the operation
  terminal (`Degraded` condition) and considers **one** recovery step toward
  the last verified healthy count — itself re-evaluated through the
  deterministic safety engine, carrying the failed operation as its parent so
  recovery never chains.

## Configuration surface

Everything a workload needs is in the CR (see the README example). Notable
fields added in v1.1: `safety.minStepReplicas`,
`safety.verificationWindowSeconds`, `safety.observationTimeoutSeconds`.
Deployment-level defaults (gRPC endpoints of the five services, metrics
address, watch namespace, requeue period) are operator CLI flags. The LLM
Interface has provider defaults (`--llm-provider-endpoint`, model, API key,
timeout); all of them can be overridden per request from the CR.

## Repository layout

```
main.go                     Cobra CLI: one subcommand per component + operator
config/                     shared connection config
helpers/                    shared client registry + gRPC server lifecycle
  ├─ clients.go / clients.py        typed clients for all five services
  └─ grpcserver.go / grpcserver.py  serve-until-cancelled boilerplate
data_acquisition/           snapshot RPC, metrics/forecast providers, K8s reads
llm_interface/              Chat RPC, OpenAI-compatible + mock providers
multi_agent_reasoning/      analysts, validation, aggregation, prompts
safety_policy/              deterministic engine + RPC
scaling_executor/           Scale subresource writes, dry-run, events
operator/
  ├─ api/v1alpha1/          CRD types (generated deepcopy + CRD + RBAC)
  ├─ controller.go          the control cycle
  ├─ ownership.go           per-workload scaling Lease
  └─ observe.go             Algorithm 9 state machine (+ controller glue)
protos/                     proto sources + generated Go/Python code
deploy/                     CRD, RBAC, manifests, kind + VM scripts
examples/demo/              demo workload, mock-LLM and real-LLM CRs
tests/                      Python unit tests (validation + aggregation)
scripts/                    proto generation, smoke test
docs/en/, docs/fa/          algorithm spec + architecture (this file), both languages
```

## Testing and verification

- **Go unit tests** (`go test ./...`): the deterministic safety engine
  (cooldown, rate limit, stabilization, percentage limits, min-step, clamping,
  health, quota, capacity), LLM providers (OpenAI client against an
  `httptest` server, mock policies, per-request routing), the ownership Lease
  (acquire/renew/conflict/takeover/release), and the Algorithm 9 state machine
  (uncertain resolution, identity conflict, verification window, health
  resets, deadline and recovery rules).
- **Python unit tests** (`pytest tests/`): response validation (Algorithm 3)
  and weighted aggregation (Algorithm 5).
- **End-to-end**: `deploy/dev/kind-up.sh` deploys the whole control plane plus
  a demo workload to kind; the observed behavior (lease held, pending
  operation lifecycle, APPLIED after the verification window, HELD verdicts
  with reasons, stepwise ramp-down 8→2) matches the algorithm spec.
- **Isolated CI-style runs**: `deploy/dev/vm-test.sh` boots a headless NixOS
  VM with its own Docker daemon and runs the full suite plus the kind
  end-to-end there.
