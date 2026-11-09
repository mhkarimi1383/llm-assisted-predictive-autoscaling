"""Shared gRPC server lifecycle.

Every component's server reduces to: a servicer, its registration
function, and a listening port. This helper owns the start/wait/
graceful-stop loop once.
"""

from __future__ import annotations

import logging
import time
from concurrent.futures import ThreadPoolExecutor

import grpc

logger = logging.getLogger(__name__)

# Poll interval while idling; short enough that Ctrl+C feels snappy.
_IDLE_SLEEP_SECONDS = 86400


def run_grpc_server(
    servicer,
    add_servicer_to_server_fn,
    host: str = "localhost",
    port: int = 50051,
    service_name: str = "gRPC service",
    max_workers: int = 10,
) -> None:
    """Start the server, block until interrupted, and stop gracefully.

    Blocks the calling thread until KeyboardInterrupt or an unrecoverable
    server error; shutdown is always graceful.
    """
    server = grpc.server(ThreadPoolExecutor(max_workers=max_workers))
    add_servicer_to_server_fn(servicer, server)
    bound_port = server.add_insecure_port(f"{host}:{port}")
    if bound_port == 0:
        raise RuntimeError(f"{service_name} failed to bind {host}:{port}")
    server.start()
    logger.info(f"{service_name} gRPC server started on {host}:{port}")

    try:
        logger.info(f"{service_name} is running. Press Ctrl+C to stop.")
        while True:
            time.sleep(_IDLE_SLEEP_SECONDS)
    except KeyboardInterrupt:
        logger.info(f"{service_name} received shutdown signal...")
    finally:
        logger.info(f"Shutting down {service_name}...")
        server.stop(0)
        logger.info(f"{service_name} stopped")
