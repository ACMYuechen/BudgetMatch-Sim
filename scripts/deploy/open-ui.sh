#!/usr/bin/env bash
set -Eeuo pipefail

server="${1:-ubuntu@51.79.164.39}"
echo 'Open https://localhost:8086 after forwarding starts; the initial certificate is self-signed.'
echo 'Keep this terminal open. Press Ctrl-C to close the tunnel.'
exec ssh -tt -o ExitOnForwardFailure=yes \
  -L 127.0.0.1:8086:127.0.0.1:18086 "$server" \
  'sudo k3s kubectl -n argocd port-forward --address 127.0.0.1 svc/argocd-server 18086:443'
