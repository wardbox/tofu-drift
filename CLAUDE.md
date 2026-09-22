# tofu-drift

Open-source Go CLI: reports drift on OpenTofu/Terraform-managed AWS resources and finds unmanaged/orphaned AWS resources with a $/mo and kgCO₂/mo estimate. AWS only, read-only, no backend.

The MVP build spec (scope, commands, technical design, covered resource types) lives in `docs/spec.md`. Read it before planning or scoping work.

## Agent skills

### Issue tracker

Issues live in GitHub Issues on `wardbox/tofu-drift`, driven via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default vocabulary: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` and `docs/adr/` at the repo root (created lazily). See `docs/agents/domain.md`.
