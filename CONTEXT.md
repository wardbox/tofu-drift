# tofu-drift

A read-only CLI that compares an OpenTofu/Terraform state against a live AWS account and reports what has changed out of band, what is not managed at all, and what that is costing.

## Language

### Resources

**Managed Resource**:
An AWS resource whose ID or ARN appears in the scanned state.
_Avoid_: tracked resource, known resource

**Unmanaged Resource**:
A live AWS resource whose ID or ARN appears in no scanned state.
_Avoid_: orphan, orphaned resource, untracked resource, ghost resource

**Idle Resource**:
A live AWS resource, managed or not, that is provably unused by a per-type rule (unattached EBS volume, unassociated Elastic IP, unattached ENI, load balancer with zero targets). Idleness is independent of management status.
_Avoid_: zombie, dead resource, wasted resource

**Derived Resource**:
A live AWS resource created by another live AWS resource (instances of an Auto Scaling group, ENIs of a load balancer). Never reported on its own; always folded into its parent.
_Avoid_: child resource, implicit resource

**Default Furniture**:
Resources AWS creates in every account or VPC without being asked (default VPC, default security group, main route table). Suppressed unless explicitly included.
_Avoid_: built-in resources, AWS defaults

### Findings

**Finding**:
One row in a report, for one resource, carrying one or more of: Drift, Unmanaged, Idle. Drift and Unmanaged never co-occur; Idle can pair with either.
_Avoid_: issue, violation, result

**Drift**:
An out-of-band change to a Managed Resource, as reported by the provider during a refresh-only plan. Drift is never costed.
_Avoid_: diff, divergence, config skew

**Finding ID**:
The single identifier shown for a Finding. For Drift it is the resource address; for Unmanaged and Idle Resources it is the AWS ID, or the ARN where the type has no short ID.
_Avoid_: resource key, handle

### Money

**Estimate**:
A monthly cost or carbon figure derived from a static pricing table shipped with the binary. Always labeled as an estimate; ranking matters, precision does not.
_Avoid_: actual cost, bill, spend

**Unmanaged Spend**:
The sum of Estimates for all Unmanaged Resources in a report.

**Idle Waste**:
The sum of Estimates for all Idle Resources in a report.
_Avoid_: waste (unqualified), savings

### Scan

**Scan**:
One run of the tool against one state, one AWS account, and one region. Global services (IAM, Route53, S3) are always included.
_Avoid_: audit, check, run

**Match Key**:
The single per-type canonical identifier used to decide whether a live resource is in state (instance ID, bucket name, role name, load balancer ARN).
_Avoid_: resource key, lookup key

**Ignore Rule**:
A configured rule (by tag, type, or ARN glob) that removes Unmanaged and Idle Findings from a report. Ignore Rules never affect Drift.
_Avoid_: exclusion, filter, allowlist
