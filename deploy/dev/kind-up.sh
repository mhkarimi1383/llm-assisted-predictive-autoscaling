#!/usr/bin/env bash
# kind-up.sh: create a local kind cluster with the full LLM autoscaler
# control plane, metrics-server, and the demo workload.
#
# Usage:
#   ./deploy/dev/kind-up.sh              # build images + cluster + deploy
#   SKIP_BUILD=1 ./deploy/dev/kind-up.sh # reuse existing images
#   ./deploy/dev/kind-up.sh --dry-run    # deploy with LLMAutoscaler dryRun: true

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && cd -P . && pwd)"
cd "$ROOT"

CLUSTER_NAME="${CLUSTER_NAME:-llm-autoscaler}"
GO_IMAGE="${GO_IMAGE:-llm-autoscaler:dev}"
PY_IMAGE="${PY_IMAGE:-multi-agent-reasoning:dev}"
DRY_RUN="${DRY_RUN:-}"

if [[ "${1:-}" == "--dry-run" ]]; then
    DRY_RUN="1"
fi

echo "==> Building images"
if [[ "${SKIP_BUILD:-0}" != "1" ]]; then
    # On the host, run this script inside the nix dev shell
    # (nix develop). Inside the test VM, go is on PATH directly.
    CGO_ENABLED=0 go build -buildvcs=false -o llm-autoscaler .
    docker build -t "$GO_IMAGE" .
    docker build -f Dockerfile.python -t "$PY_IMAGE" .
fi

echo "==> Creating kind cluster '$CLUSTER_NAME' (if needed)"
if ! kind get clusters | grep -q "^$CLUSTER_NAME$"; then
    kind create cluster --name "$CLUSTER_NAME" --wait 90s
fi

echo "==> Loading images into kind"
kind load docker-image "$GO_IMAGE" --name "$CLUSTER_NAME"
kind load docker-image "$PY_IMAGE" --name "$CLUSTER_NAME"

echo "==> Installing metrics-server"
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
# kind ships without a CA usable for metrics-server's self-signed cert
kubectl -n kube-system patch deployment metrics-server \
    --type=json -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]'

echo "==> Waiting for metrics-server"
kubectl -n kube-system rollout status deployment/metrics-server --timeout=180s

echo "==> Applying CRD and control plane"
kubectl apply -f deploy/crd/autoscaling.karimi.dev_llmautoscalers.yaml
kubectl apply -f deploy/rbac/role.yaml
kubectl apply -f deploy/manifests/00-namespace.yaml
kubectl apply -f deploy/manifests/01-rbac.yaml
kubectl apply -f deploy/manifests/20-services.yaml

echo "==> Waiting for control plane"
kubectl -n llm-autoscaler-system rollout status deployment/data-acquisition --timeout=120s
kubectl -n llm-autoscaler-system rollout status deployment/llm-interface --timeout=120s
kubectl -n llm-autoscaler-system rollout status deployment/safety-policy --timeout=120s
kubectl -n llm-autoscaler-system rollout status deployment/scaling-executor --timeout=120s
kubectl -n llm-autoscaler-system rollout status deployment/multi-agent-reasoning --timeout=120s
kubectl -n llm-autoscaler-system rollout status deployment/operator --timeout=120s

echo "==> Deploying demo workload"
kubectl apply -f examples/demo/demo-workload.yaml

echo "==> Deploying LLMAutoscaler (dryRun variant)"
if [[ -n "$DRY_RUN" ]]; then
    sed 's/dryRun: false/dryRun: true/' examples/demo/llmautoscaler-mock.yaml | kubectl apply -f -
else
    kubectl apply -f examples/demo/llmautoscaler-mock.yaml
fi

echo
echo "Cluster '$CLUSTER_NAME' is ready. Observe the first decision cycle:"
echo "  kubectl -n default get llmautoscaler demo-frontend-mock -w"
echo "  kubectl -n default logs -f deployment/operator -c operator"
echo
echo "Teardown: make kind-down (or kind delete cluster --name $CLUSTER_NAME)"
