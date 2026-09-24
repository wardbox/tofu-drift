# tofu-drift

## Permissions

tofu-drift is read-only. [`iam-policy.json`](iam-policy.json) lists every AWS action a scan calls, and a test keeps it in step with the scanners. Run without credentials and tofu-drift prints that policy and exits 2. Reading state from `s3://` additionally needs `s3:GetObject` on the state object.

If one service's calls are denied or take longer than 30 seconds, that service is skipped with a notice naming the actions it needs, and the rest of the report is still produced. The run exits 2 only when every service fails.

## Coverage

| Resource | OpenTofu type | Idle when | Cost basis |
|---|---|---|---|
| EC2 instance | `aws_instance` | never | on-demand hourly × 730 plus its launch-time volumes; stopped: volumes only |
| EBS volume | `aws_ebs_volume` | unattached | GB × per-GB rate |
| EBS snapshot | `aws_ebs_snapshot` | its source volume is gone | full volume GB × snapshot rate |
| AMI (owned) | `aws_ami` | no live instance was launched from it | its snapshots |
| Elastic IP | `aws_eip` | unassociated | public IPv4 hourly × 730 |
| NAT gateway | `aws_nat_gateway` | never | hourly × 730, plus its Elastic IPs |
| ALB / NLB | `aws_lb` | it has target groups and all are empty | hourly × 730 |
| Classic load balancer | `aws_elb` | no instances | hourly × 730 |
| Auto Scaling group | `aws_autoscaling_group` | never | its instances |
| EKS cluster | `aws_eks_cluster` | never | $0.10/h control plane × 730, plus its nodes |
| ECS cluster | `aws_ecs_cluster` | never | $0 (the `default` cluster is suppressed unless `--include-defaults`) |
| ECS service | `aws_ecs_service` | never | $0 |
| Network interface | `aws_network_interface` | unattached | $0 |
| VPC | `aws_vpc` | never | $0 |
| Subnet | `aws_subnet` | never | $0 |
| Route table | `aws_route_table` | never | $0 |
| Security group | `aws_security_group` | never | $0 |
| RDS instance | `aws_db_instance`, `aws_rds_cluster_instance` | stopped | on-demand hourly × 730, ×2 for Multi-AZ; stopped: $0 |
| RDS snapshot | `aws_db_snapshot` | never | $0 |
| DynamoDB table | `aws_dynamodb_table` | never | $0, size unknown |
| ElastiCache cluster | `aws_elasticache_cluster`, `aws_elasticache_replication_group` | never | node hourly × 730 × nodes |
| S3 bucket | `aws_s3_bucket` | never | $0, size unknown |

Resources another resource creates are folded into their parent's row: an instance's launch-time volumes and network interface, a NAT gateway's Elastic IPs and network interface, an AMI's snapshots, an RDS instance's automated snapshots, an Auto Scaling group's instances, and an EKS cluster's security group, control-plane and VPC CNI network interfaces, nodegroup Auto Scaling groups and nodes (found by the `eks:cluster-name` tag). When the parent is managed, these never appear. ElastiCache clusters in a replication group are one row for the group. Network interfaces AWS services manage for themselves (load balancers, Lambda, RDS, VPC endpoints) are skipped. Load-balancer processing (LCU) and NAT data charges are not estimated, nor is RDS storage, backup or snapshot storage. RDS is priced at MySQL rates and ElastiCache at Redis rates whatever the engine. S3 buckets are listed account-wide with no per-bucket calls, so their region shows as `global`; DynamoDB tables are not sized.

## Ignore Rules

A `tofu-drift.toml` in the current directory (or `--config <path>`) removes matching Unmanaged and Idle rows from the table, the totals and `--json`. Drift is never ignored. Every field set in one rule must match; a malformed file exits 2.

```toml
[[ignore]]
tag = "ManagedBy=other"

[[ignore]]
type = "aws_s3_bucket"

[[ignore]]
arn = "arn:aws:iam::*:role/legacy-*"  # path.Match glob: * does not cross /
```

## Estimates

Every $/mo and kgCO₂/mo figure is an estimate, meant for ranking rather than billing.

- **Cost** comes from static on-demand tables embedded in the binary and refreshed at release time by [`hack/pricing/`](hack/pricing/README.md). Nothing is queried live. Reserved instances and savings plans are ignored, so committed-use discounts are not reflected. A running instance costs its on-demand hourly rate × 730 plus its volumes; a stopped instance costs only the EBS volumes created with it. Those volumes are folded into the instance's row; volumes attached later get their own rows. Instance prices exist for ten regions; any other region uses the us-east-1 price, shown with `≈`.
- **Carbon** follows the [Cloud Carbon Footprint methodology](https://www.cloudcarbonfootprint.org/docs/methodology): compute is vCPU × the midpoint of CCF's AWS min/max watts per vCPU × 730 h × PUE × regional grid intensity; storage is TB-hours × CCF's SSD/HDD coefficient, with snapshots counted as HDD. Network is ignored, so Elastic IPs, NAT gateways, network interfaces and load balancers carry no carbon. Coefficients live in `internal/carbon/carbon.json`.
