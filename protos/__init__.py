"""Generated protobuf/gRPC Python code lives flat in this directory.

protoc generates modules that import each other by bare module name
(e.g. ``import multi_agent_reasoning_pb2`` inside
``multi_agent_reasoning_pb2_grpc.py``). Publishing this directory on
``sys.path`` makes both import styles work:

    from protos import llm_interface_pb2            # package import
    import llm_interface_pb2                        # flat import (generated code)
"""

import os
import sys

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.append(_HERE)
