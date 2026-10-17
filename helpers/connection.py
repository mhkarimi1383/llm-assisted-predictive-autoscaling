"""
Helper functions for gRPC connections.
This module eliminates duplicate gRPC connection code between components.
"""

import grpc
import logging
from typing import Any, Type



connection_cache: dict[str, grpc.Channel] = {}

def create_grpc_stub(host: str, port: int, service_name: str, stub_class: Type[Any]) -> Any:
    """
    Creates a gRPC stub for the given service.

    Args:
        host: The hostname of the service
        port: The port of the service
        service_name: Human-readable name of the service (for logging)
        stub_class: The generated stub class to instantiate

    Returns:
        An instance of the stub_class connected to the service

    Raises:
        Exception: If the connection fails
    """
    if f"{host}:{port}" in connection_cache:
        logging.info(f"Using cached connection to {service_name} service at {host}:{port}")
        channel = connection_cache[f"{host}:{port}"]
        return stub_class(channel)
    try:
        channel = grpc.insecure_channel(f'{host}:{port}')
        stub = stub_class(channel)
        logging.info(f"Connected to {service_name} service at {host}:{port}")
        connection_cache[f"{host}:{port}"] = channel
        return stub
    except Exception as e:
        logging.error(f"Failed to connect to {service_name} service: {e}")
        raise