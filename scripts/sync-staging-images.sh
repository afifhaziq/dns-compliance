#!/usr/bin/env bash
# deployment/dashboard/docker-compose.yml and deployment/crawler/docker-compose.yml
# (see DEPLOYMENT.md) assume a direct `docker compose pull` on each host. Our
# staging hosts can't reach the GitLab registry at all, so this pulls the
# same images from a host that can (e.g. after `glab auth login` / `docker
# login`), saves, scp's, and loads them on the target host instead — the
# resulting local image:tag matches what each compose file's
# SERVER_IMAGE/WEB_IMAGE/CRAWLER_IMAGE already defaults to, so no compose
# file changes are needed, just skip `docker compose pull` on staging.
#
# Usage: ./scripts/sync-staging-images.sh <crawler|dashboard> <ssh-host> [tag]
#   ./scripts/sync-staging-images.sh crawler appsadmin@192.168.88.35
#   ./scripts/sync-staging-images.sh dashboard appsadmin@192.168.88.46 main
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

echo "==> done. Images are loaded locally on $STAGING_HOST."
echo "    Copy deployment/$ROLE/ (docker-compose.yml, filled-in .env, certs/) there if you haven't,"
echo "    then: docker compose -f docker-compose.yml up -d   (skip 'docker compose pull' — images are already loaded)"
