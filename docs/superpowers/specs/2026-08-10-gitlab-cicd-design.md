# GitLab CI/CD: replace AutoDevOps with a project-aware pipeline

Date: 2026-08-10

## Problem

The repo runs on GitLab AutoDevOps with no custom `.gitlab-ci.yml`. Auditing recent pipelines (`glab ci list` / `glab api .../jobs`, `.../trace`) on `nsrd/dns-compliance` (self-managed GitLab CE, `nssti-dev.mcmc.gov.my`, v18.7.1) showed most of it is dead weight:

- **`build`** and **`code_quality`** both fail on every run: `Error response from daemon: client version 1.4x is too old. Minimum supported API version is 1.44`. The runner's Docker daemon requires a newer API than the AutoDevOps job images (`auto-build-image:v4.13.0`, `docker:20.10.12`) bundle — an infra-side dind/client version mismatch, not app code.
- **`code_intelligence_go`** fails separately: `lsif-go` infers the Go module name from the git remote URL and only understands github.com/gitlab.com-style paths, not `ssh://git@nssti-dev.mcmc.gov.my:3389/...`.
- **`secret_detection`**, **`semgrep-sast`**, **`container_scanning`** show `skipped` on every pipeline — confirmed via `glab api .../version` that `"enterprise": false` (GitLab CE). These AutoDevOps security templates are license-gated and have never actually scanned anything on this instance.
- Only the generic **`test`** job passes, via buildpack heuristics with no awareness of the `web/` frontend and no visibility into what it's actually running.

Meanwhile the repo already knows how to build and test itself: a hand-written multi-stage `Dockerfile`, and `CLAUDE.md` documents exact `go build`/`go test`/frontend commands. AutoDevOps's generic detection is actively fighting a project that doesn't need it guessed.

## Decision

Turn off AutoDevOps for this project; add a `.gitlab-ci.yml` tailored to what's actually here: two Go binaries (`cmd/server`, `cmd/crawler`), a Vite/React frontend in `web/`, and an existing multi-stage `Dockerfile`.

## Stages

```
test → lint → security → build
```

Runs on every push (branch pipelines). The image build+push job is restricted to `main` only — no reason to push an image to the registry for every feature-branch commit on a solo/small-team repo without routine MR gating.

### `test` stage

- **`go-test`**: `golang:1.26-bookworm`, `go test ./...`. Runner has real internet egress, so `internal/dns`'s live 8.8.8.8 lookups run as-is — no skip/exclude needed.
  - Cache: `$GOPATH/pkg/mod` + `$GOPATH/pkg/build` (Go module + build cache), keyed on `go.sum`.
- **`web-build`**: `node:22-alpine` (Vite 8 requires Node 20.19+/22.12+, no `engines` field or `.nvmrc` in the repo to pin against otherwise), `npm ci && npm run build` in `web/`. `tsc -b` runs as part of `build` already (per `web/CLAUDE.md`), so this doubles as a type-check gate — no separate step needed.
  - Cache: `web/node_modules`, keyed on `web/package-lock.json`.

### `lint` stage

- **`go-vet`**: `go vet ./...` — cheap, stdlib, catches real mistakes; always blocking.
- **`golangci-lint`**: official `golangci/golangci-lint` image, default linter set (no existing `.golangci.yml`, so start with upstream defaults rather than hand-tuning blind). `allow_failure: true` initially — unknown baseline on existing code, don't want day one to turn the pipeline red for pre-existing findings unrelated to whatever change triggered the run. Revisit once the baseline's been triaged.
- **`web-lint`**: `npm run lint` (ESLint) in `web/`. Blocking — this is already run manually today per `web/CLAUDE.md`, so the baseline is presumably already clean.

### `security` stage

Replaces what AutoDevOps's skipped SAST/secret_detection/container_scanning were supposed to do, using OSS tools that don't need an Ultimate license:

- **`gosec`**: `securego/gosec` image, scans Go source. `allow_failure: true` initially (no baseline yet).
- **`gitleaks`**: `zricethezav/gitleaks` image, scans git history for committed secrets. `allow_failure: true` initially.
- **`trivy`**: runs after `build` (needs the pushed image), scans `$CI_REGISTRY_IMAGE:$CI_COMMIT_SHA` for known CVEs. `allow_failure: true` initially. Only runs where `build` ran (`main`).

All three write their reports as job artifacts (plain JSON/text) rather than GitLab's proprietary SAST/secret-detection report schema — the Security Dashboard / MR widget that consumes that schema is itself an Ultimate feature, so targeting it buys nothing on CE.

### `build` stage

- **`build`**: Kaniko (`gcr.io/kaniko-project/executor:debug`), not `docker build` + dind — sidesteps the runner's Docker API version mismatch entirely, since Kaniko builds images without talking to a Docker daemon at all. Builds the existing `Dockerfile`, pushes `$CI_REGISTRY_IMAGE:$CI_COMMIT_SHA` and `$CI_REGISTRY_IMAGE:latest` to the GitLab Container Registry (already enabled on the project). Restricted to `main`.

## Out of scope

- **Deploy stage**: none. AutoDevOps's deploy strategy was already `manual` with no evidence of a wired-up target; deployment (pulling the new image via `docker-compose.yml`, etc.) stays a manual/separate concern.
- **Fixing the runner's Docker daemon version**: possible alternative to adopting Kaniko, but requires GitLab instance-admin access this session doesn't have. Kaniko sidesteps the problem instead of depending on an infra fix outside this repo's control.
- **Tuning golangci-lint / gosec rule sets**: start with defaults; revisit once real findings have been triaged.

## Rollout

1. Add `.gitlab-ci.yml`.
2. Disable AutoDevOps on the project (`auto_devops_enabled: false` via project settings or `glab api -X PUT`).
3. Push and confirm the new pipeline runs green (test/lint blocking jobs) with security jobs visible-but-non-blocking.
