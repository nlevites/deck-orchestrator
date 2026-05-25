#!/usr/bin/env bash
# Pillar 2A wire capture: boots the supervisor + 4 executors with JSON
# logs, runs scenario.py against the live stack, then asks the supervisor
# to stop the orchestrator + reaps everything. Per-run artifacts land in
# analysis/wire/runs/<timestamp>/.
#
# Prereq: `make build` (so backend/bin/* exist).
#
# Usage:
#   ./analysis/wire/capture.sh             # default ~60s scenario
#   ./analysis/wire/capture.sh --scenario all
#   ./analysis/wire/capture.sh --keep-stack   # don't tear down on exit

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

if [[ ! -x ./backend/bin/supervisor ]]; then
  echo "supervisor binary missing — run 'make build' first" >&2
  exit 1
fi

KEEP_STACK=0
SCENARIO_ARGS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --keep-stack) KEEP_STACK=1; shift ;;
    *) SCENARIO_ARGS+=("$1"); shift ;;
  esac
done

ts="$(date +%Y%m%dT%H%M%S)"
run_dir="$REPO_ROOT/analysis/wire/runs/$ts"
mkdir -p "$run_dir/logs"

# Port sweep: supervisor binds but doesn't unbind stragglers.
for p in 8080 8090 9001 9002 9003 9004; do
  lsof -nPti TCP:"$p" -sTCP:LISTEN 2>/dev/null | xargs -r kill -9 2>/dev/null || true
done

# Env vars override the YAML via cleanenv:
#  - STATE_DIR redirects supervisor child logs + per-deck DB.
#  - ORCH_DB_PATH lands the orchestrator DB next to the logs.
#  - LOG_FORMAT/LEVEL ensure NDJSON for orchestrator + executors too.
export STATE_DIR="$run_dir"
export ORCH_DB_PATH="$run_dir/orchestrator.db"
export LOG_FORMAT=json
export LOG_LEVEL=info

echo "wire-capture: run_dir=$run_dir"

# Start supervisor in background, log its own stderr to file.
"$REPO_ROOT/backend/bin/supervisor" \
  -config "$REPO_ROOT/analysis/wire/supervisor.analysis.yaml" \
  > "$run_dir/logs/supervisor.log" 2>&1 &
sup_pid=$!
echo "wire-capture: supervisor pid=$sup_pid"

cleanup() {
  if [[ "$KEEP_STACK" == "1" ]]; then
    echo "wire-capture: --keep-stack — leaving supervisor running (pid=$sup_pid)"
    return
  fi
  echo "wire-capture: stopping stack"
  curl -fsS -X POST http://localhost:8090/supervisor/orchestrator/stop > /dev/null 2>&1 || true
  kill -TERM "$sup_pid" 2>/dev/null || true
  sleep 1
  kill -KILL "$sup_pid" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# Wait for orchestrator + 4 executors to be ready.
for i in $(seq 1 60); do
  if curl -fsS http://localhost:8080/health > /dev/null 2>&1 \
     && curl -fsS http://localhost:8090/health > /dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

if ! curl -fsS http://localhost:8080/health > /dev/null 2>&1; then
  echo "wire-capture: orchestrator failed to come up; see $run_dir/logs/orchestrator.log" >&2
  exit 1
fi

# Give executors a couple seconds to register so the heartbeat steady-state
# fully appears in the capture.
sleep 3

# Run the scenario driver. scenario.py exits non-zero on assertion failure.
# `${arr[@]+"${arr[@]}"}` survives `set -u` when the array is empty.
ANALYSIS_RUN_DIR="$run_dir" \
  uv run --project analysis python -m analysis.wire.scenario \
    ${SCENARIO_ARGS[@]+"${SCENARIO_ARGS[@]}"}

# Brief soak so post-completion events (RUN_STATUS_CHANGED, deck idle
# heartbeats) appear in the capture.
sleep 4

echo "wire-capture: done. artifacts in $run_dir"
ls -la "$run_dir/logs"
