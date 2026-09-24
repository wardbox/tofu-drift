# Sandbox

A throwaway AWS stack for exercising tofu-drift end to end: an OpenTofu stack with 18 covered resource types, a script that makes a mess, a script that cleans it up, and a script that records real API responses as test fixtures.

**Use a throwaway account.** The scripts create, change and delete resources. Every script prints the account ID from `sts get-caller-identity` and waits for you to type `yes`, and refuses to run against an account other than the one the sandbox state was applied to. Everything the sandbox creates carries the tag `tofu-drift-sandbox=<name_prefix>`. What `make-mess.sh` plants also carries `tofu-drift-sandbox-planted=true`, which is how `teardown.sh` finds it.

## Cost

us-east-1 on-demand prices. Other regions are within a few cents.

| What | Rate | When |
|---|---|---|
| Internal ALB (one empty target group) | ~$0.023/h | from `tofu apply` |
| EBS: 8 GB root + 1 GB data + 1 GB planted, gp3 | ~$0.001/h | from `tofu apply` |
| Snapshot and AMI (1 GB) | ~$0.0001/h | from `tofu apply` |
| Route53 private hosted zone | $0.50/mo, not charged if deleted within 12 h | from `tofu apply` |
| t4g.nano instance | $0 while stopped (the stack stops it) | |
| Lambda, DynamoDB on-demand, S3, log groups, ECS, ASG at 0, IAM | ~$0 when idle | |
| **NAT gateway** | **$0.045/h** + data | from `make-mess.sh` |
| 2 Elastic IPs (planted, and the NAT gateway's) | $0.005/h each | from `make-mess.sh` |

**Total: about $0.025/h for the stack alone, about $0.08/h after `make-mess.sh`.** A two-hour session costs well under $1. The NAT gateway is most of it, so run `teardown.sh` as soon as you have the screenshot and the fixtures.

## Prerequisites

- A throwaway AWS account and a CLI profile for it with admin rights
- `tofu` (or `terraform`), the `aws` CLI v2, Go

## Runbook

All commands run from the repo root unless noted.

1. **Point at the throwaway account.** Set this in every shell you use:

   ```sh
   export AWS_PROFILE=<throwaway-profile>
   export TF_VAR_profile=$AWS_PROFILE
   aws sts get-caller-identity   # check the account ID before anything else
   ```

   The region defaults to `us-east-1` and the name prefix to `tdsbx`. To change either, pass `-var region=...` / `-var name_prefix=...`, or put them in `sandbox/terraform.tfvars` (it's gitignored) so every later command picks them up.

2. **Apply the stack** (about 5 minutes):

   ```sh
   cd sandbox
   tofu init
   tofu apply
   ```

   Check the plan says `profile` is your throwaway profile before you type `yes`.

3. **Check the clean baseline.** Build tofu-drift and scan from `sandbox/`, which is a root module, so Drift runs too:

   ```sh
   go build -o /tmp/tofu-drift ../cmd/tofu-drift
   /tmp/tofu-drift scan --region "$(tofu output -raw region)"
   ```

   Expect no Drift. Expected Idle rows even now: the ALB (its target group is empty) and the AMI (nothing was launched from it).

4. **Make a mess:**

   ```sh
   ./make-mess.sh
   ```

   Out of band, it changes five managed resources: an instance tag, a security group ingress rule, log group retention, the Lambda timeout, and the IAM role description. It also plants an unattached 1 GB volume, an unassociated Elastic IP and a NAT gateway. Wait a minute or two for the NAT gateway to become `available`.

5. **Scan again:**

   ```sh
   /tmp/tofu-drift scan --region "$(tofu output -raw region)"
   ```

   Expect at least 3 Drift rows (`aws_instance.main`, `aws_security_group.main`, `aws_cloudwatch_log_group.app`, `aws_lambda_function.main`, `aws_iam_role.lambda`) and at least 3 Unmanaged/Idle rows (the planted volume `unmanaged+idle`, the planted EIP `unmanaged+idle`, the NAT gateway `unmanaged`, with the NAT's own EIP folded into it). Try `--explain <id>` on one of each and `--json`. Anything else in the account shows up too; a fresh account should add nothing beyond Default Furniture, which is suppressed.

6. **Take the README screenshot.** Use a terminal about 120 columns wide with a plain theme. Run `clear`, rerun the step 5 scan, and capture only the terminal window (on macOS, Cmd-Shift-4 then Space). If the account ID is visible, crop it out. Save the image as `docs/screenshot.png`, and add this line under the first paragraph of the top-level `README.md`:

   ```md
   ![tofu-drift scan against the sandbox](docs/screenshot.png)
   ```

7. **Capture fixtures** (read-only):

   ```sh
   ./capture-fixtures.sh
   ```

   This writes one JSON file per SDK call the scanners make to `internal/scan/testdata/captured/`, plus `cmd/tofu-drift/testdata/sandbox.tfstate` and `internal/plan/testdata/sandbox-show.json`. The account ID is replaced with `123456789012`. Read through the diff before committing. The files come from the `aws` CLI, so field names match the SDK output structs and `json.Unmarshal` into e.g. `ec2.DescribeVolumesOutput` should load them (check timestamps the first time). The follow-up is to swap each scanner test's hand-written happy-path fake for its captured file. Keep the hand-written fakes that test pagination and errors, since a small account won't paginate or fail.

8. **Tear down promptly:**

   ```sh
   ./teardown.sh
   ```

   It deletes the NAT gateway and waits for it to go, releases the planted Elastic IPs, deletes the planted volume, runs `tofu destroy`, then lists any ARN still tagged `tofu-drift-sandbox`. The list should be empty. A deleted NAT gateway can stay listed for about an hour, and costs nothing once deleted. If `tofu destroy` fails partway, fix the cause and run `./teardown.sh` again; every step is safe to repeat.
