"""Shared pytest fixtures: put the project root on sys.path so tests
can import the protos package and the multi_agent_reasoning modules
the same way the services do."""

import os
import sys

_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
if _ROOT not in sys.path:
    sys.path.insert(0, _ROOT)
