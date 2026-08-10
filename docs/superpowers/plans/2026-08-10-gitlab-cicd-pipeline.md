# GitLab CI/CD Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace GitLab AutoDevOps (currently mostly broken/no-op on this project) with a hand-written `.gitlab-ci.yml` tailored to this repo's actual Go + Vite/React/Dockerfile toolchain.

**Architecture:** One `.gitlab-ci.yml` at the repo root, four stages (`test`, `lint`, `security`, `build`). Each task below adds one stage's jobs, verified against a real pipeline run on a working branch before moving to the next. The final task disables AutoDevOps on the project and merges to `main`.

**Tech Stack:** GitLab CI (self-managed CE, `nssti-dev.mcmc.gov.my`, project ID 14, `nsrd/dns-compliance`), `golang:1.26-bookworm`, `node:22-alpine`, Kaniko (image build/push without a Docker daemon), `golangci-lint`, `gosec`, `gitleaks`, `trivy`. Verification uses `glab ci lint` (static YAML validation) and `glab ci status` / `glab api .../pipelines/:id/jobs` (real pipeline runs) via the `glab` CLI, already authenticated against `nssti-dev.mcmc.gov.my`.

## Global Constraints

- Go version floor: `1.26` (from `go.mod`: `module github.com/afif/dns-tracking`, `go 1.26`).
- Node version floor: 20.19+ or 22.12+ (Vite `^8.0.12` in `web/package.json` requires it) — use `node:22-alpine`.
- GitLab instance is self-managed **Community Edition** (`"enterprise": false`, v18.7.1) — no Ultimate license. Do not target GitLab's proprietary SAST/secret-detection/container-scanning report JSON schema; that only renders in the Ultimate-gated Security Dashboard/MR widget and buys nothing here. Plain job artifacts (raw tool output) are sufficient.
- `security` stage jobs (`gosec`, `gitleaks`) and the `trivy` job start with `allow_failure: true` — no existing baseline to compare against yet.
- `build` (Kaniko image push) and `trivy` (scans that pushed image) are restricted to the `main` branch only (`rules: - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'`).
- No deploy stage — out of scope per the design spec (`docs/superpowers/specs/2026-08-10-gitlab-cicd-design.md`).
- Pushing to the `gitlab` remote and disabling AutoDevOps are shared-state/project-setting changes — confirm with the user before Task 5's push-to-main and `auto_devops_enabled` change.

---

## Task 1: Pipeline skeleton + `test` stage

**Files:**
- Create: `.gitlab-ci.yml`

**Interfaces:**
- Produces: stage list `test, lint, security, build`; jobs `go-test`, `web-build`. Later tasks append jobs to the `lint`, `security`, `build` stages of this same file — do not redefine `stages:`.

- [ ] **Step 1: Create a working branch**

```bash
git checkout -b ci/replace-autodevops
```

- [ ] **Step 2: Write `.gitlab-ci.yml` with the stage list and the `test` stage**

```yaml
stages:
  - test
  - lint
  - security
  - build

go-test:
  stage: test
  image: golang:1.26-bookworm
  variables:
    GOPATH: $CI_PROJECT_DIR/.go
  cache:
    key:
      files:
        - go.sum
    paths:
      - .go/pkg/mod/
  script:
    - go test ./...

web-build:
  stage: test
  image: node:22-alpine
  cache:
    key:
      files:
        - web/package-lock.json
    paths:
      - web/node_modules/
  script:
    - cd web
    - npm ci
    - npm run build
```

- [ ] **Step 3: Statically validate the YAML**

Run: `glab ci lint`
Expected: `✓ CI/CD Yaml is valid!` (or equivalent success message, no syntax/logic errors). If it reports an error, fix the YAML and re-run before continuing.

- [ ] **Step 4: Commit and push to trigger a real pipeline**

```bash
git add .gitlab-ci.yml
git commit -m "ci: add test stage (go-test, web-build), replacing AutoDevOps buildpack detection"
git push gitlab ci/replace-autodevops
```

- [ ] **Step 5: Watch the pipeline and confirm both jobs pass**

Run: `glab ci status --branch ci/replace-autodevops --live`
Expected: pipeline runs `go-test` and `web-build` (no AutoDevOps `build`/`code_quality`/etc. jobs — a branch pipeline with a `.gitlab-ci.yml` present uses only this file), both finish `success`. If `go-test` fails, read the failure — `go test ./...` must actually pass in this environment; do not weaken the test invocation to make it pass. If `web-build` fails on the Node version, confirm `node:22-alpine` actually satisfies Vite's engine check (`npm run build` output will say `EBADENGINE` if not) and bump the tag if needed.

---

## Task 2: `lint` stage

**Files:**
- Modify: `.gitlab-ci.yml`

**Interfaces:**
- Consumes: stage `lint` (defined in Task 1's `stages:` list).
- Produces: jobs `go-vet`, `golangci-lint`, `web-lint`.

- [ ] **Step 1: Append the lint jobs**

```yaml
go-vet:
  stage: lint
  image: golang:1.26-bookworm
  variables:
    GOPATH: $CI_PROJECT_DIR/.go
  cache:
    key:
      files:
        - go.sum
    paths:
      - .go/pkg/mod/
  script:
    - go vet ./...

golangci-lint:
  stage: lint
  image: golangci/golangci-lint:latest-alpine
  allow_failure: true
  script:
    - golangci-lint run ./...

web-lint:
  stage: lint
  image: node:22-alpine
  cache:
    key:
      files:
        - web/package-lock.json
    paths:
      - web/node_modules/
  script:
    - cd web
    - npm ci
    - npm run lint
```

- [ ] **Step 2: Validate and push**

```bash
glab ci lint
git add .gitlab-ci.yml
git commit -m "ci: add lint stage (go vet, golangci-lint, eslint)"
git push gitlab ci/replace-autodevops
```

- [ ] **Step 3: Confirm the pipeline**

Run: `glab ci status --branch ci/replace-autodevops --live`
Expected: `go-vet` and `web-lint` finish `success` (both blocking — if either fails, fix the underlying vet/lint issue, don't suppress it). `golangci-lint` may finish `failed` and that's fine as long as the pipeline's overall status still shows `passed` (yellow warning icon) rather than `failed`, since it's `allow_failure: true` — verify this distinction with `glab api "projects/14/pipelines?ref=ci/replace-autodevops" | python3 -m json.tool | head -20` and confirm `"status": "success"` on the pipeline even if the `golangci-lint` job itself is red.

---

## Task 3: `security` stage (gosec, gitleaks)

**Files:**
- Modify: `.gitlab-ci.yml`

**Interfaces:**
- Consumes: stage `security`.
- Produces: jobs `gosec`, `gitleaks`.

- [ ] **Step 1: Append the security jobs**

```yaml
gosec:
  stage: security
  image: golang:1.26-bookworm
  allow_failure: true
  script:
    - go install github.com/securego/gosec/v2/cmd/gosec@latest
    - gosec ./...

gitleaks:
  stage: security
  image:
    name: zricethezav/gitleaks:latest
    entrypoint: [""]
  allow_failure: true
  script:
    - gitleaks detect --source . --verbose
```

- [ ] **Step 2: Validate and push**

```bash
glab ci lint
git add .gitlab-ci.yml
git commit -m "ci: add security stage (gosec, gitleaks), non-blocking until triaged"
git push gitlab ci/replace-autodevops
```

- [ ] **Step 3: Confirm the pipeline**

Run: `glab ci status --branch ci/replace-autodevops --live`
Expected: `gosec` and `gitleaks` both run (not `skipped` — unlike AutoDevOps's license-gated equivalents) and the overall pipeline is `success` regardless of whether they find anything, since both are `allow_failure: true`. If either job errors out before producing a real scan (e.g. `go install` network failure, image pull failure), that's an infrastructure problem to fix — re-run and check `glab ci trace <job-id>` for the actual error, don't just leave it broken because it's non-blocking.

---

## Task 4: `build` stage (Kaniko build+push, Trivy image scan)

**Files:**
- Modify: `.gitlab-ci.yml`

**Interfaces:**
- Consumes: stage `build`; `$CI_REGISTRY_IMAGE`, `$CI_REGISTRY`, `$CI_REGISTRY_USER`, `$CI_REGISTRY_PASSWORD`, `$CI_COMMIT_SHA`, `$CI_DEFAULT_BRANCH` (all predefined GitLab CI variables — no manual configuration needed since the project already has the Container Registry enabled).
- Produces: jobs `build`, `trivy`. Pushes `$CI_REGISTRY_IMAGE:$CI_COMMIT_SHA` and `$CI_REGISTRY_IMAGE:latest` to the project's Container Registry.

- [ ] **Step 1: Append the build jobs, restricted to `main`**

```yaml
build:
  stage: build
  image:
    name: gcr.io/kaniko-project/executor:v1.23.2-debug
    entrypoint: [""]
  rules:
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'
  script:
    - mkdir -p /kaniko/.docker
    - echo "{\"auths\":{\"$CI_REGISTRY\":{\"auth\":\"$(printf '%s:%s' "$CI_REGISTRY_USER" "$CI_REGISTRY_PASSWORD" | base64 | tr -d '\n')\"}}}" > /kaniko/.docker/config.json
    - /kaniko/executor --context "$CI_PROJECT_DIR" --dockerfile "$CI_PROJECT_DIR/Dockerfile" --destination "$CI_REGISTRY_IMAGE:$CI_COMMIT_SHA" --destination "$CI_REGISTRY_IMAGE:latest"

trivy:
  stage: build
  image:
    name: aquasec/trivy:latest
    entrypoint: [""]
  needs: ["build"]
  allow_failure: true
  rules:
    - if: '$CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'
  script:
    - trivy image --severity HIGH,CRITICAL --exit-code 1 --username "$CI_REGISTRY_USER" --password "$CI_REGISTRY_PASSWORD" "$CI_REGISTRY_IMAGE:$CI_COMMIT_SHA"
```

Note: `trivy` is placed in the `build` stage (not `security`) with `needs: ["build"]`, even though it's conceptually a security scan — GitLab CI forbids a job from `needs`-ing a job in a *later* stage, and `build` (the Kaniko push `trivy` depends on) runs after `security` in the stage order. Keeping both in the `build` stage and using `needs` to sequence `trivy` after the image push avoids that restriction while still running them in parallel with nothing else.

- [ ] **Step 2: Validate the YAML**

Run: `glab ci lint`
Expected: valid. Since both new jobs are `main`-only, this only checks syntax — it won't catch registry-auth or Kaniko runtime issues.

- [ ] **Step 3: Temporarily verify on the working branch before merging to main**

Since `build`/`trivy` only run on `main`, add a second temporary condition so they also fire on this branch, to catch real Kaniko/registry/Trivy problems before they hit `main`:

```bash
sed -i "s#if: '\$CI_COMMIT_BRANCH == \$CI_DEFAULT_BRANCH'#if: '\$CI_COMMIT_BRANCH == \$CI_DEFAULT_BRANCH || \$CI_COMMIT_BRANCH == \"ci/replace-autodevops\"'#g" .gitlab-ci.yml
git add .gitlab-ci.yml
git commit -m "ci: temporarily test build+trivy jobs on this branch"
git push gitlab ci/replace-autodevops
```

- [ ] **Step 4: Confirm the pipeline**

Run: `glab ci status --branch ci/replace-autodevops --live`
Expected: `build` pushes the image successfully (`success`) and `trivy` runs after it (status `success` or `failed`-but-allowed depending on findings — either is fine, since it's `allow_failure: true`). If `build` fails, read `glab ci trace <job-id>` — common failure points are registry auth (confirm `$CI_REGISTRY_USER`/`$CI_REGISTRY_PASSWORD` are populated: they're auto-provided by GitLab whenever the Container Registry is enabled on the project, which it is here) or a Kaniko context/Dockerfile path issue.

Confirm the image landed in the registry:

```bash
glab api "projects/14/registry/repositories" | python3 -m json.tool
```

Expected: at least one repository entry, and its tag list (via `projects/14/registry/repositories/:id/tags`) includes a tag matching this branch's `$CI_COMMIT_SHA`.

- [ ] **Step 5: Revert the temporary branch condition**

Once confirmed working, remove the temporary OR-clause so `build`/`trivy` go back to `main`-only before this branch merges:

```bash
sed -i "s#if: '\$CI_COMMIT_BRANCH == \$CI_DEFAULT_BRANCH || \$CI_COMMIT_BRANCH == \"ci/replace-autodevops\"'#if: '\$CI_COMMIT_BRANCH == \$CI_DEFAULT_BRANCH'#g" .gitlab-ci.yml
glab ci lint
git add .gitlab-ci.yml
git commit -m "ci: restrict build+trivy back to main only"
git push gitlab ci/replace-autodevops
```

---

## Task 5: Disable AutoDevOps and merge to `main`

**Files:** none (project setting + git merge)

**Interfaces:**
- Consumes: the completed `.gitlab-ci.yml` from Tasks 1–4.

This task changes shared project state (the `main` branch and the project's CI/CD settings) — confirm with the user before running Steps 2–3.

- [ ] **Step 1: Confirm current AutoDevOps setting**

```bash
glab api "projects/14" | python3 -c "import json,sys; print(json.load(sys.stdin)['auto_devops_enabled'])"
```

Expected: `True`.

- [ ] **Step 2: Disable AutoDevOps**

```bash
glab api -X PUT "projects/14" -f auto_devops_enabled=false
```

- [ ] **Step 3: Verify it's off**

```bash
glab api "projects/14" | python3 -c "import json,sys; print(json.load(sys.stdin)['auto_devops_enabled'])"
```

Expected: `False`.

- [ ] **Step 4: Merge the working branch to `main`**

```bash
git checkout main
git pull gitlab main
git merge --no-ff ci/replace-autodevops -m "Merge branch 'ci/replace-autodevops': replace AutoDevOps with project-aware pipeline"
git push gitlab main
```

- [ ] **Step 5: Confirm the `main` pipeline runs the full pipeline including `build`/`trivy`**

```bash
glab ci status --branch main --live
```

Expected: all of `go-test`, `web-build`, `go-vet`, `web-lint`, `golangci-lint`, `gosec`, `gitleaks`, `build`, `trivy` run (not skipped — this is the first `main` push under the new config), with `go-test`/`web-build`/`go-vet`/`web-lint`/`build` required to pass and the rest allowed to fail. No `code_quality`, `code_intelligence_go`, `secret_detection`, `semgrep-sast`, or `container_scanning` AutoDevOps jobs should appear at all.

- [ ] **Step 6: Delete the working branch**

```bash
git push gitlab --delete ci/replace-autodevops
git branch -d ci/replace-autodevops
```
