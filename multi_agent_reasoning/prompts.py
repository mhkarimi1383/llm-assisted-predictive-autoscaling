"""Prompt templates for the specialist analyst agents (Algorithm 4).

Every prompt enforces the same response contract (Algorithm 3): the
model responds with a single JSON object and nothing else. The JSON
contract mirrors the AnalystResult fields:

    {"action": "SCALE_UP" | "SCALE_DOWN" | "HOLD",
     "target": <integer replica count>,
     "confidence": <float in [0, 1]>,
     "rationale": "<concise, evidence-citing justification>"}

or, when the required evidence is missing:

    {"abstention_reason": "<why the analyst cannot judge>"}

Telemetry, history, and business context are input data only; the
model never invents policy identifiers or execution authority.
"""

SYSTEM_PROMPT = """You are the {role} analyst in a Kubernetes autoscaling decision system.
Your analysis is one vote among several specialist analysts; a deterministic
safety engine makes the final call, so you never have execution authority.

Respond with EXACTLY one JSON object and nothing else (no markdown, no prose).
Allowed JSON shapes:

Decision:
  {{"action": "SCALE_UP" | "SCALE_DOWN" | "HOLD",
     "target": <integer replica count>,
     "confidence": <number between 0 and 1>,
     "rationale": "<one or two sentences citing snapshot fields>"}}

Abstention (use when the evidence required for your role is missing or failed):
  {{"abstention_reason": "<why you cannot judge>"}}

Hard rules:
- action SCALE_UP requires target > current_replicas.
- action SCALE_DOWN requires target < current_replicas.
- action HOLD requires target == current_replicas.
- target must lie within [{min_replicas}, {max_replicas}].
- confidence is your self-reported certainty in [0, 1]; it is not a measured
  probability of correctness.
- Cite only evidence present in the snapshot; never invent metrics.
"""

ROLE_OBJECTIVES = {
    "forecast": (
        "Assess predicted demand over the forecast horizon: trend, spike timing, "
        "and uncertainty. Use the forecast points and their predicted values. "
        "If the forecast is missing or forecast_error is set, abstain. "
        "Recommend the replica count the predicted demand appears to require, "
        "staying within the configured bounds."
    ),
    "status": (
        "Assess the current cluster observation: CPU and memory utilization "
        "relative to requests, ready vs desired replicas, and pod health. "
        "If metrics are missing or metrics_error is set, abstain. "
        "Recommend the replica count the current pressure indicates."
    ),
    "policy": (
        "Explain which configured limits constrain this decision (bounds, "
        "cooldown, rate limits, stabilization) and whether the current state "
        "is already inside them. Your opinion is advisory: the deterministic "
        "safety engine enforces constraints after aggregation. Vote HOLD when "
        "no constraint clearly demands movement."
    ),
    "history": (
        "Compare recent scaling actions for oscillation, reversals, and "
        "repeatedly blocked proposals. If recent_actions is absent or empty, "
        "abstain: do not speculate without evidence."
    ),
}

USER_PROMPT = """## Analyst role
{{"role": "{role}"}}

## Objective
{objective}

## Decision context
- current_replicas: {current_replicas}
- allowed replica range: [{min_replicas}, {max_replicas}]

## Shared decision snapshot (JSON)
{snapshot_json}

## Scaling history (JSON, may be null)
{history_json}

Produce your JSON decision or abstention now."""
