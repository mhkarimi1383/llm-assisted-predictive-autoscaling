#!/usr/bin/env python3
"""
Multi Agent Reasoning Component - CLI Entry Point
This component implements multi-agent reasoning capabilities for the system.
It serves gRPC connections for external clients to connect to.
"""

import argparse
import sys
import logging
import threading
import time
import grpc
from typing import Optional

from protos import multi_agent_reasoning_pb2 as pb2
from protos import multi_agent_reasoning_pb2_grpc as pb2_grpc
from protos import llm_interface_pb2 as llm_pb2
from protos import llm_interface_pb2_grpc as llm_pb2_grpc
from protos import safety_policy_pb2 as sp_pb2
from protos import safety_policy_pb2_grpc as sp_pb2_grpc
from protos import data_acquisition_pb2 as da_pb2
from protos import data_acquisition_pb2_grpc as da_pb2_grpc
from protos import scaling_executor_pb2 as se_pb2
from protos import scaling_executor_pb2_grpc as se_pb2_grpc

# Helper functions
sys.path.append('.')
from helpers.connection import create_grpc_stub

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


class ReasoningService(pb2_grpc.MultiAgentReasoningServiceServicer):
    """Implementation of the MultiAgentReasoningService."""

    def __init__(self):
        # Client connections for outgoing gRPC calls
        self.llm_interface_client = None
        self.safety_policy_client = None
        self.data_acquisition_client = None
        self.scaling_executor_client = None

    def initialize_outgoing_connections(self, llm_interface_host: str, llm_interface_port: int,
                                      safety_policy_host: str, safety_policy_port: int,
                                      data_acquisition_host: str, data_acquisition_port: int,
                                      scaling_executor_host: str, scaling_executor_port: int):
        """Initialize outgoing gRPC connections to all services."""
        try:
            # Initialize gRPC connection to LLM Interface service
            self.llm_interface_client = create_grpc_stub(
                llm_interface_host, llm_interface_port, "LLM Interface",
                llm_pb2_grpc.LLMInterfaceServiceStub
            )

            # Initialize gRPC connection to Safety Policy service
            self.safety_policy_client = create_grpc_stub(
                safety_policy_host, safety_policy_port, "Safety Policy",
                sp_pb2_grpc.SafetyPolicyServiceStub
            )

            # Initialize gRPC connection to Data Acquisition service
            self.data_acquisition_client = create_grpc_stub(
                data_acquisition_host, data_acquisition_port, "Data Acquisition",
                da_pb2_grpc.DataAcquisitionServiceStub
            )

            # Initialize gRPC connection to Scaling Executor service
            self.scaling_executor_client = create_grpc_stub(
                scaling_executor_host, scaling_executor_port, "Scaling Executor",
                se_pb2_grpc.ScalingExecutorServiceStub
            )
        except Exception as e:
            logger.error(f"Failed to initialize outgoing connections: {e}")
            raise

    def Reason(self, request, context):
        """Perform reasoning on a given prompt."""
        logger.info(f"Received reasoning request: {request.prompt[:50]}... (ID: {request.request_id})")

        # Validate request
        if not request.prompt:
            context.set_code(grpc.StatusCode.INVALID_ARGUMENT)
            context.set_details("Prompt cannot be empty")
            return pb2.ReasoningResponse()

        try:
            # In a real implementation, we would:
            # 1. Optionally check the prompt with the Safety Policy service
            # 2. Send the prompt to the LLM Interface service for actual reasoning
            # 3. Process and potentially refine the response
            # 4. Return the final reasoning result

            # For now, simulate a more sophisticated response
            prompt = request.prompt

            # Simulate different reasoning approaches based on prompt characteristics
            if "?" in prompt:
                reasoning_type = "analytical"
                answer = f"Based on my analysis of your question '{prompt}', here's my reasoned response."
            elif len(prompt) > 100:
                reasoning_type = "comprehensive"
                answer = f"After careful consideration of your detailed prompt, I've synthesized the following insights."
            else:
                reasoning_type = "direct"
                answer = f"Here's my reasoned response to your prompt: '{prompt}'"

            # Simulate token count based on response length
            token_count = max(10, len(answer) // 4)  # Rough estimate

            # Create reasoning steps
            reasoning_steps = [
                "Received and validated request",
                f"Identified reasoning approach: {reasoning_type}",
                "Consulted knowledge base",
                "Applied logical reasoning framework",
                "Generated response",
                "Performed safety check (simulated)",
                "Formatted final output"
            ]

            # Add request ID echo and reasoning steps
            response = pb2.ReasoningResponse(
                text=answer,
                token_count=token_count,
                finish_reason=0,  # STOP
                request_id=request.request_id,
                reasoning_steps=reasoning_steps
            )

            logger.info(f"Completed reasoning request {request.request_id} with {token_count} tokens")
            return response

        except Exception as e:
            logger.error(f"Error processing reasoning request {request.request_id}: {e}")
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(f"Internal error: {str(e)}")
            return pb2.ReasoningResponse()

    def GetStatus(self, request, context):
        """Get the current status of the reasoning component."""
        logger.info("Received status request")

        # Check health of outgoing connections
        llm_healthy = self.llm_interface_client is not None
        sp_healthy = self.safety_policy_client is not None
        da_healthy = self.data_acquisition_client is not None
        se_healthy = self.scaling_executor_client is not None
        is_healthy = llm_healthy and sp_healthy and da_healthy and se_healthy

        status_msg = "Multi Agent Reasoning service is running"
        if not is_healthy:
            status_msg = "Multi Agent Reasoning service is degraded"
            if not llm_healthy:
                status_msg += " - LLM Interface connection unavailable"
            if not sp_healthy:
                status_msg += " - Safety Policy connection unavailable"
            if not da_healthy:
                status_msg += " - Data Acquisition connection unavailable"
            if not se_healthy:
                status_msg += " - Scaling Executor connection unavailable"

        response = pb2.StatusResponse(
            is_healthy=is_healthy,
            status_message=status_msg,
            timestamp=int(time.time()),
            version="0.1.0"
        )
        return response


def serve_grpc_server(reasoning_servicer: ReasoningService, host: str, port: int) -> grpc.Server:
    """Start the gRPC server to listen for incoming connections."""
    server = grpc.server(threading.ThreadPoolExecutor(max_workers=10))
    pb2_grpc.add_MultiAgentReasoningServiceServicer_to_server(
        reasoning_servicer, server
    )
    server.add_insecure_port(f'{host}:{port}')
    server.start()
    logger.info(f"Multi Agent Reasoning gRPC server started on {host}:{port}")
    return server


def start_component(
    llm_interface_host: str = "localhost",
    llm_interface_port: int = 50052,
    safety_policy_host: str = "localhost",
    safety_policy_port: int = 50053,
    data_acquisition_host: str = "localhost",
    data_acquisition_port: int = 50051,
    scaling_executor_host: str = "localhost",
    scaling_executor_port: int = 50054,
    reasoning_host: str = "localhost",
    reasoning_port: int = 50055
) -> None:
    """
    Start the Multi Agent Reasoning component.

    Args:
        llm_interface_host: Hostname of the LLM Interface service
        llm_interface_port: Port of the LLM Interface service
        safety_policy_host: Hostname of the Safety Policy service
        safety_policy_port: Port of the Safety Policy service
        data_acquisition_host: Hostname of the Data Acquisition service
        data_acquisition_port: Port of the Data Acquisition service
        scaling_executor_host: Hostname of the Scaling Executor service
        scaling_executor_port: Port of the Scaling Executor service
        reasoning_host: Hostname for the Multi Agent Reasoning service to listen on
        reasoning_port: Port for the Multi Agent Reasoning service to listen on
    """
    logger.info("Starting Multi Agent Reasoning component...")
    logger.info(f"LLM Interface service: {llm_interface_host}:{llm_interface_port}")
    logger.info(f"Safety Policy service: {safety_policy_host}:{safety_policy_port}")
    logger.info(f"Listening for gRPC connections on {reasoning_host}:{reasoning_port}")

    # Initialize the reasoning service
    reasoning_servicer = ReasoningService()

    # Initialize outgoing connections to all services
    try:
        reasoning_servicer.initialize_outgoing_connections(
            llm_interface_host, llm_interface_port,
            safety_policy_host, safety_policy_port,
            data_acquisition_host, data_acquisition_port,
            scaling_executor_host, scaling_executor_port
        )
    except Exception as e:
        logger.error(f"Failed to initialize outgoing connections: {e}")
        # Continue anyway - the service can still operate in a degraded mode

    # Start the gRPC server
    server = None
    try:
        server = serve_grpc_server(reasoning_servicer, reasoning_host, reasoning_port)

        # Wait for termination signal
        logger.info("Multi Agent Reasoning component is running. Press Ctrl+C to stop.")
        while True:
            time.sleep(86400)  # Sleep for a day

    except KeyboardInterrupt:
        logger.info("Received shutdown signal...")
    except Exception as e:
        logger.error(f"Failed to start Multi Agent Reasoning component: {e}")
        raise
    finally:
        # Graceful shutdown
        logger.info("Shutting down Multi Agent Reasoning component...")
        if server:
            server.stop(0)
            logger.info("gRPC server stopped")
        logger.info("Multi Agent Reasoning component shutdown complete")


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
  python -m multi_agent_reasoning.main --llm-interface-host llm-service --llm-interface-port 50052 --multi-agent-reasoning-host reasoning.service --multi-agent-reasoning-port 9000
        """
    )

    # Connection parameters for LLM Interface service (required by this component)
    parser.add_argument(
        "--llm-interface-host",
        type=str,
        default="localhost",
        help="Hostname of the LLM Interface service (default: localhost)"
    )

    parser.add_argument(
        "--llm-interface-port",
        type=int,
        default=50052,
        help="Port of the LLM Interface service (default: 50052)"
    )

    # Connection parameters for Safety Policy service (required by this component)
    parser.add_argument(
        "--safety-policy-host",
        type=str,
        default="localhost",
        help="Hostname of the Safety Policy service (default: localhost)"
    )

    parser.add_argument(
        "--safety-policy-port",
        type=int,
        default=50053,
        help="Port of the Safety Policy service (default: 50053)"
    )

    # Connection parameters for Data Acquisition service (required by this component)
    parser.add_argument(
        "--data-acquisition-host",
        type=str,
        default="localhost",
        help="Hostname of the Data Acquisition service (default: localhost)"
    )

    parser.add_argument(
        "--data-acquisition-port",
        type=int,
        default=50051,
        help="Port of the Data Acquisition service (default: 50051)"
    )

    # Connection parameters for Scaling Executor service (required by this component)
    parser.add_argument(
        "--scaling-executor-host",
        type=str,
        default="localhost",
        help="Hostname of the Scaling Executor service (default: localhost)"
    )

    parser.add_argument(
        "--scaling-executor-port",
        type=int,
        default=50054,
        help="Port of the Scaling Executor service (default: 50054)"
    )

    # Connection parameters for Multi Agent Reasoning service (where this component listens for gRPC connections)
    parser.add_argument(
        "--multi-agent-reasoning-host",
        type=str,
        default="localhost",
        help="Hostname for the Multi Agent Reasoning service to listen on (default: localhost)"
    )

    parser.add_argument(
        "--multi-agent-reasoning-port",
        type=int,
        default=50055,
        help="Port for the Multi Agent Reasoning service to listen on (default: 50055)"
    )

    args = parser.parse_args()

    try:
        start_component(
            llm_interface_host=args.llm_interface_host,
            llm_interface_port=args.llm_interface_port,
            safety_policy_host=args.safety_policy_host,
            safety_policy_port=args.safety_policy_port,
            data_acquisition_host=args.data_acquisition_host,
            data_acquisition_port=args.data_acquisition_port,
            scaling_executor_host=args.scaling_executor_host,
            scaling_executor_port=args.scaling_executor_port,
            reasoning_host=args.multi_agent_reasoning_host,
            reasoning_port=args.multi_agent_reasoning_port
        )
    except Exception as e:
        logger.error(f"Failed to start Multi Agent Reasoning component: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()