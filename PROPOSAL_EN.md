# Employing Large Language Models (LLMs) as a Reasoning Layer over Workload Predictions and the Kubernetes Cluster Status to Improve Scaling Decisions

## University Project Proposal

**University:** Payame Noor University, Qom Province  
**Faculty/Department:** Faculty of Engineering  
**Field of Study:** Computer Science  
**Author:** Muhammed Hussain Karimi  
**Supervisor:** Prof. Tari  
**Semester:** First semester, academic year 1405–1406 (Fall 2026)

---

## Abstract

This project aims to improve automatic scaling (autoscaling) decisions in Kubernetes by integrating a large language model (LLM) as a reasoning layer over workload forecasts and the real-time cluster state. The system uses an OpenAPI-compatible connection to interact with the language model, employs the Agent Fanout pattern to improve the speed and quality of decision-making, and simultaneously enforces deterministic safety policies. The proposed approach seeks to overcome the limitations of traditional metric-based autoscaling and — by leveraging contextual understanding, predictive insight, and policy-aware reasoning — produces more accurate, more interpretable, and safer decisions. The project follows an applied, development-oriented methodology comprising process familiarization, requirements analysis, architecture and data design, incremental implementation, and evaluation against controlled scenarios. The deliverable is an intelligent autoscaling system for Kubernetes clusters, together with its architecture documentation, evaluation scenarios, and user guide.

**Keywords:** Kubernetes, autoscaling, large language model, workload forecasting, multi-agent system, safety policy, OpenAPI

---

## 1. Project Title

Employing large language models (LLMs) as a reasoning layer over workload predictions and the Kubernetes cluster status to improve scaling decisions.

---

## 2. Problem Statement

Kubernetes has become the de facto standard for container orchestration; however, its built-in autoscaling mechanisms — the Horizontal Pod Autoscaler, the Vertical Pod Autoscaler, and the Cluster Autoscaler — rely mainly on simple threshold rules or basic forecasting models. These approaches often fail to capture complex workload patterns, seasonal variations, and the business logic specific to each application; as a result, answering questions such as "What is the correct scaling action for a forecast load spike during peak hours?", "What is the impact of a scaling decision on cost and cluster stability?", and "Why did the system make this decision?" is difficult and time-consuming.

This project introduces an architecture in which a large language model serves as the reasoning layer: it receives workload forecasts and the Kubernetes state through a standard OpenAPI interface, draws on multi-agent reasoning for greater robustness, and applies deterministic safety constraints to prevent unsafe scaling decisions. The core problem of the project is therefore to design an integrated process that ingests load forecasts and cluster status, reasons over them, and securely executes the outcome — in such a way that every scaling decision is traceable, interpretable, and always compliant with safety policies.

---

## 3. Importance and Rationale

Current Kubernetes autoscaling solutions face a number of limitations: reactive rather than proactive response to load changes, difficulty in accounting for business context and domain knowledge, limited capability in navigating complex multi-dimensional decision spaces, susceptibility to oscillation or thrashing under variable load, and the lack of interpretability in decisions. These limitations harm both resource efficiency and infrastructure cost, as well as application performance and service stability.

Introducing an LLM reasoning layer makes it possible to leverage the model's ability to combine information from diverse sources, produce fine-grained context-aware decisions, provide interpretable justifications for scaling actions, and preserve safety through the deterministic enforcement of policies — thereby improving resource efficiency and application performance. As a practical undertaking, the project brings together concepts from software engineering, distributed systems, applied artificial intelligence, and DevOps in a real-world infrastructure problem, and it has direct applicability to organizations for whom cloud resource costs and service stability are critical.

---

## 4. Project Objectives

### 4.1 Main Objective

Design and implement a system that, by employing a large language model as a reasoning layer over workload forecasts and the real-time state of the Kubernetes cluster, produces improved, interpretable, and policy-compliant scaling decisions and executes them securely.

### 4.2 Sub-objectives

- Collect and aggregate workload forecasts and cluster status in the data-acquisition layer
- Implement an OpenAPI-compatible interface for flexible, vendor-independent connection to the language-model service
- Design and implement the Agent Fanout pattern for versatile and robust reasoning
- Define a policy specification language and implement a validation engine for deterministic safety policies
- Integrate the system with Kubernetes through a custom controller or an HPA extension
- Expose system metrics for monitoring with Prometheus and Grafana
- Evaluate performance, decision quality, and safety using load tests and controlled scenarios
- Prepare design documentation, evaluation scenarios, and a user guide

---

## 5. Research Questions

- How can workload-forecast and cluster-status data be structured so that the language model produces scaling decisions that are interpretable and traceable?
- How does the agent fan-out pattern improve decision quality and robustness relative to single-agent reasoning, and which aggregation mechanism is most appropriate?
- How can deterministic safety policies be applied to the language model's output such that cluster stability is never violated under any circumstances?
- How does the proposed system's performance compare with traditional metric-based autoscaling, and in which scenarios is a significant improvement observed?

Owing to the applied, implementation-oriented nature of the project, no standalone statistical hypothesis is formulated. The questions will be answered through system design and evaluation against operational scenarios.

---

## 6. Theoretical Foundations and Related Work

In the field of autoscaling, the built-in Kubernetes mechanisms — the Horizontal Pod Autoscaler (HPA), the Vertical Pod Autoscaler (VPA), and the Cluster Autoscaler — rely mainly on resource metrics and threshold rules [1], and predictive approaches based on time-series forecasting and machine-learning systems, including reinforcement learning, have been proposed to overcome their reactive nature [5].

In the field of large language models, recent studies have examined their use in DevOps and infrastructure-as-code (IaC) tasks, anomaly detection, and root-cause analysis [3]; however, their use for real-time scaling decisions remains limited.

In the field of multi-agent systems, agent fan-out patterns for parallel processing and higher reliability, aggregation methods for combining decisions, and consensus mechanisms for safety-critical systems are well established [4], and standardized interaction with language-model services is based on the OpenAPI specification [2].

The project focuses on designing and evaluating an intelligent infrastructure tool for an experimental cluster; it does not claim to introduce a new learning method or a new optimization algorithm.

---

## 7. Methodology

The research is applied in terms of purpose and development-oriented with a case-study approach in terms of execution. The operational scope is scaling decisions for a Kubernetes cluster based on workload forecasts and real-time status. The required information is gathered from official Kubernetes documentation, related research literature, open datasets published by cloud providers such as Alibaba Cloud, Huawei Cloud, and Microsoft Azure, and data produced by experiments on the laboratory cluster.

| Phase | Activity | Deliverable |
|:---:|---|---|
| 1 | Problem analysis and requirements capture | List of functional requirements and constraints |
| 2 | Architecture design and API specification | Detailed architecture and OpenAPI specification |
| 3 | Data-acquisition layer implementation | Workload-forecast and cluster-status collectors |
| 4 | Language-model interface and prompt engineering | OpenAPI coverage and initial prompt tests |
| 5 | Multi-agent reasoning system implementation | Agent implementations and aggregation logic |
| 6 | Safety policy layer implementation | Policy language and validation engine |
| 7 | Kubernetes integration | Custom controller and HPA integration |
| 8 | Evaluation and documentation | Evaluation results, final report, and user guide |

---

## 8. Scope and System Capabilities

The system's capabilities within the project scope are as follows:

| Area | In-scope capabilities |
|---|---|
| Data acquisition | Collection of workload forecasts and cluster status via the Kubernetes API and forecasting service |
| Language-model interface | OpenAPI-compatible connection, validation and interpretation of model responses |
| Multi-agent reasoning | Forecast-, status-, policy-, and history-analysis agents plus a synthesizer agent |
| Safety policy | Action-rate limits, resource bounds, stability windows, namespace quotas, and application-specific constraints |
| Execution | Application of approved decisions via a custom controller or an HPA extension |
| Monitoring | Metric exposure for Prometheus/Grafana and decision-event logging |

Full replacement of an organization's management infrastructure, support for every Kubernetes distribution, and training or fine-tuning of the language model from scratch are outside the project scope.

---

## 9. Proposed Reasoning and Decision-Making Model

For each decision request, the system receives a standard structure comprising the workload forecast (indicators such as CPU, memory, and request rate), the current cluster state (number of nodes, pod status, and resource utilization), supplementary context (time of day, day of week, and business events), and the identifier of the active safety policies. The system's response comprises the proposed action (scale up, scale down, or no change), the target replica count, a confidence score between zero and one, a rationale, and the outcome of safety checks. This structure is defined in an OpenAPI specification with a `/scaling-decision` endpoint, so that the system remains independent of the language-model provider.

In the agent fan-out pattern, five specialized agents — the forecast analyst, the status analyst, the policy analyst, the history analyst, and the synthesizer — receive the same data but apply distinct analytical perspectives. Their outputs are combined by weighted voting or a consensus mechanism to produce a final recommendation together with a confidence score. This structure mitigates the error of any single agent or model through diversity of viewpoints.

Notwithstanding the reasoning capability of the language model, hard safety constraints are enforced deterministically and independently of the model: rate limits (maximum scaling actions per time window), absolute bounds on the replica count, stability windows (minimum spacing between scaling actions), namespace-level resource quotas, and application-specific constraints such as the maximum number of pods per node. The safety enforcer validates every recommendation before execution; a recommendation that violates policy is either amended for compliance or rejected with a safe fallback (typically holding the current state). These decisions are based on data recorded in the system, and output accuracy depends on the quality of the input forecast and the freshness of the cluster state.

---

## 10. Tools and Technical Architecture

The system architecture comprises five main components: the data-acquisition layer, which collects workload forecasts and the Kubernetes state; the language-model reasoning interface, implemented as an OpenAPI-compatible connection; the multi-agent reasoning engine with specialized reasoning agents; the safety-policy enforcer, which applies deterministic constraints to the model's output; and the scaling executor, which applies approved decisions in Kubernetes. Detailed architecture and data-flow diagrams will be provided in the detailed report.

The system will be implemented in Python (reasoning and data-processing components) and Go (controllers and Kubernetes operators). Kubernetes 1.20 or higher, the language-model service (API or open-source model), the Prometheus and Grafana monitoring stack, and CI/CD pipeline tooling will be employed. User login and technical event logging have been included for error tracing.

The required hardware includes a development and test cluster of at least three nodes, storage for historical data and metrics, and — if open-source models are used — a GPU accelerator for local inference. The human resources comprise one to two graduate students as the primary developers, a supervisor specialized in distributed systems or machine learning, and, where possible, a domain expert from the technical operations or DevOps team.

---

## 11. Evaluation Method

Evaluation will be carried out using a set of applied scenarios and controlled data. For each scenario, the expected value or outcome is computed before execution and compared with the system's output. The scenarios cover at least the following:

- Load tests with daily and weekly patterns, comparing the system's behaviour with the default HPA
- Reduction of resource waste caused by over-provisioning (target: 20–30% reduction)
- Improvement of application response time under load spikes (target: 15–25% improvement)
- Reduction of scaling oscillation and thrashing
- Deliberate disconnection of the language-model service and examination of graceful degradation and safe fallback
- Injection of policy-violating recommendations and examination of their amendment or rejection by the safety layer
- Assessment of explanation quality and decision auditability

The evaluation criteria include completeness of the core scenarios, agreement of results with expected values, zero violations of hard safety constraints, safe behaviour when the language model is unavailable, and a clear rollback mechanism for incorrect decisions. Evaluation results will be recorded as a table of scenario, expected value, observed result, and status.

---

## 12. Proposed Timeline

| Timeframe | Activity | Deliverable |
|:---:|---|---|
| Weeks 1–4 | Requirements and design | Detailed architecture and API specification |
| Weeks 4–7 | Data-acquisition layer implementation | Forecast and status collectors |
| Weeks 7–10 | Language-model interface and prompt engineering | OpenAPI coverage and initial prompt tests |
| Weeks 10–14 | Multi-agent reasoning system | Agent implementations and aggregation logic |
| Weeks 14–16 | Safety policy enforcer | Policy language and validation engine |
| Weeks 16–19 | Kubernetes integration | Custom controller and HPA integration |
| Weeks 19–23 | Testing and evaluation | Load tests, safety validation, and benchmarks |
| Weeks 23–25 | Documentation and presentation | Final report, slides, and demo preparation |

**Total duration: 25 weeks.** This schedule is preliminary and will be finalized against the semester calendar in consultation with the supervisor.

---

## 13. Expected Outcomes

- An intelligent, LLM-based autoscaling system with an architecture deployable on Kubernetes
- 20–30% reduction in resource waste caused by over-provisioning
- 15–25% improvement in application response time under load spikes
- Reduced scaling oscillation and closer alignment of decisions with the true workload pattern
- Interpretable, auditable decisions accompanied by rationales
- Guaranteed enforcement of deterministic safety constraints and graceful degradation when the model service is unavailable
- Design documentation, evaluation scenarios, and a user guide for the system

---

## 14. Limitations and Considerations

The quality of the system's decisions depends on the quality of the incoming workload forecasts and the freshness of the cluster state; the system cannot definitively reconstruct unrecorded events. Unavailability of the language-model service can halt recommendation generation; in that case the system must revert to a safe default behaviour (holding the current state or falling back to the HPA). Model response latency may affect highly time-critical decisions, and the cost of access to commercial models must also be taken into account. For the academic evaluation, open datasets published by cloud providers — including Huawei Cloud, Alibaba Cloud, and Microsoft Azure — as well as data from experiments on the laboratory cluster will be used. No confidential business information will be included in the research reports or presentations.

---

## 15. Initial References

- [1] Kubernetes Horizontal Pod Autoscaler Documentation.
- [2] OpenAPI Specification v3.0.0.
- [3] Recent Papers on LLMs for DevOps and AIOps.
- [4] Works on Multi-Agent Systems for Decision Making.
- [5] Research on Predictive Autoscaling in Cloud Environments.

---

## 16. Intellectual Property Rights

All economic and moral rights in this research, titled "Employing Large Language Models (LLMs) as a Reasoning Layer over Workload Predictions and the Kubernetes Cluster Status to Improve Scaling Decisions" — including the idea, the proposed method, the system architecture, the algorithms, the software implementation, the source code, the mechanism by which the large language model is used, the prompts and methods of interacting with the model, the data used and produced in the experiments, the evaluation results, and the documentation arising from the research — shall remain protected in accordance with the university's regulations and by-laws and the applicable intellectual-property laws.

Any use, reproduction, publication, or exploitation of the exclusive outcomes of this research, outside the framework of the university's regulations and without observing the authors' rights, shall be subject to the relevant intellectual-property provisions.

Wherever language models, software, libraries, open-source frameworks, data, or other components belonging to third parties or third-party projects are used, the rights of the copyright holders and the applicable license terms shall be respected, and the use of such components shall not be construed as a transfer of their ownership to the researcher.

The publication of papers, source code, experimental data, and other outcomes of this research shall likewise take place in accordance with the university's regulations and the agreements governing the authors' economic and moral rights.

---

*Note: This is the English version of the university project proposal "به‌کارگیری مدل زبانی بزرگ (LLM) به‌عنوان لایه استدلال بر پیش‌بینی‌های بار کاری و وضعیت کوبرنتیز برای بهبود تصمیم‌های مقیاس‌بندی". The original Persian proposal is available as PROPOSAL_FA.md in the project directory.*
