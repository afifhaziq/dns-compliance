# Staging Auto-Deploy via Ansible — Design (Phase 1: pull-based)

## Problem

CI (`.gitlab-ci.yml`) already builds and pushes `server`/`crawler`/`web` images on every merge to `main`, but getting the new image running on staging is a manual step: someone runs `ansible-playbook playbooks/deploy-{dashboard,crawler}.yml` (or the older `sync-staging-images.sh`) by hand from a machine that can reach both the GitLab registry and the staging hosts. The goal is auto-deploy to staging on merge, as the first piece of a CI/CD pattern intended to eventually generalize to other repos on the team.

## Constraints discovered during design

These came from live checks against the actual GitLab instance (`nssti-dev.mcmc.gov.my`), not assumptions:

- This project has exactly one runner available (`glab api /projects/14/runners`): a shared, `instance_type` runner (id 35, `docker` executor), tagged `docker, shared, build, deploy`. It runs on the GitLab host itself — the team wants it to stay there (not installed on staging hosts, not on a separate control machine).
- **That runner cannot reach either staging host over SSH.** Verified by pushing a throwaway CI job (`verify-staging-reach`, since removed) that SSHed from the runner to `192.168.88.46` and `192.168.88.35`: both timed out (`Operation timed out`, not a connection-refused/auth error — genuine network-level unreachability), confirming this is a firewall gap, not a missing key.
- The firewall between the GitLab host and the staging subnet currently only allows ports 80/443, not 22. Opening 22 is possible later but not committed to yet.
- Staging hosts can already reach the GitLab container registry directly over 443 (`docker pull` works today) — this used to not be true, which is why the existing `ansible/playbooks/tasks/sync-image.yml` pulls the image on the control node and relays it to staging as a gzipped tarball over SSH/copy. That workaround is now unnecessary.

Net effect: Ansible's default transport is SSH (`copy`/`template`/`command` modules all require it) — with port 22 closed, the runner cannot drive Ansible against staging at all right now. Given staging can already reach the registry, the design goal is achievable without SSH by flipping to a pull model.

## Approach: two phases, not one

**Phase 1 (this spec, buildable now):** each staging host pulls its own updates on a timer. No CI→staging network path needed at all, so it isn't blocked on the firewall.

**Phase 2 (future, not built now):** once port 22 is opened, add a `deploy` stage to `.gitlab-ci.yml` that runs the existing Ansible playbooks from the runner (already tagged `deploy`) after the build stage, on merge to `main`. The playbooks already do the right thing today for a human running them by hand; Phase 2 is "point CI at them," not a rewrite. Migration note: once Phase 2 lands, the Phase 1 timer should be disabled on each host to avoid two mechanisms racing to redeploy the same containers.

Phase 2 is included here for context/continuity, not as work to schedule now — only Phase 1 gets an implementation plan.

## Phase 1 architecture

Each staging host runs a **user-level systemd timer** (`systemctl --user`, not root) polling on an interval:

```
docker compose pull && docker compose up -d --remove-orphans
```

`docker compose pull` no-ops if the image tag's digest hasn't changed; `up -d` only recreates containers whose image actually changed — same idempotency property the existing Ansible playbooks already rely on for safe re-runs. Poll interval: every 3 minutes (`OnUnitActiveSec=3min`), balancing staging-freshness against unnecessary registry hits; not user-configurable, no need for that yet.

CI's side of `.gitlab-ci.yml` does not change in Phase 1 — the existing `build:server`/`build:crawler`/`build:web` jobs already push `:main` on every merge, which is all this depends on.

### One-time host bootstrap (manual, root required once)

`systemctl --user` units don't run without an active login session unless lingering is enabled:

```bash
sudo loginctl enable-linger appsadmin
```

Run once per staging host, by a human with existing SSH access (the same access used for today's manual deploys) — not by CI, not by Ansible over a network path that doesn't exist yet. This is a one-time host bootstrap step, documented in `DEPLOYMENT.md` alongside the Ansible instructions.

### Installing the timer: reuses existing Ansible access

The actual unit files are installed via the *existing* Ansible playbooks, run by hand today exactly as `deploy-dashboard.yml`/`deploy-crawler.yml` already are (same SSH access path, nothing new required of it):

- `ansible/playbooks/templates/docker-compose-pull.service.j2` and `docker-compose-pull.timer.j2` — new unit file templates. Content is the same for both hosts (only `remote_dir` varies, which is already `dns-compliance` for both today), so these can be near-static templates, not per-host logic.
- `ansible/playbooks/tasks/install-pull-timer.yml` — new shared task: copies both unit files to `~/.config/systemd/user/`, then `systemctl --user daemon-reload` + `enable --now docker-compose-pull.timer`.
- `deploy-dashboard.yml` and `deploy-crawler.yml` each gain one new task: `include_tasks: tasks/install-pull-timer.yml`, alongside their existing steps.

Because this only writes files and enables a systemd unit, it's as idempotent/safe-to-rerun as everything else in those playbooks.

## Cleanup: drop the tarball workaround

`ansible/playbooks/tasks/sync-image.yml` exists solely because staging couldn't reach the registry before. Since it can now, replace its pull→save→scp→load→cleanup dance with a single task run directly on the target host:

```yaml
- name: Pull {{ image_ref }} directly from the registry
  ansible.builtin.command: docker pull {{ image_ref }}
  changed_when: true
```

This simplifies the *manual* Ansible redeploy path too (`deploy-dashboard.yml`/`deploy-crawler.yml`'s "Sync images" step), not just the new Phase 1 timer — one less moving part, no `delegate_to: localhost`, no control-node tarball juggling, no `/tmp` cleanup steps. Prerequisite: staging hosts need registry pull credentials (`docker login` or equivalent) configured — already true today per the confirmed working `docker pull`.

## Generic reuse for other repos (not built now)

Only this repo needs this today, so no shared component/template repo is being created in this pass — per YAGNI, there's nothing to validate a shared abstraction against with one consumer. What's already repo-agnostic and would be a clean extraction later, once a second repo needs it:

- `docker-compose-pull.service.j2`/`.timer.j2` and `install-pull-timer.yml` — parameterized only by `remote_dir`, already a var.
- The CI `deploy` stage pattern from Phase 2, once it exists.

What stays repo-specific: inventory (hosts/IPs), `group_vars` (image names, secrets), and each playbook's own compose/template file paths — same shape as today.

## Testing / rollout

- Deploy the timer to one staging host first (crawler, lower blast radius than dashboard's traefik+postgres+minio stack), confirm `systemctl --user status docker-compose-pull.timer` and a manual `systemctl --user start docker-compose-pull.service` picks up a real image update, before rolling to the dashboard host.
- Verify `loginctl enable-linger appsadmin` took effect by checking the timer keeps running after closing the SSH session (`systemctl --user list-timers` after logout/login, or `loginctl show-user appsadmin | grep Linger`).
- No automated test suite applies here — this is infra/ops tooling (Ansible YAML + systemd units), verified by running it against the real hosts, not unit-testable.

## Open items

- None blocking Phase 1. Phase 2 (push-based via port 22) is explicitly deferred, not an open question to resolve now — revisit once the firewall exception is actually requested/granted.
