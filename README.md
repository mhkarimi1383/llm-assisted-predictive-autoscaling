# LLM-Based Intelligent Autoscaling System

An intelligent autoscaling system that uses Large Language Models (LLMs) as a reasoning layer for Kubernetes workload prediction and cluster status to improve scaling decisions.

## Overview

This system integrates OpenAPI-compatible LLM interfaces, multi-agent reasoning, and deterministic safety policies to make intelligent scaling decisions for Kubernetes workloads. The approach combines predictive workload forecasting with real-time cluster status analysis to generate more accurate and context-aware scaling decisions than traditional rule-based autoscalers.

## Features

- **LLM-Powered Reasoning**: Uses Large Language Models as a reasoning layer for complex decision-making
- **Multi-Agent Architecture**: Specialized agents for prediction analysis, status analysis, policy analysis, historical analysis, and synthesis
- **Deterministic Safety Policies**: Hard constraints that prevent unsafe scaling actions regardless of LLM output
- **OpenAPI Compatible**: Standardized interface for LLM communication
- **Kubernetes Native**: Built as a Kubernetes Operator with Custom Resource Definitions
- **Comprehensive Monitoring**: Prometheus metrics and Grafana dashboards for observability
- **Fail-Safe Design**: Graceful degradation to standard HPA when LLM services are unavailable

## System Architecture

The system consists of five main components:

1. **Data Acquisition Layer** - Collects workload predictions and cluster status
2. **LLM Interface Layer** - OpenAPI-compatible interface for LLM communication
3. **Multi-Agent Reasoning Engine** - Specialized agents for different analysis perspectives
4. **Safety Policy Engine** - Enforces hard safety constraints on LLM outputs
5. **Scaling Executor** - Implements scaling decisions in Kubernetes

## Phased Implementation

This project follows an 8-phase implementation approach:

1. **Requirements Analysis and Planning**
2. **Architecture Design and API Specification**
3. **Data Acquisition Layer Implementation**
4. **LLM Interface and Prompt Engineering**
5. **Multi-Agent Reasoning System Implementation**
6. **Safety Policy Layer Implementation**
7. **Kubernetes Integration**
8. **Evaluation and Documentation**

## Technology Stack

- **Backend Services**: Python 3.9+ (reasoning), Go 1.19+ (K8s operators)
- **Framework**: FastAPI/OpenAPI for LLM interface, Operator SDK for K8s integration
- **Database**: Redis for caching, PostgreSQL for audit logs (optional)
- **Infrastructure**: Kubernetes 1.20+, Prometheus + Grafana, ELK stack
- **External Dependencies**: OpenAPI-compatible LLM services, workload prediction APIs

## Safety Features

Multiple safety layers ensure reliable operation:
1. **Deterministic Policy Engine** - Hard constraints that cannot be overridden
2. **Fail-Default Behavior** - Return to previous state or standard HPA on LLM failure
3. **Rate Limiting** - Prevent thrashing through temporal constraints
4. **Validation Pipeline** - Multiple checks before executing scaling decisions

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details on our code of conduct and the process for submitting pull requests.

## License

This project is licensed under the AGPLv3 License - see the [LICENSE](LICENSE) file for details.

## Citation

If you use this software in your research, please cite it using the CITATION.cff file:

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

This work is based on the university project proposal titled "به‌کارگیری مدل زبانی بزرگ (LLM) به‌عنوان لایه استدلال بر پیش‌بینی‌های بار کاری و وضعیت کوبرنتیز برای بهبود تصمیم‌های مقیاس‌بندی" (Using Large Language Model (LLM) as a reasoning layer on workload predictions and cluster status to improve scaling decisions). The original Farsi proposal is available as PROPOSAL_FA.md and an English translation is available as PROPOSAL_EN.md in the project directory.
