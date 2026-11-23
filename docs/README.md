# Project Documentation

## Architecture

Start with the architecture document — what is actually built, component by
component, with the data flow and concurrency mechanisms:

- English: [Architecture](en/ARCHITECTURE.md)
- Persian: [معماری سیستم](fa/ARCHITECTURE.md)

## Algorithms and Pseudocode

The behavioral specification — each of the 10 algorithms pairs a Mermaid
flowchart with pseudocode, and carries an implementation-status table synced
with the code:

1. The autoscaling control cycle and data acquisition.
2. Language-model requests and response validation.
3. Parallel analysis agents, voting, and synthesis.
4. Deterministic safety policies.
5. Scaling execution, observation, and recovery.
6. Metric-based fallback and evaluation against an HPA baseline.

- English: [Algorithms and Pseudocode](en/ALGORITHMS.md)
- Persian: [الگوریتم‌ها و شبه‌کد](fa/ALGORITHMS.md)

## Evaluation

Load-test comparison of three modes (no autoscaler / HPA / LLMAutoscaler with
GLM-5.3) measured against the evaluation rubric:

- English: [Evaluation](en/EVALUATION.md)
- Persian: [ارزیابی](fa/EVALUATION.md)

Raw data: `docs/eval/data/` — analysis helper: `docs/eval/analyze.py`.

## Proposals

The project objectives and research scope are documented in the
[English proposal](../PROPOSAL_EN.md) and
[Persian proposal](../PROPOSAL_FA.md).
