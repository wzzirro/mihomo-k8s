#!/bin/sh
# Transparent client, init step: redirects the pod's outgoing TCP connections
# to the listed CIDRs into the local mihomo sidecar. Runs as root with
# NET_ADMIN and NET_RAW, inside the pod's own network namespace.
#
# Environment: REDIRECT_CIDRS (space separated), REDIRECT_PORT, PROXY_UID;
# optional IPTABLES (binary to use, default "iptables").
set -eu

ipt="${IPTABLES:-iptables}"
chain="MIHOMO_K8S"

# Idempotent: init containers run again when the pod sandbox is recreated.
"$ipt" -w -t nat -N "$chain" 2>/dev/null || "$ipt" -w -t nat -F "$chain"

# The sidecar's own connections must not loop back into itself.
"$ipt" -w -t nat -A "$chain" -m owner --uid-owner "$PROXY_UID" -j RETURN

for cidr in $REDIRECT_CIDRS; do
  "$ipt" -w -t nat -A "$chain" -p tcp -d "$cidr" -j REDIRECT --to-ports "$REDIRECT_PORT"
done

"$ipt" -w -t nat -C OUTPUT -p tcp -j "$chain" 2>/dev/null ||
  "$ipt" -w -t nat -A OUTPUT -p tcp -j "$chain"

echo "redirect: TCP to [$REDIRECT_CIDRS] -> 127.0.0.1:$REDIRECT_PORT (uid $PROXY_UID excluded)"
"$ipt" -w -t nat -S "$chain"
