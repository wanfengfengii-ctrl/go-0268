#!/usr/bin/env bash
#
# run_benzhi_smoke.sh — deterministic local smoke test for the service.
#
# Builds the server binary, starts it on a local address with a throwaway
# database, probes its health endpoint, and exercises the create → lock → get
# API flow over localhost. It performs no external network access, captures
# every curl response in a variable before asserting (never "curl | grep"),
# and cleans up the server process and all temporary files on exit.
set -euo pipefail

cd "$(dirname "$0")"

PORT="${PORT:-18080}"
ADDR="127.0.0.1:${PORT}"
TMPDIR="$(pwd)/.smoke-tmp-$$"
SERVER_PID=""

cleanup() {
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  rm -rf "${TMPDIR}"
}
trap cleanup EXIT

mkdir -p "${TMPDIR}"

echo "==> building server binary"
go build -o "${TMPDIR}/server" ./cmd/server

echo "==> starting server on ${ADDR}"
DB_PATH="${TMPDIR}/smoke.db" ADDR="${ADDR}" "${TMPDIR}/server" &
SERVER_PID=$!

echo "==> waiting for health"
health=""
for _ in $(seq 1 100); do
  if health="$(curl -s "http://${ADDR}/healthz" 2>/dev/null || true)"; then
    if [[ "${health}" == *'"status":"ok"'* ]]; then
      break
    fi
  fi
  sleep 0.1
done

if [[ "${health}" != *'"status":"ok"'* ]]; then
  echo "health check failed: ${health}" >&2
  exit 1
fi
echo "==> health ok: ${health}"

echo "==> creating task"
create_resp="$(curl -s -X POST "http://${ADDR}/api/v1/tasks" \
  -H 'Content-Type: application/json' \
  -d '{"id":"smoke-1","leaf_batch":"batch-smoke","garden_plot":"plot-east","picking_round":"round-spring","operation_id":"op-create"}')"
if [[ "${create_resp}" != *'"id":"smoke-1"'* ]]; then
  echo "create task failed: ${create_resp}" >&2
  exit 1
fi
echo "==> create ok"

echo "==> locking task"
lock_resp="$(curl -s -X POST "http://${ADDR}/api/v1/tasks/smoke-1/lock" \
  -H 'Content-Type: application/json' \
  -d '{"operation_id":"op-lock","rule_digest":"rule-demo-v1","basket_seals":["basket-1","basket-2"],"blind_codes":["blind-1","blind-2","blind-3"],"tenderness_points":["tp-1"],"assay_wells":["well-1"],"fixation_slots":["fix-1"],"air_branches":["air-a"],"withering_slots":["slot-1"],"receipt_persons":["recv-a","recv-b"],"review_persons":["rev-x","rev-y"]}')"
if [[ "${lock_resp}" != *'"state":"pending_receipt"'* ]]; then
  echo "lock task failed: ${lock_resp}" >&2
  exit 1
fi
echo "==> lock ok"

echo "==> reading task"
get_resp="$(curl -s "http://${ADDR}/api/v1/tasks/smoke-1")"
if [[ "${get_resp}" != *'"generation":1'* ]]; then
  echo "get task failed: ${get_resp}" >&2
  exit 1
fi
echo "==> get ok: ${get_resp}"

echo "==> smoke test passed"
