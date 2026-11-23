#!/usr/bin/env bash
# run-mode.sh <mode> — run one experiment with the standard 12-minute trace
# (baseline 10 rps → ramp to 40 → sustain → back to 10) and collect artifacts
# into docs/eval/data/<mode>/.
#
# Modes: fixed | hpa | llm-mock | llm
set -euo pipefail
MODE="${1:?mode: fixed|hpa|llm-mock|llm}"
DATA="docs/eval/data/$MODE"
mkdir -p "$DATA"

echo "== [$MODE] resetting state"
kubectl -n default delete hpa burn-hpa >/dev/null 2>&1 || true
kubectl -n default delete llmautoscaler burn-llmas >/dev/null 2>&1 || true
kubectl -n default scale deploy burn --replicas=2 >/dev/null
kubectl -n default rollout status deploy/burn --timeout=120s >/dev/null
sleep 45   # let metrics/HPA state settle and CPU go idle

echo "== [$MODE] applying autoscaler"
case "$MODE" in
  fixed)    : ;;
  hpa)      kubectl -n default apply -f examples/loadtest/hpa.yaml ;;
  llm-mock) sed 's/endpoint: "https:\/\/api.z.ai\/api\/paas\/v4"/endpoint: "mock:\/\/"/; s/name: zai-key/name: zai-key/' examples/loadtest/llmas-zai.yaml | kubectl apply -f - ;;
  llm)      kubectl -n default apply -f examples/loadtest/llmas-zai.yaml ;;
  *) echo "unknown mode $MODE"; exit 1 ;;
esac
if [ "$MODE" != "fixed" ]; then sleep 20; fi

echo "== [$MODE] starting sampler"
bash examples/loadtest/sample.sh "$MODE" "$DATA/samples.csv" &
SAMPLE_PID=$!

echo "== [$MODE] starting load (720 s)"
CGO_ENABLED=0 go build -buildvcs=false -o /tmp/loadgen examples/loadtest/loadgen/main.go
NODEIP=$(kubectl get node -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
cd /tmp && ./loadgen -url "http://$NODEIP:30080/burn" \
  -schedule "0:10,120:40,300:40,540:10" -duration 720 \
  -out "$OLDPWD/$DATA/persecond.jsonl" -summary "$OLDPWD/$DATA/summary.json"
cd "$OLDPWD"

wait $SAMPLE_PID 2>/dev/null || true

echo "== [$MODE] collecting artifacts"
# pod list with creation/ready timestamps → Pod Startup Time (rubric #5)
kubectl -n default get pods -l app=burn -o json > "$DATA/pods-final.json" 2>/dev/null || true
# scale events → exact scaling reaction times
kubectl -n default get events --field-selector involvedObject.kind=Deployment --sort-by=.lastTimestamp -o json > "$DATA/events-final.json" 2>/dev/null || true
kubectl -n default get deploy burn -o json > "$DATA/deploy-final.json" 2>/dev/null || true
if kubectl -n default get llmautoscaler burn-llmas >/dev/null 2>&1; then
  kubectl -n default get llmautoscaler burn-llmas -o json > "$DATA/llmas-final.json"
  kubectl -n llm-autoscaler-system logs deployment/operator --tail=200 > "$DATA/operator.log" 2>/dev/null || true
fi
kubectl -n default get hpa -o yaml > "$DATA/hpa-final.yaml" 2>/dev/null || true

echo "== [$MODE] done → $DATA/"
cat "$DATA/summary.json"
