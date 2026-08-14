#!/usr/bin/env bash
# Pulls images from the GitLab registry (run this from a host that can reach
# it, e.g. after `glab auth login` / `docker login`) and loads them onto a
# staging host that can't reach the registry itself.
#
# Usage: ./scripts/sync-staging-images.sh <crawler|dashboard> <ssh-host> [tag]
#   ./scripts/sync-staging-images.sh crawler appsadmin@192.168.78.135
#   ./scripts/sync-staging-images.sh dashboard appsadmin@192.168.78.46 main
set -euo pipefail

ROLE="${1:?usage: $0 <crawler|dashboard> <ssh-host> [tag]}"
STAGING_HOST="${2:?usage: $0 <crawler|dashboard> <ssh-host> [tag]}"
TAG="${3:-main}"
REGISTRY_IMAGE="${REGISTRY_IMAGE:-nssti-dev.mcmc.gov.my/nsrd/dns-compliance}"

case "$ROLE" in
  crawler)   SERVICES="crawler" ;;
  dashboard) SERVICES="server web" ;;
  *) echo "role must be 'crawler' or 'dashboard', got: $ROLE" >&2; exit 1 ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

for svc in $SERVICES; do
  echo "==> pulling $REGISTRY_IMAGE/$svc:$TAG"
  docker pull "$REGISTRY_IMAGE/$svc:$TAG"
  docker save "$REGISTRY_IMAGE/$svc:$TAG" | gzip > "$tmp/$svc.tar.gz"
done

echo "==> copying to $STAGING_HOST"
scp "$tmp"/*.tar.gz "$STAGING_HOST:/tmp/"

echo "==> loading on $STAGING_HOST"
# shellcheck disable=SC2029
ssh "$STAGING_HOST" "for f in $(for s in $SERVICES; do echo -n "/tmp/$s.tar.gz "; done); do docker load -i \"\$f\" && rm \"\$f\"; done"

echo "==> done. On $STAGING_HOST, run (from docker-compose.$ROLE.yml):"
echo "    REGISTRY_IMAGE=$REGISTRY_IMAGE IMAGE_TAG=$TAG docker compose -f docker-compose.$ROLE.yml up -d"
