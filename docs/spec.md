# tofu-drift — MVP Build Spec (CLI v0.1)

Vocabulary in this document follows `CONTEXT.md`. Decisions with lasting consequences live in `docs/adr/`.

## 1. MVP Definition

**v0.1 is:** a single open-source CLI, `tofu-drift`, that (a) reports Drift on OpenTofu/Terraform-managed AWS resources and (b) finds Unmanaged and Idle AWS resources with a $/mo and kgCO₂/mo Estimate on each. AWS only. Read-only. No hosted backend, no billing, no accounts.

**The demo that must work end-to-end:** `tofu-drift scan` in a repo with an S3-backed state → drift table, then unmanaged/idle table sorted by monthly cost, with a footer line: `Unmanaged: $214/mo · Idle: $61/mo · ~15 kgCO₂/mo (≈ 0.02 trans-Atlantic flights)`.

**Success = launchable:** works on real-world state files, runs in <60s on a typical small account, zero write permissions required.

## 2. CLI Commands & UX (v0.1)

- `tofu-drift scan [--state <path|s3://...>] [--profile] [--region]` — full report: Drift on Managed Resources plus Unmanaged and Idle Resources. In a root module with no `--state`, state comes from `tofu state pull`. `--state` given → unmanaged/idle only (no module to plan against).
- `tofu-drift unmanaged [--sort cost|type|age]` — unmanaged/idle-only view. Age unknown for some types (EIP, SG, VPC, subnet) → shown as `-`, sorted last.
- `tofu-drift import-gen [--ids <...>]` — prints `import {}` blocks for the given AWS IDs to stdout. Address is `aws_<type>.<Name-tag slug, else ID>` with a header comment saying to rename before apply. No resource stubs; the user runs `tofu plan -generate-config-out`.
- `--explain <finding-id>` — Drift: attribute-level before/after. Unmanaged/Idle: type, ID, ARN, tags, creation time, idle reason, cost math line (`gp3 100 GB × $0.08 = $8.00/mo`), carbon line.
- `--json` on everything. Exit codes: 0 no Findings / 1 any Finding (drift, unmanaged, or idle) / 2 error. A `--fail-on` selector is deferred until requested.
- Config file `tofu-drift.toml` in cwd (`--config` overrides): Ignore Rules by tag (`ManagedBy=other`), type, or ARN glob. Ignore Rules remove Unmanaged and Idle Findings only; Drift is never suppressed by config.

**Report layout:** no color. Two sections.

1. **Drift** — `ADDRESS  TYPE  CHANGED` (changed-attribute count plus first attribute names).
2. **Unmanaged & idle** — `TYPE  ID  NAME  STATUS  AGE  $/MO  kgCO₂/MO`, cost descending. `NAME` from the `Name` tag. `STATUS` is `unmanaged`, `idle`, or `unmanaged+idle`. Region column shows `global` for S3/IAM/Route53.

Every run ends with the footer line. First run with no credentials prints the read-only IAM policy (copy-pasteable JSON) and exits 2.

**`--json` schema** (bump `schema` on breaking change):

```json
{
  "schema": 1,
  "scan": {"account": "", "region": "", "state_source": ""},
  "findings": [{
    "id": "", "address": "", "type": "", "name": "",
    "drift": null, "unmanaged": true, "idle": "unattached",
    "usd_mo": 0, "usd_mo_approx": true, "kgco2_mo": 0, "age_days": null
  }],
  "totals": {"unmanaged_usd_mo": 0, "idle_usd_mo": 0, "kgco2_mo": 0}
}
```

A drifted Finding's `drift` is `{"changed": ["attr", ...]}`, plus `"deleted": true` when the resource was deleted out of band. Attribute values never appear in `--json`; `--explain` is the only place they are shown, with sensitive ones masked.

**Cut from v0.1:** destroy-plan (liability), multi-state scanning, multi-region, HTTP state backend (`tofu state pull` covers it in-repo), CloudWatch-metric idleness, rightsizing/"oversized" detection, `--fail-on`, `--binary` override.

## 3. Technical Design

**Managed drift (don't reimplement providers, see ADR-0001):** shell out to `tofu plan -refresh-only -lock=false -input=false -out=<tmp>` (fall back to `terraform`, PATH lookup only), then parse `tofu show -json <tmp>` — its `resource_drift` entries carry before/after per resource (the `plan -json` UI stream only names them). The provider does all diffing; we render. Drift runs only when cwd has `.tf` files, a binary exists, and no `--state` was passed; otherwise degrade to unmanaged/idle-only with a notice. Drift is never costed.

**State reading:** parse state JSON schema v4 only. Sources: `tofu state pull` (in-repo, any backend, respects workspace), local file, `s3://bucket/key` via SDK. Extract per resource instance: type, address (including module path and index key), Match Key, tags. `mode: "data"` entries skipped. Never write state, never lock.

**Unmanaged scan:** per covered service, aws-sdk-go-v2 list/describe → live inventory → subtract everything whose Match Key appears in state → remainder is Unmanaged. Concurrency-limited (`errgroup.SetLimit(8)`), one region from flag/env/profile, single account. IAM, Route53, and S3 are always scanned (global); S3 buckets are listed without per-bucket location calls. Notice printed when state references resources in regions not scanned. Warning (not error) when state ARNs' account differs from the STS caller.

**Match Key:** one `keyFor(type, attrs)` table producing the per-type canonical identifier (instance ID, bucket name, role name, Route53 zone ID stripped of `/hostedzone/`, ELB ARN). Match on key equality only; no ARN fallback. Unknown types in state are ignored.

**Derived Resources:** folded into their parent, never a row. If the parent is Unmanaged, the parent is reported once with derived cost rolled up. Suppression table: EC2 instance→EBS volumes and ENIs created with it (delete-on-termination; ones attached later stand alone); AMI→its EBS snapshots; ASG→instances; EKS→ENIs/SGs/nodegroup instances; ALB/NLB→ENIs; NAT→ENI/EIP; RDS→automated snapshots; Lambda→`/aws/lambda/*` log groups.

**Default Furniture** (suppressed unless `--include-defaults`): default VPC and its subnets/IGW/route table/NACL/DHCP options; the default SG and main route table in every VPC; service-linked IAM roles (`/aws-service-role/`, `AWSServiceRoleFor*`); the `default` ECS cluster.

**Idle rules (structural only, no CloudWatch):** EBS volume `available`; EIP with no association; ENI `available`; ALB/NLB with all target groups empty or CLB with zero instances; EBS snapshot whose source volume no longer exists; AMI with no instance launched from it; RDS instance stopped. NAT gateways are never Idle in v0.1, only costed.

**Failure handling:** each scanner declares `Permissions() []string`. AccessDenied or a 30s timeout on one scanner → skip it, print a notice, continue. Exit 2 only if every scanner fails or credentials are absent. No global scan timeout.

**Trust posture:** read-only — `iam-policy.json` hand-written in the repo, with a test asserting each scanner's declared actions ⊆ policy. No telemetry in v0.1; state contents never leave the machine.

## 4. Covered AWS Resource Types — Launch List (25)

Chosen for waste-likelihood × small-team ubiquity. Published as a hand-written coverage matrix in the README, with a test asserting the README's list equals the registered scanners.

- **Compute/network waste (the money):** EC2 instances, EBS volumes (esp. unattached), EBS snapshots, Elastic IPs (unassociated), NAT gateways, ALB/NLB/CLB, ENIs (unattached)
- **Data:** RDS instances, RDS snapshots, DynamoDB tables, ElastiCache clusters, S3 buckets
- **Compute misc:** Lambda functions, ECS clusters/services, EKS clusters, Auto Scaling groups, AMIs (owned)
- **Plumbing:** security groups, VPCs (non-default), subnets, route tables, IAM roles, IAM users, CloudWatch log groups (retention=never is a classic leak), Route53 hosted zones

**Rule for additions post-launch:** only from user requests, only with a cost story, cap total at ~50.

## 5. Cost & Carbon Estimation

**Cost:** static pricing table shipped in the binary, refreshed at release time by `hack/pricing/` from the AWS Pricing API — never queried live. Flat rates (NAT hourly, EBS per GB, EIP, ALB hourly, snapshot per GB) for all regions. Instance-class tables (EC2, RDS, ElastiCache) for us-east-1, us-east-2, us-west-2, eu-west-1, eu-west-2, eu-central-1, ap-southeast-1, ap-southeast-2, ap-northeast-1, ca-central-1; other regions use the us-east-1 price marked `≈` (`usd_mo_approx`, omitted when false, in `--json`).

Cost basis: running instance = on-demand hourly × 730 plus its Derived EBS volumes; stopped instance = those volumes only; EBS and snapshots = GB × rate (snapshots at full size); log groups = `storedBytes` from DescribeLogGroups; S3 = $0 with note "size unknown"; Lambda, IAM, SG, VPC, subnet, route table = $0. Reserved instances and savings plans ignored. Everything labeled "estimated"; ranking matters more than precision.

**Carbon:** Cloud Carbon Footprint methodology. Compute = vCPU × average watts (midpoint of CCF min/max) × 730 h × PUE × regional grid intensity; storage = TB-hours × CCF coefficient; network ignored; plumbing = 0. vCPU counts come from the instance-type table already needed for pricing. Coefficients and grid intensities shipped as JSON in `internal/carbon/`, CCF cited in the README. One decimal, clearly labeled estimate. All figures monthly.

**Footer math:** `Unmanaged: $X/mo · Idle: $Y/mo · ~Z kgCO₂/mo (≈ N trans-Atlantic flights)`. Unmanaged Spend and Idle Waste overlap by design (an unmanaged unattached volume counts in both). Drift contributes $0.

## 6. Repo & Stack

- **Go**, latest stable at build time, minor pinned in `go.mod`; tiny static binaries via goreleaser (linux/darwin, amd64/arm64), Homebrew tap at `wardbox/homebrew-tap` + `go install`
- **Dependencies, complete list:** cobra, aws-sdk-go-v2 (per-service modules), BurntSushi/toml, golang.org/x/sync. Table rendering via stdlib `text/tabwriter`. Any new dependency needs a justification line in its PR.
- **Layout:** `internal/state/` (v4 parser + local/S3 readers + `tofu state pull`), `internal/plan/` (refresh-only runner + parser), `internal/scan/<service>.go` (one file per service implementing `Scanner` with `List(ctx) ([]LiveResource, error)` and `Permissions() []string` — the contributor surface), `internal/match/` (Match Key table, derived/furniture suppression), `internal/pricing/` + `internal/carbon/` (data via `go:embed`), `internal/report/`, `cmd/tofu-drift/`, `hack/pricing/`
- **Tests:** each scanner takes a narrow interface (only the SDK methods it calls); tests use hand-written fakes with canned responses. Golden-file tests on recorded state/plan JSON. No live AWS in CI. Build order: core with hand-written fixtures first, then `sandbox/` — an OpenTofu stack in a throwaway AWS account provisioning ~15 resource types, a make-mess script that induces Drift and plants Unmanaged/Idle resources, and a tear-down script. Sandbox output replaces hand-written fixtures and produces the demo screenshot.
- **CI:** GitHub Actions — golangci-lint, `go test`, DCO check, goreleaser on `v*` tags. Nothing else until asked.
- **License Apache-2.0** (matches OpenTofu — no CLA, plain DCO sign-offs)
