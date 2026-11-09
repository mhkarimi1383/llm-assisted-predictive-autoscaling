"""Manual smoke test for the ReasonScaling pipeline.

Starts nothing: point it at a running Multi Agent Reasoning service
(backed by an LLM Interface with the mock provider) and print the
full response. Not part of the automated test suite.

Usage:
    .venv/bin/python scripts/smoke_reason_scaling.py
"""

import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import grpc
from protos import multi_agent_reasoning_pb2 as pb2
from protos import multi_agent_reasoning_pb2_grpc as pb2_grpc

SNAPSHOT = {
    "namespace": "default",
    "resource_name": "demo",
    "desired_replicas": 4,
    "ready_replicas": 4,
    "cpu_utilization": 0.85,       # above the mock's scale-up threshold
    "memory_utilization": 0.55,
    "forecast": [
        {"timestamp": 1760000000, "predicted_cpu": 0.9, "predicted_memory": 0.6, "predicted_rps": -1},
        {"timestamp": 1760000300, "predicted_cpu": 0.95, "predicted_memory": 0.65, "predicted_rps": -1},
    ],
}

CONFIG = {
    "min_replicas": 2,
    "max_replicas": 20,
    "quorum": 3,
    "confidence_threshold": 0.6,
    "analyst_weights": {},
    "reasoning_timeout_seconds": 20,
    "recent_actions": [],
    "llm": {"endpoint": "mock://", "model": "", "api_key": ""},
}


def main() -> int:
    channel = grpc.insecure_channel("localhost:50055")
    stub = pb2_grpc.MultiAgentReasoningServiceStub(channel)

    request = pb2.ReasonScalingRequest(
        request_id="smoke-1",
        snapshot_json=json.dumps(SNAPSHOT),
        config_json=json.dumps(CONFIG),
    )
    try:
        response = stub.ReasonScaling(request, timeout=30)
    except grpc.RpcError as exc:
        print(f"RPC failed: {exc.code().name}: {exc.details()}")
        return 1

    print("== Analyst results")
    for a in response.analyst_results:
        if a.abstention_reason:
            print(f"  {a.role}: ABSTAIN ({a.abstention_reason})")
        elif a.error:
            print(f"  {a.role}: ERROR ({a.error})")
        else:
            print(f"  {a.role}: {a.action}@{a.target} conf={a.confidence:.2f} — {a.rationale}")

    r = response.recommendation
    print("== Recommendation")
    if r.has_recommendation:
        print(f"  {r.action}@{r.target} confidence={r.confidence:.3f}")
        print(f"  rationale: {r.rationale}")
        print(f"  summary:   {r.aggregation_summary}")
    else:
        print(f"  NONE: {r.no_recommendation_reason}")
        print(f"  summary: {r.aggregation_summary}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
