# LLM-Based Intelligent Autoscaling System

An intelligent autoscaling system that uses Large Language Models (LLMs) as a
reasoning layer over workload forecasts and live Kubernetes cluster state —
configured entirely through a Custom Resource Definition, with every decision
enforced by a deterministic safety engine.

**Status: v1.1 — implemented and verified end-to-end on a local kind cluster.**

## Overview

Traditional autoscalers (HPA/VPA/Cluster Autoscaler) react to CPU/memory
thresholds. This project puts an LLM in the loop as a *reasoning layer*: four
specialist analyst agents examine a forecast, the live cluster state, the
applicable constraints, and the scaling history — in parallel — then a
weighted vote fixes a recommendation. A deterministic safety engine (independent
of the LLM) gets the final say, and only its approval can reach the cluster.

Everything is driven by one Custom Resource:

```yaml
apiVersion: autoscaling.karimi.dev/v1alpha1
kind: LLMAutoscaler          # shortName: llmas
spec:
  scaleTargetRef: {kind: Deployment, name: frontend}
  minReplicas: 2
  maxReplicas: 10
  llm:
    endpoint: "http://ollama:11434/v1"   # any OpenAI-compatible API, or mock://
    apiKeySecret: {name: llm-key, key: key}
  safety:
    cooldownSeconds: 120
    maxScaleDownPercent: 25
    minStepReplicas: 1
    stabilizationWindowSeconds: 60
    verificationWindowSeconds: 60
  reasoning: {quorum: 3, confidenceThreshold: 0.6}
  metrics:  {provider: metrics-server}   # or prometheus
  forecast: {provider: stub}             # or rest (external predictor)
  dryRun: false
```

## Architecture

```
LLMAutoscaler CR ──watch──> Operator (Go, controller-runtime)
   │  Algorithm 1: ownership Lease → cycle → status/history
   ├─> Data Acquisition  (Go :50051)  state, metrics, forecast, capacity, quota
   ├─> Multi-Agent Reasoning (Python :50055)  4 analysts → weighted vote → synthesis
   │      └─ per analyst ─> LLM Interface (Go :50052)  OpenAI-compatible API or mock://
   ├─> Safety Policy     (Go :50053)  deterministic engine — Algorithm 6
   └─> Scaling Executor  (Go :50054)  Scale subresource, dry-run, K8s Events
```

- **Operator** — watches `LLMAutoscaler` resources and runs the control cycle:
  per-workload **scaling ownership Lease**, decision snapshot, multi-agent
  reasoning, deterministic safety evaluation, **action-budget reservation**
  (persisted intent before any write), one checked execution step, and
  **observation/recovery** of in-flight operations (Algorithm 9).
- **Data Acquisition** — collects the decision snapshot: deployment/pod state,
  utilization metrics (metrics-server or Prometheus), workload forecast
  (synthetic `stub` or external `rest` predictor), node allocatable capacity,
  and namespace quotas.
- **LLM Interface** — one shared chat endpoint for all workloads: any
  OpenAI-compatible API (Ollama, vLLM, OpenAI, …) plus a deterministic
  `mock://` provider for tests and demos. Per-workload endpoint/model/key are
  forwarded as request-scoped overrides.
- **Multi-Agent Reasoning** — forecast/status/policy/history analysts run in
  parallel under a shared deadline; every response is validated (Algorithm 3)
  before weighted voting and synthesis (Algorithm 5). The history analyst
  votes only on detected direction reversals.
- **Safety Policy** — pure, stateless, LLM-independent (Algorithm 6): replica
  bounds, cooldown, hourly rate limit, downscale stabilization window,
  percentage limits with strict floor rounding + `minStepReplicas`, automatic
  clamping, downscale health check, namespace pod quota, and node-capacity fit
  using peak (overlapping old/new pods) demand.
- **Scaling Executor** — writes the Deployment `Scale` subresource with a
  version condition, supports dry-run, and emits Kubernetes Events.

Decisions are auditable end-to-end: votes, verdict reasons, and action history
live in the resource status; every executed action emits a Kubernetes Event.

## Documentation

- **Architecture** — components, gRPC contracts, data flow, and concurrency
  mechanisms:
  [`docs/en/ARCHITECTURE.md`](docs/en/ARCHITECTURE.md) ·
  [`docs/fa/ARCHITECTURE.md`](docs/fa/ARCHITECTURE.md)
- **Algorithms** — the full behavioral design (10 algorithms, flowchart +
  pseudocode each) with implementation-status tables:
  [`docs/en/ALGORITHMS.md`](docs/en/ALGORITHMS.md) ·
  [`docs/fa/ALGORITHMS.md`](docs/fa/ALGORITHMS.md)
- **Evaluation** — load-test comparison of fixed vs HPA vs LLMAutoscaler
  (GLM-5.3) with the full metric rubric:
  [`docs/en/EVALUATION.md`](docs/en/EVALUATION.md) ·
  [`docs/fa/EVALUATION.md`](docs/fa/EVALUATION.md)

## Algorithms

The algorithm specification pairs each of the 10 algorithms with a Mermaid
flowchart and pseudocode:

- English: [`docs/en/ALGORITHMS.md`](docs/en/ALGORITHMS.md)
- Persian: [`docs/fa/ALGORITHMS.md`](docs/fa/ALGORITHMS.md)

Both files carry an implementation-status table kept in sync with the code.

**Implemented (v1.1):** control cycle with ownership lock (1), decision
snapshot with capacity/quota/pod-requests (2), validated model responses (3),
parallel analyst fanout with evidence-based abstentions (4), weighted voting
and synthesis (5), deterministic safety with the full constraint set (6),
checked execution with reservations (7), hold-only fallback (8), observation,
uncertain-submission reconciliation, verification window and one-step recovery
(9).

**Deferred:** HPA-style metric fallback (8), per-service capacity models and
scheduling constraints, multi-instance reconciliation scheduling, and the HPA
baseline evaluation harness (10).

## Quick start

Requirements: Go 1.26+, Python 3.13+, `protoc` + Go proto plugins, `kind`,
`kubectl`, Docker. Everything is pinned in the Nix flake — run `nix develop`
to get the exact toolchain.

```bash
nix develop                  # pinned toolchain (Go, uv, protoc, kind, kubectl)

make generate                # protos, deepcopy, CRD, RBAC manifests
make build                   # single binary: llm-autoscaler
make test                    # 38 Go tests + 30 Python tests

# full local deployment: kind cluster + metrics-server + CRD +
# all 6 workloads + demo Deployment + LLMAutoscaler (mock LLM)
make kind-up                 # ./deploy/dev/kind-up.sh

# watch the first decision cycles
kubectl -n default get llmautoscaler demo-frontend-mock -w
```

Example resources live in [`examples/demo/`](examples/demo/): a demo workload,
a **mock-LLM** `LLMAutoscaler` (no API key needed), and a **real-LLM** variant
pointing at any OpenAI-compatible endpoint with the key in a Secret.

### Test VM (optional)

A headless NixOS VM with its own Docker daemon can run the whole suite in
isolation (unit tests + kind end-to-end):

```bash
./deploy/dev/vm-test.sh         # unit tests inside the VM
./deploy/dev/vm-test.sh --kind  # ... plus the kind end-to-end run
```

### Running components individually

```bash
./llm-autoscaler llminterface --llm-provider-endpoint mock://
./llm-autoscaler dataacquisition
./llm-autoscaler safetypolicy
./llm-autoscaler scalingexecutor
./llm-autoscaler operator
python -m multi_agent_reasoning.main
```

## Safety guarantees

1. **Deterministic enforcement** — no LLM output reaches the cluster without
   passing the constraint engine; unknown policies reject.
2. **Fail-safe behavior** — unavailable snapshot/LLM ⇒ hold (fallback);
   failed forecast ⇒ forecast analyst abstains.
3. **Temporal limits** — cooldown between writes, hourly rate limit,
   downscale stabilization below the highest recent target.
4. **Validation pipeline** — model responses validated (JSON contract,
   direction, bounds, confidence); out-of-range targets clamped or rejected;
   unsafe proposals never executed.
5. **Bounded actions** — at most one checked step per cycle, with a persisted
   reservation released on failure and reconciled after uncertainty.

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details on our code of
conduct and the process for submitting pull requests.

## License

This project is licensed under the AGPLv3 License - see the [LICENSE](LICENSE)
file for details.

## Citation

If you use this software in your research, please cite it using the
CITATION.cff file:

```bibtex
@software{karimi2026llmautoscaler,
  author = {Karimi, Muhammed Hussain},
  title = {LLM-Based Intelligent Autoscaling System for Kubernetes},
  version = {1.0.0},
  date = {2026-10-11},
  url = {https://github.com/mhkarimi1383/llm-assisted-predictive-autoscaling},
  license = {AGPL-3.0}
}
```

## Acknowledgments

This work is based on the university project proposal titled
«به‌کارگیری مدل زبانی بزرگ (LLM) به‌عنوان لایه استدلال بر پیش‌بینی‌های بار کاری
و وضعیت کوبرنتیز برای بهبود تصمیم‌های مقیاس‌بندی» (Employing Large Language
Models as a reasoning layer over workload predictions and Kubernetes cluster
status to improve scaling decisions). The original Persian proposal is
available as [PROPOSAL_FA.md](PROPOSAL_FA.md) and an English translation as
[PROPOSAL_EN.md](PROPOSAL_EN.md) in the project directory.
