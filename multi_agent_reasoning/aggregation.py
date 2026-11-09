"""Weighted voting and synthesis (Algorithm 5).

Analyst results are aggregated deterministically: configured weights
sum per action, the winning action needs strictly more than half of
the valid voting weight, the winning target is the weighted median of
the winning analysts' targets, and the recommendation confidence is
the lesser of vote support and the mean winning confidence.

Weights come from configuration, never from the model. Agreement and
self-reported confidence are heuristic scores, not safety checks.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass, field
from typing import Optional

from .validation import ValidatedResult

logger = logging.getLogger(__name__)

DEFAULT_WEIGHT = 1.0


@dataclass
class AggregationOutcome:
    """Result of the aggregation stage, fixed before synthesis."""

    has_recommendation: bool
    action: str = ""
    target: int = 0
    confidence: float = 0.0
    aggregation_summary: str = ""
    no_recommendation_reason: str = ""
    votes: list = field(default_factory=list)  # ValidatedResult list


def _weighted_median(targets: list, weights: list) -> int:
    """First target whose cumulative weight reaches half the total."""
    pairs = sorted(zip(targets, weights), key=lambda p: p[0])
    total = sum(weights)
    cumulative = 0.0
    for target, weight in pairs:
        cumulative += weight
        if cumulative * 2 >= total:
            return int(target)
    return int(pairs[-1][0])


def _vote_summary(
    action_weights: dict,
    valid: list,
    abstentions: list,
    failures: list,
) -> str:
    """Deterministic summary of the vote for audit purposes."""
    parts = [f"{action}={weight:.2f}" for action, weight in sorted(action_weights.items())]
    summary = f"weights[{', '.join(parts) if parts else 'none'}]"
    if valid:
        summary += "; votes: " + ", ".join(
            f"{v.role}:{v.action}@{v.target}" for v in valid
        )
    if abstentions:
        summary += "; abstained: " + ", ".join(a.role for a in abstentions)
    if failures:
        summary += "; failed: " + ", ".join(f.role for f in failures)
    return summary


def aggregate(
    results: list,
    weights: dict,
    quorum: int,
    confidence_threshold: float,
    required_roles: tuple = ("forecast", "status"),
    current_replicas: int = 0,
) -> AggregationOutcome:
    """Aggregate analyst results into a fixed recommendation.

    results:   list[ValidatedResult] — validated votes, abstentions,
               and failures by role.
    weights:   per-role configured voting weights.
    quorum:    minimum number of validated votes.
    """
    valid = [r for r in results if r.is_valid_vote]
    abstentions = [r for r in results if r.abstained]
    failures = [r for r in results if r.error]

    summary = _vote_summary({}, valid, abstentions, failures)

    # --- Quorum: required roles and minimum count of valid votes ---
    present_roles = {r.role for r in valid}
    missing_required = [role for role in required_roles if role not in present_roles]
    if missing_required:
        return AggregationOutcome(
            has_recommendation=False,
            aggregation_summary=summary,
            no_recommendation_reason=(
                f"quorum not met: required roles absent: {', '.join(missing_required)}"
            ),
        )
    if len(valid) < quorum:
        return AggregationOutcome(
            has_recommendation=False,
            aggregation_summary=summary,
            no_recommendation_reason=(
                f"quorum not met: {len(valid)} valid votes, {quorum} required"
            ),
        )

    # --- Weighted majority: strictly more than half the valid weight ---
    action_weights: dict = {}
    total_weight = 0.0
    for r in valid:
        w = float(weights.get(r.role, DEFAULT_WEIGHT))
        if w < 0:
            w = 0.0
        action_weights[r.action] = action_weights.get(r.action, 0.0) + w
        total_weight += w

    winning_action = None
    winning_weight = 0.0
    for action, weight in action_weights.items():
        if weight * 2 > total_weight and weight > winning_weight:
            winning_action = action
            winning_weight = weight

    summary = _vote_summary(action_weights, valid, abstentions, failures)

    if winning_action is None:
        return AggregationOutcome(
            has_recommendation=False,
            aggregation_summary=summary,
            no_recommendation_reason=(
                "no action holds strictly more than half of the voting weight"
            ),
        )

    # --- Target: weighted median of the winning analysts' targets ---
    if winning_action == "HOLD":
        target = current_replicas
    else:
        winning = [r for r in valid if r.action == winning_action]
        target = _weighted_median(
            [r.target for r in winning],
            [float(weights.get(r.role, DEFAULT_WEIGHT)) for r in winning],
        )

    # --- Confidence: lesser of support and mean winning confidence ---
    support = winning_weight / total_weight if total_weight > 0 else 0.0
    winning = [r for r in valid if r.action == winning_action]
    mean_confidence = sum(r.confidence for r in winning) / len(winning)
    confidence = min(support, mean_confidence)

    if confidence < confidence_threshold:
        return AggregationOutcome(
            has_recommendation=False,
            action=winning_action,
            target=target,
            confidence=confidence,
            aggregation_summary=summary,
            no_recommendation_reason=(
                f"recommendation confidence {confidence:.3f} below threshold "
                f"{confidence_threshold:.3f}"
            ),
        )

    return AggregationOutcome(
        has_recommendation=True,
        action=winning_action,
        target=int(target),
        confidence=confidence,
        aggregation_summary=summary,
        votes=valid,
    )
