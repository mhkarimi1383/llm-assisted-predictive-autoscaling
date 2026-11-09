#!/usr/bin/env bash
# vm-test.sh: run the project's tests inside the NixOS test VM
# (vm.nix). The VM has its own Docker daemon, so kind end-to-end runs
# never touch the host container runtime.
#
# Usage:
#   ./deploy/dev/vm-test.sh            # unit tests (Go + Python)
#   ./deploy/dev/vm-test.sh --kind     # unit tests + kind end-to-end
#   ./deploy/dev/vm-test.sh --shell    # interactive SSH session only
#
# Teardown: kill $(cat vm/vm.pid). The VM root disk persists between
# boots, so caches (go, uv, kind images) survive restarts.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

SSH_PORT=2222
SSH_OPTS=(-p "$SSH_PORT" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null
  -o LogLevel=ERROR -o ConnectTimeout=5 -o BatchMode=yes)

# The VM shares the live project directory; vm.nix reads this env var
# at evaluation time.
export PROJECT_DIR="$ROOT"

run_script() {
  echo "==> Building test VM (first build takes a while)"
  nix build .#nixosConfigurations.vm.config.system.build.vm \
    --out-link vm/result --impure
  echo "vm/result/bin/"run-*-vm-vm
}

vm_running() {
  ssh "${SSH_OPTS[@]}" root@localhost true 2>/dev/null
}

boot_vm() {
  local script
  script="$(run_script)"
  if vm_running; then
    echo "==> VM already running"
    return
  fi
  echo "==> Booting VM (log: vm/vm.log)"
  mkdir -p vm
  nohup "$script" > vm/vm.log 2>&1 &
  echo $! > vm/vm.pid

  echo -n "==> Waiting for VM SSH"
  for _ in $(seq 1 150); do
    if vm_running; then
      echo " ready"
      return
    fi
    echo -n "."
    sleep 2
  done
  echo
  echo "ERROR: VM did not become reachable over SSH; check vm/vm.log" >&2
  exit 1
}

ssh_vm() {
  ssh "${SSH_OPTS[@]}" root@localhost "$@"
}

boot_vm

if [[ "${1:-}" == "--shell" ]]; then
  exec ssh -p "$SSH_PORT" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    root@localhost
fi

echo "==> Running unit tests in VM"
ssh_vm '
  set -euo pipefail
  cd /project
  export PATH="$GOPATH/bin:$PATH"

  echo "--- Python: uv sync + pytest"
  uv sync --frozen --extra dev
  "$UV_PROJECT_ENVIRONMENT/bin/python" -m pytest tests/ -q

  echo "--- Go: go test (static, no cgo)"
  CGO_ENABLED=0 go test ./...

  echo "--- Go: build"
  CGO_ENABLED=0 go build -buildvcs=false ./...
'

if [[ "${1:-}" == "--kind" || "${2:-}" == "--kind" ]]; then
  echo "==> Running kind end-to-end in VM"
  ssh_vm '
    set -euo pipefail
    cd /project
    ./deploy/dev/kind-up.sh
    sleep 90
    echo "--- LLMAutoscaler status"
    kubectl -n default get llmautoscaler demo-frontend-mock -o yaml | grep -A20 "^status:"
  '
fi

echo "==> VM tests done"
