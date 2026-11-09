"""Unit tests for Algorithm 3 (validation) and Algorithm 5 (aggregation)."""

import pytest

from multi_agent_reasoning.aggregation import aggregate
from multi_agent_reasoning.validation import (
    ValidationContext,
    ValidatedResult,
    parse_json_content,
    validate_analyst_result,
    validate_synthesis,
    SynthesisRejected,
)

CTX = ValidationContext(
    role="forecast",
    request_id="req-1",
    current_replicas=4,
    min_replicas=2,
    max_replicas=20,
)


def vote(role="forecast", action="SCALE_UP", target=6, confidence=0.9, rationale="r"):
    v = ValidatedResult(role=role)
    v.action = action
    v.target = target
    v.confidence = confidence
    v.rationale = rationale
    return v


# ----------------------------------------------------------------------
# Algorithm 3: analyst response validation
# ----------------------------------------------------------------------
class TestParseJsonContent:
    def test_plain_json(self):
        assert parse_json_content('{"action": "HOLD"}') == {"action": "HOLD"}

    def test_markdown_fenced(self):
        assert parse_json_content('```json\n{"action": "HOLD"}\n```') == {"action": "HOLD"}

    def test_prose_rejected(self):
        assert parse_json_content("I recommend scaling up.") is None

    def test_array_rejected(self):
        assert parse_json_content("[1, 2, 3]") is None

    def test_empty_rejected(self):
        assert parse_json_content("") is None
        assert parse_json_content("   ") is None


class TestValidateAnalystResult:
    def test_valid_decision(self):
        content = '{"action": "SCALE_UP", "target": 6, "confidence": 0.9, "rationale": "cpu high"}'
        r = validate_analyst_result("forecast", content, "stop", "", CTX)
        assert r.is_valid_vote
        assert r.action == "SCALE_UP"
        assert r.target == 6
        assert r.confidence == 0.9

    def test_transport_error_rejects(self):
        r = validate_analyst_result("forecast", "{}", "stop", "boom", CTX)
        assert r.error and not r.is_valid_vote

    def test_truncation_rejects(self):
        r = validate_analyst_result("forecast", '{"action": "HOLD"', "length", "", CTX)
        assert "truncated" in r.error

    def test_non_json_rejects(self):
        r = validate_analyst_result("forecast", "scale up please", "stop", "", CTX)
        assert "not a valid JSON object" in r.error

    def test_abstention_recorded(self):
        r = validate_analyst_result(
            "history", '{"abstention_reason": "no history"}', "stop", "", CTX
        )
        assert r.abstained
        assert r.abstention_reason == "no history"
        assert not r.is_valid_vote

    def test_unknown_action_rejects(self):
        r = validate_analyst_result(
            "forecast", '{"action": "GROW", "target": 6, "confidence": 0.9}', "stop", "", CTX
        )
        assert "invalid action" in r.error

    def test_float_target_rejects(self):
        r = validate_analyst_result(
            "forecast", '{"action": "SCALE_UP", "target": 6.5, "confidence": 0.9}', "stop", "", CTX
        )
        assert "integer" in r.error

    def test_direction_mismatch_rejects(self):
        r = validate_analyst_result(
            "forecast", '{"action": "SCALE_UP", "target": 4, "confidence": 0.9}', "stop", "", CTX
        )
        assert "must exceed current" in r.error

        r = validate_analyst_result(
            "forecast", '{"action": "SCALE_DOWN", "target": 4, "confidence": 0.9}', "stop", "", CTX
        )
        assert "must be below current" in r.error

        r = validate_analyst_result(
            "forecast", '{"action": "HOLD", "target": 5, "confidence": 0.9}', "stop", "", CTX
        )
        assert "must equal current" in r.error

    def test_confidence_out_of_range_rejects(self):
        r = validate_analyst_result(
            "forecast", '{"action": "HOLD", "target": 4, "confidence": 1.5}', "stop", "", CTX
        )
        assert "outside [0, 1]" in r.error

    def test_target_out_of_bounds_rejects(self):
        r = validate_analyst_result(
            "forecast", '{"action": "SCALE_UP", "target": 25, "confidence": 0.9}', "stop", "", CTX
        )
        assert "outside configured bounds" in r.error


class TestValidateSynthesis:
    def test_json_explanation(self):
        out = validate_synthesis(
            '{"action": "SCALE_UP", "target": 6, "rationale": "because"}',
            "stop", "", "SCALE_UP", 6,
        )
        assert out == "because"

    def test_prose_explanation(self):
        out = validate_synthesis("Scale up because CPU is high.", "stop", "", "SCALE_UP", 6)
        assert out == "Scale up because CPU is high."

    def test_tampered_action_rejected(self):
        with pytest.raises(SynthesisRejected):
            validate_synthesis(
                '{"action": "SCALE_DOWN", "target": 6, "rationale": "x"}',
                "stop", "", "SCALE_UP", 6,
            )

    def test_tampered_target_rejected(self):
        with pytest.raises(SynthesisRejected):
            validate_synthesis(
                '{"action": "SCALE_UP", "target": 9, "rationale": "x"}',
                "stop", "", "SCALE_UP", 6,
            )

    def test_empty_rejected(self):
        with pytest.raises(SynthesisRejected):
            validate_synthesis("", "stop", "", "SCALE_UP", 6)


# ----------------------------------------------------------------------
# Algorithm 5: weighted voting and synthesis
# ----------------------------------------------------------------------
def base_config(**over):
    cfg = {
        "weights": {},
        "quorum": 3,
        "confidence_threshold": 0.6,
        "current_replicas": 4,
    }
    cfg.update(over)
    return cfg


def run_aggregation(results, **over):
    cfg = base_config(**over)
    return aggregate(
        results=results,
        weights=cfg["weights"],
        quorum=cfg["quorum"],
        confidence_threshold=cfg["confidence_threshold"],
        current_replicas=cfg["current_replicas"],
    )


class TestAggregate:
    def test_majority_scale_up(self):
        outcome = run_aggregation(
            [
                vote("forecast", "SCALE_UP", 8, 0.9),
                vote("status", "SCALE_UP", 6, 0.8),
                vote("policy", "HOLD", 4, 0.9),
            ]
        )
        assert outcome.has_recommendation
        assert outcome.action == "SCALE_UP"
        # Weighted median of [6, 8] -> 6
        assert outcome.target == 6
        # support 2/3, mean conf ~0.85 -> min
        assert outcome.confidence == pytest.approx(2 / 3, abs=1e-6)

    def test_hold_wins(self):
        outcome = run_aggregation(
            [
                vote("forecast", "HOLD", 4, 0.9),
                vote("status", "HOLD", 4, 0.9),
                vote("policy", "HOLD", 4, 0.9),
            ]
        )
        assert outcome.has_recommendation
        assert outcome.action == "HOLD"
        assert outcome.target == 4

    def test_no_majority(self):
        outcome = run_aggregation(
            [
                vote("forecast", "SCALE_UP", 6, 0.9),
                vote("status", "SCALE_DOWN", 3, 0.9),
                vote("policy", "HOLD", 4, 0.9),
            ]
        )
        assert not outcome.has_recommendation
        assert "half of the voting weight" in outcome.no_recommendation_reason

    def test_quorum_count(self):
        outcome = run_aggregation([vote("forecast"), vote("status")])
        assert not outcome.has_recommendation
        assert "quorum not met" in outcome.no_recommendation_reason

    def test_required_roles(self):
        # Quorum count satisfied but forecast role missing.
        outcome = run_aggregation(
            [vote("status"), vote("policy"), vote("history")]
        )
        assert not outcome.has_recommendation
        assert "forecast" in outcome.no_recommendation_reason

    def test_confidence_threshold(self):
        outcome = run_aggregation(
            [
                vote("forecast", "SCALE_UP", 6, 0.5),
                vote("status", "SCALE_UP", 6, 0.5),
                vote("policy", "SCALE_UP", 6, 0.5),
            ],
            confidence_threshold=0.9,
        )
        assert not outcome.has_recommendation
        assert "below threshold" in outcome.no_recommendation_reason

    def test_weights_apply(self):
        # Policy weight 4 vs forecast+status 2: HOLD wins despite being
        # one analyst, and confidence reflects support 4/6.
        outcome = run_aggregation(
            [
                vote("forecast", "SCALE_UP", 6, 0.9),
                vote("status", "SCALE_UP", 6, 0.9),
                vote("policy", "HOLD", 4, 0.9),
            ],
            weights={"policy": 4.0},
        )
        assert outcome.has_recommendation
        assert outcome.action == "HOLD"
        assert outcome.confidence == pytest.approx(4 / 6, abs=1e-6)

    def test_abstentions_and_failures_reduce_valid_weight(self):
        abstain = ValidatedResult(role="history", abstained=True, abstention_reason="none")
        failed = ValidatedResult(role="policy", error="bad json")
        # With quorum 3 the two valid votes would not suffice; quorum 2
        # shows the abstention/failure excluded them from the vote.
        outcome = run_aggregation(
            [vote("forecast", "SCALE_UP", 6, 0.9), vote("status", "SCALE_UP", 6, 0.9), abstain, failed],
            quorum=2,
        )
        assert outcome.has_recommendation
        assert outcome.action == "SCALE_UP"
        # Full support (2/2), capped by mean winning confidence 0.9.
        assert outcome.confidence == pytest.approx(0.9, abs=1e-6)
        assert "abstained: history" in outcome.aggregation_summary
        assert "failed: policy" in outcome.aggregation_summary

    def test_abstentions_count_toward_quorum_shortfall(self):
        abstain = ValidatedResult(role="history", abstained=True, abstention_reason="none")
        failed = ValidatedResult(role="policy", error="bad json")
        outcome = run_aggregation(
            [vote("forecast", "SCALE_UP", 6, 0.9), vote("status", "SCALE_UP", 6, 0.9), abstain, failed]
        )
        assert not outcome.has_recommendation
        assert "quorum not met: 2 valid votes, 3 required" in outcome.no_recommendation_reason

    def test_weighted_median(self):
        # Targets 6 (w1) and 10 (w3): median is 10 (cumulative 3 >= 2).
        outcome = run_aggregation(
            [
                vote("forecast", "SCALE_UP", 6, 0.9),
                vote("status", "SCALE_UP", 10, 0.9),
                vote("policy", "SCALE_UP", 10, 0.9),
            ],
            weights={"forecast": 1.0, "status": 3.0, "policy": 3.0},
        )
        assert outcome.has_recommendation
        assert outcome.target == 10
