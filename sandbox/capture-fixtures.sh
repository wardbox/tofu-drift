#!/usr/bin/env bash
# Records the read-only responses every scanner consumes, one JSON file per
# call, into internal/scan/testdata/captured/. Run after make-mess.sh so the
# mess is in the recording.
#
# Every call is narrowed to the sandbox (by tag, VPC or name prefix), so
# nothing else in the account ends up in the repo. The account ID becomes
# 123456789012, the caller becomes user/sandbox and public IPs become
# 203.0.113.x. Still read the diff before committing.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "$0")/lib.sh"

confirm_account "Record read-only API responses"
AWS_REGION=$(out region)
export AWS_REGION
account=$(out account_id)
prefix=$(out name_prefix)
vpc=$(out vpc_id)
instance=$(out instance_id)
read -r caller_id caller_arn < <(aws sts get-caller-identity --query '[UserId, Arn]' --output text)
dir=../internal/scan/testdata/captured
mkdir -p "$dir"

# scrub: account ID, caller, public IPs.
scrub() {
	# IAM unique IDs (AIDA..., AROA...) encode the account, so they go too.
	sed -E -e "s|$caller_arn|arn:aws:iam::123456789012:user/sandbox|g" -e "s/$account/123456789012/g" \
		-e 's/(AIDA|AROA|AKIA|ASIA|ANPA|AGPA)[A-Z0-9]{12,}/\1EXAMPLEEXAMPLE/g' |
		jq 'walk(if type == "object" then with_entries(
			if (.key | test("^(PublicIp|CarrierIp|CustomerOwnedIp)$")) then .value = "203.0.113.10"
			elif .key == "PublicDnsName" and .value != "" then .value = "ec2-203-0-113-10.compute.amazonaws.com"
			else . end) else . end)'
}

# rec NAME aws-args...: one call, saved as NAME.json.
rec() {
	local name=$1
	shift
	echo "$name"
	aws "$@" --output json | scrub >"$dir/$name.json"
}

tagged="Name=tag:$TAG_KEY,Values=$prefix"
in_vpc="Name=vpc-id,Values=$vpc"
has_tag="Tags[?Key=='$TAG_KEY']"

rec ec2.DescribeInstances ec2 describe-instances --filters "$tagged"
# The root volume may not carry default tags; take it by attachment.
rec ec2.DescribeVolumes ec2 describe-volumes \
	--query "{Volumes: Volumes[?$has_tag || Attachments[?InstanceId=='$instance']]}"
rec ec2.DescribeSnapshots ec2 describe-snapshots --owner-ids self --filters "$tagged"
rec ec2.DescribeImages ec2 describe-images --owners self --filters "$tagged"
rec ec2.DescribeAddresses ec2 describe-addresses --filters "$tagged"
rec ec2.DescribeNatGateways ec2 describe-nat-gateways --filter "$tagged"
rec ec2.DescribeNetworkInterfaces ec2 describe-network-interfaces --filters "$in_vpc"
rec ec2.DescribeVpcs ec2 describe-vpcs --filters "$tagged"
rec ec2.DescribeSubnets ec2 describe-subnets --filters "$in_vpc"
rec ec2.DescribeRouteTables ec2 describe-route-tables --filters "$in_vpc"
rec ec2.DescribeSecurityGroups ec2 describe-security-groups --filters "$in_vpc"
rec autoscaling.DescribeAutoScalingGroups autoscaling describe-auto-scaling-groups \
	--query "{AutoScalingGroups: AutoScalingGroups[?starts_with(AutoScalingGroupName, '$prefix-')]}"
rec elb.DescribeLoadBalancers elb describe-load-balancers \
	--query "{LoadBalancerDescriptions: LoadBalancerDescriptions[?starts_with(LoadBalancerName, '$prefix-')]}"
rec elbv2.DescribeLoadBalancers elbv2 describe-load-balancers \
	--query "{LoadBalancers: LoadBalancers[?starts_with(LoadBalancerName, '$prefix-')]}"
rec elbv2.DescribeTargetGroups elbv2 describe-target-groups \
	--query "{TargetGroups: TargetGroups[?starts_with(TargetGroupName, '$prefix-')]}"
# Loop over live ARNs: the recorded ones have the account scrubbed.
for arn in $(aws elbv2 describe-target-groups --output text \
	--query "TargetGroups[?starts_with(TargetGroupName, '$prefix-')].TargetGroupArn"); do
	rec "elbv2.DescribeTargetHealth.${arn##*/}" elbv2 describe-target-health --target-group-arn "$arn"
done
rec rds.DescribeDBInstances rds describe-db-instances \
	--query "{DBInstances: DBInstances[?starts_with(DBInstanceIdentifier, '$prefix-')]}"
rec rds.DescribeDBSnapshots rds describe-db-snapshots \
	--query "{DBSnapshots: DBSnapshots[?starts_with(DBSnapshotIdentifier, '$prefix-')]}"
rec elasticache.DescribeCacheClusters elasticache describe-cache-clusters \
	--query "{CacheClusters: CacheClusters[?starts_with(CacheClusterId, '$prefix-')]}"
rec dynamodb.ListTables dynamodb list-tables --query "{TableNames: TableNames[?starts_with(@, '$prefix-')]}"
for t in $(jq -r '.TableNames[]' "$dir/dynamodb.ListTables.json"); do
	rec "dynamodb.DescribeTable.$t" dynamodb describe-table --table-name "$t"
done
rec ecs.ListClusters ecs list-clusters --query "{clusterArns: clusterArns[?contains(@, ':cluster/$prefix-')]}"
for c in $(aws ecs list-clusters --output text --query "clusterArns[?contains(@, ':cluster/$prefix-')]"); do
	rec "ecs.DescribeClusters.${c##*/}" ecs describe-clusters --clusters "$c" --include TAGS
	rec "ecs.ListServices.${c##*/}" ecs list-services --cluster "$c"
	services=$(aws ecs list-services --cluster "$c" --query serviceArns --output text)
	if [[ -n $services ]]; then
		# shellcheck disable=SC2086 # one argument per ARN
		rec "ecs.DescribeServices.${c##*/}" ecs describe-services --cluster "$c" --services $services --include TAGS
	fi
done
rec eks.ListClusters eks list-clusters --query "{clusters: clusters[?starts_with(@, '$prefix-')]}"
for c in $(jq -r '.clusters[]' "$dir/eks.ListClusters.json"); do
	rec "eks.DescribeCluster.$c" eks describe-cluster --name "$c"
done
rec lambda.ListFunctions lambda list-functions \
	--query "{Functions: Functions[?starts_with(FunctionName, '$prefix-')]}"
rec logs.DescribeLogGroups logs describe-log-groups \
	--query "{logGroups: logGroups[?contains(logGroupName, '$prefix-')]}"
rec iam.ListRoles iam list-roles --query "{Roles: Roles[?starts_with(RoleName, '$prefix-')]}"
# The CLI decodes AssumeRolePolicyDocument; the API and the SDK have it as a string.
jq '.Roles[].AssumeRolePolicyDocument |= tojson' "$dir/iam.ListRoles.json" >"$dir/iam.ListRoles.json.tmp"
mv "$dir/iam.ListRoles.json.tmp" "$dir/iam.ListRoles.json"
rec iam.ListUsers iam list-users --query "{Users: Users[?starts_with(UserName, '$prefix-')]}"
rec route53.ListHostedZones route53 list-hosted-zones \
	--query "{HostedZones: HostedZones[?starts_with(Name, '$prefix.')]}"
rec s3.ListBuckets s3api list-buckets --query "{Buckets: Buckets[?starts_with(Name, '$prefix-')]}"

# Golden state and refresh-only plan, the other two inputs tofu-drift reads.
tofu state pull | scrub >../cmd/tofu-drift/testdata/sandbox.tfstate
tofu plan -refresh-only -lock=false -input=false -out=.terraform/refresh.tfplan >/dev/null
tofu show -json .terraform/refresh.tfplan | scrub >../internal/plan/testdata/sandbox-show.json

echo "Wrote $dir, cmd/tofu-drift/testdata/sandbox.tfstate and internal/plan/testdata/sandbox-show.json."
echo "Review them for anything sensitive before committing."
