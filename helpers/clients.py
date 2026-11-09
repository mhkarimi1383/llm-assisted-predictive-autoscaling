"""Shared service client registry.

One place constructs every typed gRPC stub, so components never
repeat connection wiring. Stubs are created lazily on first access
and cached; the underlying channels live in helpers.connection's
shared cache.
"""

from __future__ import annotations

import logging
from typing import Optional

from protos import data_acquisition_pb2_grpc as da_pb2_grpc
from protos import llm_interface_pb2_grpc as llm_pb2_grpc
from protos import multi_agent_reasoning_pb2_grpc as mar_pb2_grpc
from protos import safety_policy_pb2_grpc as sp_pb2_grpc
from protos import scaling_executor_pb2_grpc as se_pb2_grpc

from helpers.connection import create_grpc_stub

logger = logging.getLogger(__name__)

DEFAULT_HOSTS = {
    "data_acquisition": ("localhost", 50051),
    "llm_interface": ("localhost", 50052),
    "safety_policy": ("localhost", 50053),
    "scaling_executor": ("localhost", 50054),
    "multi_agent_reasoning": ("localhost", 50055),
}


class ServiceClients:
    """Typed lazy accessors for the five backing services."""

    def __init__(self, hosts: Optional[dict] = None):
        self._hosts = dict(DEFAULT_HOSTS)
        if hosts:
            self._hosts.update(hosts)

    def _stub(self, name: str, stub_class):
        host, port = self._hosts[name]
        return create_grpc_stub(host, port, name.replace("_", " ").title(), stub_class)

    def data_acquisition(self) -> da_pb2_grpc.DataAcquisitionServiceStub:
        return self._stub("data_acquisition", da_pb2_grpc.DataAcquisitionServiceStub)

    def llm_interface(self) -> llm_pb2_grpc.LLMInterfaceServiceStub:
        return self._stub("llm_interface", llm_pb2_grpc.LLMInterfaceServiceStub)

    def multi_agent_reasoning(self) -> mar_pb2_grpc.MultiAgentReasoningServiceStub:
        return self._stub("multi_agent_reasoning", mar_pb2_grpc.MultiAgentReasoningServiceStub)

    def safety_policy(self) -> sp_pb2_grpc.SafetyPolicyServiceStub:
        return self._stub("safety_policy", sp_pb2_grpc.SafetyPolicyServiceStub)

    def scaling_executor(self) -> se_pb2_grpc.ScalingExecutorServiceStub:
        return self._stub("scaling_executor", se_pb2_grpc.ScalingExecutorServiceStub)
