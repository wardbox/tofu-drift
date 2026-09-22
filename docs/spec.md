# tofu-drift — MVP Build Spec (CLI v0.1)

## 1. MVP Definition

**v0.1 is:** a single open-source CLI, `tofu-drift`, that (a) reports drift on OpenTofu/Terraform-managed AWS resources and (b) finds unmanaged/orphaned AWS resources with a $/mo and kgCO₂/mo estimate on each. AWS only. Read-only. No hosted backend, no billing, no accounts.

**The demo that must work end-to-end:** `tofu-drift scan` in a repo with an S3-backed state → table of drifted resources + orphans sorted by monthly cost, with a footer line: `Estimated waste: $214/mo · ~180 kgCO₂/yr`.

**Success = launchable:** works on real-world state files, runs in <60s on a typical small account, zero write permissions required.

## 2. CLI Commands & UX (v0.1)

- `tofu-drift scan [--state <path|s3://...>] [--profile] [--region]` — full report: drifted managed resources + unmanaged resources. Auto-detects state from the current repo's backend config when flags omitted
- `tofu-drift orphans [--sort cost|type|age]` — unmanaged-only view with cost/carbon columns
- `tofu-drift import-gen [--ids <...>]` — prints import blocks for selected orphans to stdout
- `--json` on everything; exit code 0 clean / 1 drift found / 2 error (CI-gateable)
- Config file `tofu-drift.toml`: ignore rules by tag (`ManagedBy=other`), type, or ARN glob

**UX rules:** no color soup — one table, cost right-aligned, worst offenders first; every run ends with the waste-footer line; `--explain <id>` prints the attribute-level diff for one resource. First-run with no credentials prints exactly what read-only IAM policy to attach (copy-pasteable JSON).

**Cut from v0.1:** destroy-plan (liability, needs more care), multi-state scanning, non-S3 remote backends beyond local + S3 + HTTP.

## 3. Technical Design

**Managed drift (don't reimplement providers):** shell out to `tofu plan -refresh-only -json` (fall back to `terraform`) and parse the JSON stream — `resource_drift` entries carry before/after per resource. The provider does all diffing; we render. If neither binary exists, degrade gracefully to unmanaged-scan-only with a notice.

**State reading:** parse state JSON schema v4 only (current for both tools). Sources: local file, S3 (via SDK, honoring the repo's backend "s3" block), plain HTTP. Extract: resource type, name, provider, ARN/ID, tags. Never write state, never lock.

**Unmanaged scan:** per covered service, SDK v3 list/describe → build live-resource inventory → subtract everything whose ID/ARN appears in state → remainder is unmanaged. Concurrency-limited (p-limit ~8), region from flag/env, single account v0.1.

**Matching subtleties (the actual hard part):** normalize IDs vs ARNs per type; resources created by managed resources (ASG→instances, EKS→ENIs/SGs) must be attributed to their parent, not flagged — maintain a per-type "derived resource" suppression table; default-VPC furniture (default SG/route tables) suppressed unless `--include-defaults`.

**Trust posture:** read-only — publish the exact IAM policy in the repo; no telemetry in v0.1; state contents never leave the machine.

## 4. Covered AWS Resource Types — Launch List (25)

Chosen for waste-likelihood × small-team ubiquity. Published as a public coverage matrix in the README.

- **Compute/network waste (the money):** EC2 instances, EBS volumes (esp. unattached), EBS snapshots, Elastic IPs (unassociated), NAT gateways, ALB/NLB/CLB, ENIs (unattached)
- **Data:** RDS instances, RDS snapshots, DynamoDB tables, ElastiCache clusters, S3 buckets
- **Compute misc:** Lambda functions, ECS clusters/services, EKS clusters, Auto Scaling groups, AMIs (owned)
- **Plumbing:** security groups, VPCs (non-default), subnets, route tables, IAM roles, IAM users, CloudWatch log groups (retention=never is a classic leak), Route53 hosted zones

**Rule for additions post-launch:** only from user requests, only with a cost story, cap total at ~50.

## 5. Cost & Carbon Estimation

**Cost:** static pricing table shipped in the binary (per type × instance class × region for the big movers; flat estimates for the rest), refreshed at release time from the AWS Pricing API — not queried live (slow, needs extra perms). Label everything "estimated"; precision matters less than ranking. NAT gateway, unattached EBS, idle ALB, unassociated EIP cover most real-world waste and have simple flat rates.

**Carbon:** Cloud Carbon Footprint's published methodology — per-service energy coefficients × regional grid intensity (their open dataset) → kgCO₂e/mo per resource. Ship the coefficients as data, cite CCF in the README. One decimal, clearly labeled estimate.

**Footer math:** sum of unmanaged + drifted-oversized resources → `$X/mo · ~Y kgCO₂/yr — roughly Z trans-Atlantic flights` (one relatable equivalence, no more).

## 6. Repo & Stack

- **Go 1.23+** — ecosystem-native (OpenTofu, Terraform, driftctl, goat are all Go; contributors and credibility follow); tiny static binaries via goreleaser (linux/darwin, amd64/arm64), Homebrew tap + `go install`
- **aws-sdk-go-v2**, per-service clients; stdlib `encoding/json` + typed structs for state/plan parsing; **cobra** for the CLI — no other frameworks
- **Layout:** `internal/state/` (parsers), `internal/plan/` (refresh-only runner+parser), `internal/scan/<service>.go` (one file per service implementing `Scanner` with `List(ctx) ([]LiveResource, error)` — the contributor surface), `internal/match/`, `internal/pricing/` + `internal/carbon/` (data embedded via `go:embed`), `internal/report/`, `cmd/tofu-drift/`
- **Tests:** golden-file tests on recorded state/plan JSON + SDK responses (no live AWS in CI); `sandbox/` — an OpenTofu stack in a throwaway AWS account that provisions ~15 resource types, plus a make-mess script that induces drift (out-of-band tag/setting edits) and plants orphans (unattached EBS, unassociated EIP, short-lived NAT). The sandbox is the source of fixtures and the demo screenshot; tear-down script included, expected cost a few dollars per session
- **License Apache-2.0** (matches OpenTofu itself — no CLA, plain DCO sign-offs like the OpenTofu project uses; community-respect over relicensing optionality); CI = lint, test, release binaries on tag
