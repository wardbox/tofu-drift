# tofu-drift

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
| Network interface | `aws_network_interface` | unattached | $0 |
| VPC | `aws_vpc` | never | $0 |
| Subnet | `aws_subnet` | never | $0 |
| Route table | `aws_route_table` | never | $0 |
| Security group | `aws_security_group` | never | $0 |

Resources another resource creates are folded into their parent's row: an instance's launch-time volumes and network interface, a NAT gateway's Elastic IPs and network interface, an AMI's snapshots. Network interfaces AWS services manage for themselves (load balancers, Lambda, RDS, VPC endpoints) are skipped. Load-balancer processing (LCU) and NAT data charges are not estimated.

## Estimates

Every $/mo and kgCO₂/mo figure is an estimate, meant for ranking rather than billing.

- **Cost** comes from static on-demand tables embedded in the binary and refreshed at release time by [`hack/pricing/`](hack/pricing/README.md). Nothing is queried live. Reserved instances and savings plans are ignored, so committed-use discounts are not reflected. A running instance costs its on-demand hourly rate × 730 plus its volumes; a stopped instance costs only the EBS volumes created with it. Those volumes are folded into the instance's row; volumes attached later get their own rows. Instance prices exist for ten regions; any other region uses the us-east-1 price, shown with `≈`.
- **Carbon** follows the [Cloud Carbon Footprint methodology](https://www.cloudcarbonfootprint.org/docs/methodology): compute is vCPU × the midpoint of CCF's AWS min/max watts per vCPU × 730 h × PUE × regional grid intensity; storage is TB-hours × CCF's SSD/HDD coefficient, with snapshots counted as HDD. Network is ignored, so Elastic IPs, NAT gateways, network interfaces and load balancers carry no carbon. Coefficients live in `internal/carbon/carbon.json`.
