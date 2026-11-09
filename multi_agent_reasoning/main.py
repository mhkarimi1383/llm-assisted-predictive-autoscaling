#!/usr/bin/env python3
"""
Multi Agent Reasoning Component - CLI Entry Point

Implements the multi-agent scaling reasoning pipeline (Algorithms 3-5):
four specialist analysts run in parallel over a shared decision
snapshot, their validated results are aggregated by weighted voting,
and a synthesizer produces the explanation for the fixed
recommendation. Every analyst call goes through the LLM Interface
Chat RPC; this component dials only the LLM Interface service.
"""

import argparse
import json
import logging
import sys
import time

import grpc

from protos import multi_agent_reasoning_pb2 as pb2
from protos import multi_agent_reasoning_pb2_grpc as pb2_grpc
from protos import llm_interface_pb2 as llm_pb2

sys.path.append(".")
from helpers.clients import ServiceClients
from helpers.grpcserver import run_grpc_server

from .agents import AnalystRunner
from .aggregation import aggregate
from .prompts import SYSTEM_PROMPT, ROLE_OBJECTIVES
from .validation import SynthesisRejected, validate_synthesis

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
)
logger = logging.getLogger(__name__)

# Default reasoning deadline when the request config omits one.
DEFAULT_REASONING_TIMEOUT_SECONDS = 30.0

# The synthesizer prompt explains a fixed action/target; it may not
# change them (validated in validate_synthesis).
SYNTHESIZER_PROMPT = """{{"role": "synthesizer"}}

You are the synthesis agent of a Kubernetes autoscaling decision system.
A deterministic vote among specialist analysts already fixed the decision below.
Your task is to explain it, citing snapshot evidence and the disagreement if any.

Fixed decision: {action} to {target} replicas (current: {current}).
Vote summary: {summary}

Respond with EXACTLY one JSON object and nothing else:
  {{"action": "{action}", "target": {target}, "rationale": "<two or three sentences explaining the fixed decision>"}}

Do not change the action or the target."""


class ReasoningService(pb2_grpc.MultiAgentReasoningServiceServicer):
    """Implementation of the MultiAgentReasoningService."""

    def __init__(self):
        # Shared client registry. This component dials only the LLM
        # Interface service (corrected dependency graph); gRPC stubs
        # are thread-safe, so the analyst fanout shares one stub.
        self.clients = ServiceClients()
        self.llm_interface_stub = None

    def initialize_outgoing_connections(self, llm_interface_host: str, llm_interface_port: int):
        """Point the registry at the configured LLM Interface and
        create the stub eagerly so failures surface at startup."""
        try:
            self.clients = ServiceClients(
                hosts={"llm_interface": (llm_interface_host, llm_interface_port)}
            )
            self.llm_interface_stub = self.clients.llm_interface()
        except Exception as e:
            logger.error(f"Failed to initialize LLM Interface connection: {e}")
            raise

    # ------------------------------------------------------------------
    # ReasonScaling: Algorithms 3-5 pipeline
    # ------------------------------------------------------------------
    def ReasonScaling(self, request, context):
        request_id = request.request_id or "unknown"
        logger.info(f"Received ReasonScaling request (ID: {request_id})")

        if not request.snapshot_json or not request.config_json:
            context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
            context.set_details("snapshot_json and config_json are required")
            return pb2.ReasonScalingResponse()

        try:
            snapshot = json.loads(request.snapshot_json)
            config = json.loads(request.config_json)
        except json.JSONDecodeError as exc:
            context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
            context.set_details(f"invalid JSON payload: {exc}")
            return pb2.ReasonScalingResponse()

        if self.llm_interface_stub is None:
            context.set_code(grpc.StatusCode.FAILED_PRECONDITION)
            context.set_details("LLM Interface connection is not initialized")
            return pb2.ReasonScalingResponse()

        timeout = float(
            config.get("reasoning_timeout_seconds") or DEFAULT_REASONING_TIMEOUT_SECONDS
        )
        deadline = time.monotonic() + timeout

        # --- Algorithm 4: parallel specialist analysis ---
        runner = AnalystRunner(self.llm_interface_stub)
        analyst_results = runner.run_all(snapshot, config, request_id, deadline)

        # --- Algorithm 5: weighted voting ---
        current = int(snapshot.get("desired_replicas") or 0)
        outcome = aggregate(
            results=analyst_results,
            weights=config.get("analyst_weights") or {},
            quorum=int(config.get("quorum") or 3),
            confidence_threshold=float(config.get("confidence_threshold") or 0.6),
            current_replicas=current,
        )

        rationale = outcome.aggregation_summary
        if outcome.has_recommendation:
            rationale = self._synthesize(
                snapshot, config, request_id, deadline, outcome, current
            )
            logger.info(
                "Request %s -> %s@%d confidence=%.3f",
                request_id, outcome.action, outcome.target, outcome.confidence,
            )
        else:
            logger.info(
                "Request %s -> no recommendation: %s",
                request_id, outcome.no_recommendation_reason,
            )

        # --- Build the response ---
        response = pb2.ReasonScalingResponse()
        for r in analyst_results:
            response.analyst_results.append(
                pb2.AnalystResult(
                    role=r.role,
                    action=r.action if r.is_valid_vote else "",
                    target=r.target if r.is_valid_vote else 0,
                    confidence=r.confidence if r.is_valid_vote else 0.0,
                    rationale=r.rationale,
                    abstention_reason=r.abstention_reason,
                    error=r.error,
                )
            )

        response.recommendation.CopyFrom(
            pb2.ScalingRecommendation(
                has_recommendation=outcome.has_recommendation,
                no_recommendation_reason=outcome.no_recommendation_reason,
                action=outcome.action,
                target=outcome.target,
                confidence=outcome.confidence,
                rationale=rationale,
                aggregation_summary=outcome.aggregation_summary,
            )
        )
        return response

    def _synthesize(self, snapshot, config, request_id, deadline, outcome, current):
        """Explain the fixed decision; fall back to the deterministic
        vote summary when synthesis fails or tampers (Algorithm 5)."""
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            return outcome.aggregation_summary

        llm_cfg = config.get("llm") or {}
        chat_request = llm_pb2.ChatRequest(
            messages=[
                llm_pb2.ChatMessage(
                    role="system",
                    content=SYSTEM_PROMPT.format(
                        role="synthesizer",
                        min_replicas=int(config.get("min_replicas") or 1),
                        max_replicas=int(config.get("max_replicas") or 1),
                    ),
                ),
                llm_pb2.ChatMessage(
                    role="user",
                    content=SYNTHESIZER_PROMPT.format(
                        action=outcome.action,
                        target=outcome.target,
                        current=current,
                        summary=outcome.aggregation_summary,
                    )
                    + "\n\n## Shared decision snapshot (JSON)\n"
                    + json.dumps(snapshot, sort_keys=True)
                    + "\n\nProduce your JSON explanation now.",
                ),
            ],
            model=llm_cfg.get("model", ""),
            temperature=0.2,
            json_mode=True,
            request_id=f"{request_id}:synthesizer",
            endpoint=llm_cfg.get("endpoint", ""),
            api_key=llm_cfg.get("api_key", ""),
        )

        try:
            response = self.llm_interface_stub.Chat(
                chat_request, timeout=remaining, wait_for_ready=False
            )
            return validate_synthesis(
                content=response.content,
                finish_reason=response.finish_reason,
                in_band_error=response.error,
                fixed_action=outcome.action,
                fixed_target=outcome.target,
            )
        except SynthesisRejected as exc:
            logger.info("Synthesis rejected (%s); using vote summary", exc)
        except grpc.RpcError as exc:
            logger.info("Synthesis RPC failed (%s); using vote summary", exc.code().name)
        except Exception as exc:  # defensive: synthesis must never break the cycle
            logger.info("Synthesis failed unexpectedly (%s); using vote summary", exc)
        return outcome.aggregation_summary

    # ------------------------------------------------------------------
    # Legacy Reason RPC (kept for compatibility with earlier scaffolding)
    # ------------------------------------------------------------------
    def Reason(self, request, context):
        """Perform reasoning on a given prompt (single-turn passthrough)."""
        logger.info(f"Received reasoning request: {request.prompt[:50]}... (ID: {request.request_id})")

        if not request.prompt:
            context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
            context.set_details("Prompt cannot be empty")
            return pb2.ReasoningResponse()

        if self.llm_interface_stub is None:
            context.set_code(grpc.StatusCode.FAILED_PRECONDITION)
            context.set_details("LLM Interface connection is not initialized")
            return pb2.ReasoningResponse()

        try:
            response = self.llm_interface_stub.Chat(
                llm_pb2.ChatRequest(
                    messages=[
                        llm_pb2.ChatMessage(role="user", content=request.prompt),
                    ],
                    model="",
                    temperature=request.temperature,
                    max_tokens=request.max_tokens,
                    request_id=request.request_id,
                ),
                timeout=DEFAULT_REASONING_TIMEOUT_SECONDS,
            )
        except grpc.RpcError as exc:
            logger.error(f"Reason request {request.request_id} failed: {exc.code().name}")
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(f"LLM Interface RPC failed: {exc.code().name}")
            return pb2.ReasoningResponse()

        return pb2.ReasoningResponse(
            text=response.content,
            token_count=response.completion_tokens,
            finish_reason=0 if response.finish_reason == "stop" else 1,
            request_id=request.request_id,
            reasoning_steps=["single-turn passthrough via LLM Interface Chat"],
        )

    def GetStatus(self, request, context):
        """Get the current status of the reasoning component."""
        logger.info("Received status request")

        llm_healthy = self.llm_interface_stub is not None
        status_msg = "Multi Agent Reasoning service is running"
        if not llm_healthy:
            status_msg = "Multi Agent Reasoning service is degraded - LLM Interface connection unavailable"

        return pb2.StatusResponse(
            is_healthy=llm_healthy,
            status_message=status_msg,
            timestamp=int(time.time()),
            version="0.2.0",
        )


def serve_grpc_server(reasoning_servicer: ReasoningService, host: str, port: int):
    """Run the gRPC server until interrupted (shared lifecycle helper)."""
    run_grpc_server(
        reasoning_servicer,
        pb2_grpc.add_MultiAgentReasoningServiceServicer_to_server,
        host=host,
        port=port,
        service_name="Multi Agent Reasoning",
    )


def start_component(
    llm_interface_host: str = "localhost",
    llm_interface_port: int = 50052,
    reasoning_host: str = "localhost",
    reasoning_port: int = 50055,
) -> None:
    """
    Start the Multi Agent Reasoning component.

    Args:
        llm_interface_host: Hostname of the LLM Interface service
        llm_interface_port: Port of the LLM Interface service
        reasoning_host: Hostname for this service to listen on
        reasoning_port: Port for this service to listen on
    """
    logger.info("Starting Multi Agent Reasoning component...")
    logger.info(f"LLM Interface service: {llm_interface_host}:{llm_interface_port}")
    logger.info(f"Listening for gRPC connections on {reasoning_host}:{reasoning_port}")

    reasoning_servicer = ReasoningService()

    try:
        reasoning_servicer.initialize_outgoing_connections(
            llm_interface_host, llm_interface_port
        )
    except Exception as e:
        logger.error(f"Failed to initialize outgoing connections: {e}")
        # Continue anyway - the service can still operate in a degraded mode

    try:
        serve_grpc_server(reasoning_servicer, reasoning_host, reasoning_port)
    except Exception as e:
        logger.error(f"Failed to start Multi Agent Reasoning component: {e}")
        raise


def main():
    """Main entry point for the CLI."""
    parser = argparse.ArgumentParser(
        description="Multi Agent Reasoning Component",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Start with default connection parameters
  python -m multi_agent_reasoning.main

  # Start with custom connection parameters
  python -m multi_agent_reasoning.main --llm-interface-host llm-service --multi-agent-reasoning-port 50055
        """,
    )

    parser.add_argument(
        "--llm-interface-host",
        type=str,
        default="localhost",
        help="Hostname of the LLM Interface service (default: localhost)",
    )
    parser.add_argument(
        "--llm-interface-port",
        type=int,
        default=50052,
        help="Port of the LLM Interface service (default: 50052)",
    )
    parser.add_argument(
        "--multi-agent-reasoning-host",
        type=str,
        default="0.0.0.0",
        help="Hostname for this service to listen on (default: 0.0.0.0)",
    )
    parser.add_argument(
        "--multi-agent-reasoning-port",
        type=int,
        default=50055,
        help="Port for this service to listen on (default: 50055)",
    )

    args = parser.parse_args()

    try:
        start_component(
            llm_interface_host=args.llm_interface_host,
            llm_interface_port=args.llm_interface_port,
            reasoning_host=args.multi_agent_reasoning_host,
            reasoning_port=args.multi_agent_reasoning_port,
        )
    except Exception as e:
        logger.error(f"Failed to start Multi Agent Reasoning component: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
