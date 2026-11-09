"""Specialist analyst execution (Algorithms 3 and 4).

Four analysts — forecast, status, policy, history — receive the same
shared snapshot and run in parallel with a shared deadline. Each one
calls the LLM Interface Chat RPC, and its response is validated by
validation.py before it may participate in aggregation.

The history analyst abstains when no history was supplied (v1 keeps
history in the LLMAutoscaler status; it may be absent).
"""

from __future__ import annotations

import json
import logging
import time
from concurrent.futures import ThreadPoolExecutor
from typing import Optional

import grpc

from protos import llm_interface_pb2 as llm_pb2
from protos import llm_interface_pb2_grpc as llm_pb2_grpc

from .prompts import ROLE_OBJECTIVES, SYSTEM_PROMPT, USER_PROMPT
from .validation import ValidationContext, ValidatedResult, validate_analyst_result

logger = logging.getLogger(__name__)

ANALYST_ROLES = ("forecast", "status", "policy", "history")


class AnalystRunner:
    """Runs the analyst fanout against the LLM Interface service."""

    def __init__(self, llm_stub: llm_pb2_grpc.LLMInterfaceServiceStub):
        self.llm_stub = llm_stub

    def run_all(
        self,
        snapshot: dict,
        config: dict,
        request_id: str,
        deadline: float,
    ) -> list:
        """Run all analysts in parallel; return list[ValidatedResult].

        deadline: absolute monotonic deadline (time.monotonic()) for
        the whole fanout. Each analyst's gRPC call is bounded by the
        remaining time (Algorithm 3: attempt budget is the cycle
        budget). Unfinished calls are cancelled at deadline expiry.
        """
        current = int(snapshot.get("desired_replicas") or 0)
        min_replicas = int(config.get("min_replicas") or 1)
        max_replicas = int(config.get("max_replicas") or 1)

        results = [None] * len(ANALYST_ROLES)

        def run_one(index_role):
            index, role = index_role
            results[index] = self._run_analyst(
                role, snapshot, config, request_id, current, min_replicas, max_replicas, deadline
            )

        with ThreadPoolExecutor(max_workers=len(ANALYST_ROLES)) as pool:
            list(pool.map(run_one, enumerate(ANALYST_ROLES)))

        # Defensive: run_one always assigns, but a missing result is
        # recorded as a failure rather than dropped (auditability).
        return [
            r if r is not None else ValidatedResult(role="unknown", error="analyst did not return")
            for r in results
        ]

    def _run_analyst(
        self,
        role: str,
        snapshot: dict,
        config: dict,
        request_id: str,
        current: int,
        min_replicas: int,
        max_replicas: int,
        deadline: float,
    ) -> ValidatedResult:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            return ValidatedResult(
                role=role,
                abstained=True,
                abstention_reason="analysis deadline expired before the request started",
            )

        # Role-specific evidence requirements (Algorithm 4): an analyst
        # abstains when the evidence its role needs is unavailable.
        abstention = self._evidence_check(role, snapshot)
        if abstention:
            return ValidatedResult(role=role, abstained=True, abstention_reason=abstention)

        prompt_user = USER_PROMPT.format(
            role=role,
            objective=ROLE_OBJECTIVES[role],
            current_replicas=current,
            min_replicas=min_replicas,
            max_replicas=max_replicas,
            snapshot_json=json.dumps(snapshot, sort_keys=True),
            history_json=json.dumps(config.get("recent_actions") or []),
        )
        prompt_system = SYSTEM_PROMPT.format(
            role=role,
            min_replicas=min_replicas,
            max_replicas=max_replicas,
        )

        llm_cfg = config.get("llm") or {}
        chat_request = llm_pb2.ChatRequest(
            messages=[
                llm_pb2.ChatMessage(role="system", content=prompt_system),
                llm_pb2.ChatMessage(role="user", content=prompt_user),
            ],
            model=llm_cfg.get("model", ""),
            temperature=0.2,
            max_tokens=0,
            json_mode=True,
            request_id=f"{request_id}:{role}",
            endpoint=llm_cfg.get("endpoint", ""),
            api_key=llm_cfg.get("api_key", ""),
        )

        try:
            response = self.llm_stub.Chat(
                chat_request, timeout=remaining, wait_for_ready=False
            )
        except grpc.RpcError as exc:
            if exc.code() == grpc.StatusCode.DEADLINE_EXCEEDED:
                return ValidatedResult(
                    role=role,
                    abstained=True,
                    abstention_reason="analysis deadline expired",
                )
            return ValidatedResult(
                role=role,
                error=f"LLM Interface RPC failed: {exc.code().name}",
            )

        ctx = ValidationContext(
            role=role,
            request_id=request_id,
            current_replicas=current,
            min_replicas=min_replicas,
            max_replicas=max_replicas,
        )
        result = validate_analyst_result(
            role=role,
            content=response.content,
            finish_reason=response.finish_reason,
            in_band_error=response.error,
            ctx=ctx,
        )
        logger.info(
            "Analyst %s -> %s (action=%s target=%s conf=%.2f)",
            role,
            "abstain" if result.abstained else ("error" if result.error else "vote"),
            result.action or "-",
            result.target or "-",
            result.confidence,
        )
        return result

    @staticmethod
    def _evidence_check(role: str, snapshot: dict) -> Optional[str]:
        """Return an abstention reason when the role's evidence is missing."""
        if role == "forecast":
            if snapshot.get("forecast_error"):
                return f"forecast unavailable: {snapshot['forecast_error']}"
            if not snapshot.get("forecast"):
                return "no forecast points in the snapshot"
        if role == "status":
            if snapshot.get("metrics_error"):
                return f"metrics unavailable: {snapshot['metrics_error']}"
            cpu = snapshot.get("cpu_utilization")
            mem = snapshot.get("memory_utilization")
            if (cpu is None or float(cpu) < 0) and (mem is None or float(mem) < 0):
                return "no utilization metrics in the snapshot"
        if role == "history":
            # The operator supplies history in the snapshot's
            # recent_actions; v1 has no separate history store, so
            # this abstains when the record is empty.
            if not snapshot.get("recent_actions"):
                return "no scaling history available (v1 has no history store)"
        return None
