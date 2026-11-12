#!/bin/sh
# Reachability probe. Asks the local mihomo core to reach the target through
# the proxy group and reports state changes to a webhook.
#
# Environment: CONTROLLER_PORT, PROBE_GROUP, PROBE_TARGET, PROBE_TARGET_ENCODED,
# PROBE_INTERVAL, PROBE_TIMEOUT_MS, PROBE_FAILURE_THRESHOLD, POD_NAME,
# POD_NAMESPACE; optional MIHOMO_CONTROLLER_SECRET, PROBE_WEBHOOK_URL.
set -u

endpoint="http://127.0.0.1:${CONTROLLER_PORT}/proxies/${PROBE_GROUP}/delay?url=${PROBE_TARGET_ENCODED}&timeout=${PROBE_TIMEOUT_MS}"
wget_timeout=$((PROBE_TIMEOUT_MS / 1000 + 5))

log() {
  echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) probe: $*"
}

check() {
  if [ -n "${MIHOMO_CONTROLLER_SECRET:-}" ]; then
    wget -q -T "$wget_timeout" -O /dev/null \
      --header "Authorization: Bearer ${MIHOMO_CONTROLLER_SECRET}" "$endpoint" 2>/dev/null
  else
    wget -q -T "$wget_timeout" -O /dev/null "$endpoint" 2>/dev/null
  fi
}

notify() {
  status="$1"
  message="$2"
  if [ -z "${PROBE_WEBHOOK_URL:-}" ]; then
    return 0
  fi
  body=$(printf '{"source":"mihomo-k8s","status":"%s","target":"%s","group":"%s","namespace":"%s","pod":"%s","message":"%s"}' \
    "$status" "$PROBE_TARGET" "$PROBE_GROUP" "$POD_NAMESPACE" "$POD_NAME" "$message")
  if wget -q -T 15 -O /dev/null --header "Content-Type: application/json" \
    --post-data "$body" "$PROBE_WEBHOOK_URL" 2>/dev/null; then
    log "webhook delivered (status=$status)"
  else
    log "webhook delivery FAILED (status=$status)"
    return 1
  fi
}

failures=0
firing=0
pending=""

log "target=${PROBE_TARGET} group=${PROBE_GROUP} interval=${PROBE_INTERVAL}s threshold=${PROBE_FAILURE_THRESHOLD}"

while true; do
  if check; then
    if [ "$failures" -gt 0 ]; then
      log "ok again after ${failures} failed check(s)"
    fi
    failures=0
    if [ "$firing" -eq 1 ]; then
      firing=0
      pending="resolved"
    fi
  else
    failures=$((failures + 1))
    log "check failed (${failures}/${PROBE_FAILURE_THRESHOLD})"
    if [ "$failures" -ge "$PROBE_FAILURE_THRESHOLD" ] && [ "$firing" -eq 0 ]; then
      firing=1
      pending="firing"
    fi
  fi

  # A webhook that could not be delivered is retried on the next cycle.
  case "$pending" in
    firing)
      notify firing "${PROBE_TARGET} is unreachable through proxy group ${PROBE_GROUP}" && pending=""
      ;;
    resolved)
      notify resolved "${PROBE_TARGET} is reachable again through proxy group ${PROBE_GROUP}" && pending=""
      ;;
  esac

  sleep "$PROBE_INTERVAL"
done
