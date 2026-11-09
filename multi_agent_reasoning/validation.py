"""Response validation for analyst and synthesizer results (Algorithm 3).

Every model response is treated as untrusted input: it must be complete,
untruncated, valid JSON, and consistent with the shared snapshot before
it may participate in aggregation. A failing response becomes a recorded
failure with reasons; it is never silently repaired.
"""

from __future__ import annotations

import json
import logging
import re
from dataclasses import dataclass, field
from typing import Any, Optional

logger = logging.getLogger(__name__)

ALLOWED_ACTIONS = ("SCALE_UP", "SCALE_DOWN", "HOLD")

# Model wrappers sometimes wrap JSON in markdown fences despite
# json_mode; strip them before parsing.
_FENCE_RE = re.compile(r"^\s*```(?:json)?\s*(.*?)\s*```\s*$", re.DOTALL)


@dataclass
class ValidatedResult:
    """A validated analyst outcome (AnalystResult in the contract)."""

    role: str
    action: str = ""
    target: int = 0
    confidence: float = 0.0
    rationale: str = ""
    abstained: bool = False
    abstention_reason: str = ""
    error: str = ""
    evidence: dict = field(default_factory=dict)

    @property
    def is_valid_vote(self) -> bool:
        """True when this result counts as a validated vote."""
        return not self.abstained and not self.error and self.action != ""


@dataclass
class ValidationContext:
    """Snapshot facts every result is checked against."""

    role: str
    request_id: str
    current_replicas: int
    min_replicas: int
    max_replicas: int


def parse_json_content(content: str) -> Optional[dict]:
    """Parse the model content into a JSON object.

    Returns None when the content is not a JSON object; the caller
    records the failure (Algorithm 3: no partial acceptance).
    """
    if not content or not content.strip():
        return None
    text = content.strip()

    fenced = _FENCE_RE.match(text)
    if fenced:
        text = fenced.group(1)

    try:
        parsed = json.loads(text)
    except json.JSONDecodeError as exc:
        logger.debug("JSON parse failed: %s", exc)
        return None

    return parsed if isinstance(parsed, dict) else None


def validate_analyst_result(
    role: str,
    content: str,
    finish_reason: str,
    in_band_error: str,
    ctx: ValidationContext,
) -> ValidatedResult:
    """Validate one analyst response (Algorithm 3).

    Checks, in order:
      1. transport/in-band failures and truncation reject the response,
      2. the content must parse to a JSON object,
      3. an abstention object is recorded as an abstention,
      4. a decision object must have a known action, an integer target
         consistent with the action and current replica count, a finite
         confidence in [0, 1], and a target inside the configured bounds.
    """
    result = ValidatedResult(role=role)

    if in_band_error:
        result.error = f"LLM interface error: {in_band_error}"
        return result

    if finish_reason == "length":
        result.error = "response truncated (finish_reason=length)"
        return result

    parsed = parse_json_content(content)
    if parsed is None:
        result.error = "response is not a valid JSON object"
        return result

    abstention = parsed.get("abstention_reason")
    if isinstance(abstention, str) and abstention.strip():
        result.abstained = True
        result.abstention_reason = abstention.strip()
        return result

    action = parsed.get("action")
    target = parsed.get("target")
    confidence = parsed.get("confidence")

    if action not in ALLOWED_ACTIONS:
        result.error = f"invalid action {action!r}"
        return result

    if isinstance(target, bool) or not isinstance(target, int):
        result.error = f"target must be an integer, got {target!r}"
        return result

    if isinstance(confidence, bool) or not isinstance(confidence, (int, float)):
        result.error = f"confidence must be a number, got {confidence!r}"
        return result
    confidence = float(confidence)
    if confidence != confidence or confidence in (float("inf"), float("-inf")):
        result.error = "confidence must be finite"
        return result
    if not 0.0 <= confidence <= 1.0:
        result.error = f"confidence {confidence} outside [0, 1]"
        return result

    # Action and target must agree with the snapshot's replica count.
    if action == "SCALE_UP" and target <= ctx.current_replicas:
        result.error = (
            f"SCALE_UP target {target} must exceed current {ctx.current_replicas}"
        )
        return result
    if action == "SCALE_DOWN" and target >= ctx.current_replicas:
        result.error = (
            f"SCALE_DOWN target {target} must be below current {ctx.current_replicas}"
        )
        return result
    if action == "HOLD" and target != ctx.current_replicas:
        result.error = (
            f"HOLD target {target} must equal current {ctx.current_replicas}"
        )
        return result

    # Bounds are checked here for reasoning quality; the deterministic
    # safety engine remains the authority (Algorithm 6) and clamps.
    if not ctx.min_replicas <= target <= ctx.max_replicas:
        result.error = (
            f"target {target} outside configured bounds "
            f"[{ctx.min_replicas}, {ctx.max_replicas}]"
        )
        return result

    rationale = parsed.get("rationale", "")
    result.action = action
    result.target = target
    result.confidence = confidence
    result.rationale = rationale if isinstance(rationale, str) else ""
    return result


class SynthesisRejected(Exception):
    """The synthesizer failed or tried to change the fixed decision."""


def validate_synthesis(
    content: str,
    finish_reason: str,
    in_band_error: str,
    fixed_action: str,
    fixed_target: int,
) -> str:
    """Validate the synthesizer explanation for a fixed decision.

    Returns the explanation text, or raises SynthesisRejected when the
    synthesis failed or tried to change the fixed action/target; the
    caller then attaches a deterministic voting summary (Algorithm 5).
    """

    def reject(reason: str) -> None:
        raise SynthesisRejected(reason)

    if in_band_error:
        reject(f"LLM interface error: {in_band_error}")
    if finish_reason == "length":
        reject("synthesis truncated (finish_reason=length)")

    parsed = parse_json_content(content)
    if parsed is None:
        # Accept plain prose explanations; only reject when empty.
        explanation = content.strip() if content else ""
        if not explanation:
            reject("synthesis content empty")
        return explanation

    action = parsed.get("action")
    if action is not None and action != fixed_action:
        reject(f"synthesis changed action {fixed_action!r} to {action!r}")
    target = parsed.get("target")
    if target is not None and target != fixed_target:
        reject(f"synthesis changed target {fixed_target} to {target!r}")

    explanation = parsed.get("rationale") or parsed.get("explanation") or ""
    if not isinstance(explanation, str) or not explanation.strip():
        reject("synthesis JSON lacks a rationale")
    return explanation.strip()
