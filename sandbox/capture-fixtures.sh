#!/usr/bin/env bash
# Records the read-only responses every scanner consumes, one JSON file per
# call, into internal/scan/testdata/captured/. Run after make-mess.sh so the
# mess is in the recording. The account ID is replaced with 123456789012.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "$0")/lib.sh"

confirm_account "Record read-only API responses"
AWS_REGION=$(out region)
export AWS_REGION
account=$(out account_id)
dir=../internal/scan/testdata/captured
mkdir -p "$dir"

# rec NAME aws-args...: one call, saved as NAME.json.
rec() {
	local name=$1
	shift
	echo "$name"
	aws "$@" --output json | sed "s/$account/123456789012/g" >"$dir/$name.json"
}

rec sts.GetCallerIdentity sts get-caller-identity
rec ec2.DescribeInstances ec2 describe-instances
rec ec2.DescribeVolumes ec2 describe-volumes
rec ec2.DescribeSnapshots ec2 describe-snapshots --owner-ids self
rec ec2.DescribeImages ec2 describe-images --owners self
rec ec2.DescribeAddresses ec2 describe-addresses
rec ec2.DescribeNatGateways ec2 describe-nat-gateways
rec ec2.DescribeNetworkInterfaces ec2 describe-network-interfaces
rec ec2.DescribeVpcs ec2 describe-vpcs
rec ec2.DescribeSubnets ec2 describe-subnets
rec ec2.DescribeRouteTables ec2 describe-route-tables
rec ec2.DescribeSecurityGroups ec2 describe-security-groups
rec autoscaling.DescribeAutoScalingGroups autoscaling describe-auto-scaling-groups
rec elb.DescribeLoadBalancers elb describe-load-balancers
rec elbv2.DescribeLoadBalancers elbv2 describe-load-balancers
rec elbv2.DescribeTargetGroups elbv2 describe-target-groups
for arn in $(aws elbv2 describe-target-groups --query 'TargetGroups[].TargetGroupArn' --output text); do
	rec "elbv2.DescribeTargetHealth.${arn##*/}" elbv2 describe-target-health --target-group-arn "$arn"
done
rec rds.DescribeDBInstances rds describe-db-instances
rec rds.DescribeDBSnapshots rds describe-db-snapshots
rec elasticache.DescribeCacheClusters elasticache describe-cache-clusters
rec dynamodb.ListTables dynamodb list-tables
for t in $(aws dynamodb list-tables --query TableNames --output text); do
	rec "dynamodb.DescribeTable.$t" dynamodb describe-table --table-name "$t"
done
rec ecs.ListClusters ecs list-clusters
for c in $(aws ecs list-clusters --query clusterArns --output text); do
	rec "ecs.DescribeClusters.${c##*/}" ecs describe-clusters --clusters "$c"
	rec "ecs.ListServices.${c##*/}" ecs list-services --cluster "$c"
	services=$(aws ecs list-services --cluster "$c" --query serviceArns --output text)
	if [[ -n $services ]]; then
		# shellcheck disable=SC2086 # one argument per ARN
		rec "ecs.DescribeServices.${c##*/}" ecs describe-services --cluster "$c" --services $services
	fi
done
rec eks.ListClusters eks list-clusters
for c in $(aws eks list-clusters --query clusters --output text); do
	rec "eks.DescribeCluster.$c" eks describe-cluster --name "$c"
done
rec lambda.ListFunctions lambda list-functions
rec logs.DescribeLogGroups logs describe-log-groups
rec iam.ListRoles iam list-roles
rec iam.ListUsers iam list-users
rec route53.ListHostedZones route53 list-hosted-zones
rec s3.ListBuckets s3api list-buckets

# Golden state and refresh-only plan, the other two inputs tofu-drift reads.
tofu state pull | sed "s/$account/123456789012/g" >../cmd/tofu-drift/testdata/sandbox.tfstate
tofu plan -refresh-only -lock=false -input=false -out=.terraform/refresh.tfplan >/dev/null
tofu show -json .terraform/refresh.tfplan | sed "s/$account/123456789012/g" >../internal/plan/testdata/sandbox-show.json

echo "Wrote $dir, cmd/tofu-drift/testdata/sandbox.tfstate and internal/plan/testdata/sandbox-show.json."
echo "Review them for anything sensitive before committing."
