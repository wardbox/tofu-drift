---
status: accepted
---

# Delegate state access and drift detection to the tofu binary

tofu-drift never reimplements provider reads or backend clients. Managed drift comes from shelling out to `tofu plan -refresh-only -json` (falling back to `terraform`) and parsing the `resource_drift` stream; state in a root module comes from `tofu state pull`. The only state readers we own are a local-file reader and an `s3://` reader, used solely when `--state` is passed explicitly.

## Considered Options

- **Reimplement provider reads per resource type** (the driftctl approach). Rejected: every covered type becomes a second, permanently lagging implementation of the AWS provider's read logic, and every backend becomes a client we maintain. The provider already does the diffing correctly; we only need to render it.
- **Parse `terraform { backend "..." }` blocks ourselves** to reach remote state. Rejected: requires an HCL dependency and one client per backend, when `tofu state pull` already resolves any backend and respects the current workspace.

## Consequences

- Drift detection only works inside a root module with an initialised backend and a `tofu` or `terraform` binary on PATH. Anywhere else the tool degrades to unmanaged-scan-only and says so.
- Passing `--state` explicitly always skips drift detection, because a state file alone has no module to plan against.
- We inherit the provider's plan output format and must track changes to the plan JSON schema.
