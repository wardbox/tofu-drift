#!/usr/bin/env bash
# Deletes what make-mess.sh planted (found by tag), destroys the stack, then
# lists anything still carrying the sandbox tag.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "$0")/lib.sh"

confirm_account "Delete the planted resources and destroy the sandbox stack"
# Outputs can be gone after a half-finished destroy; then AWS_REGION must be set.
AWS_REGION=$(out region 2>/dev/null || echo "${AWS_REGION:?no sandbox outputs left; set AWS_REGION and rerun}")
export AWS_REGION
planted_filter="Name=tag:$PLANTED_KEY,Values=true"

nats=$(aws ec2 describe-nat-gateways --filter "$planted_filter" Name=state,Values=pending,available \
	--query 'NatGateways[].NatGatewayId' --output text)
for id in $nats; do
	echo "Deleting NAT gateway $id"
	aws ec2 delete-nat-gateway --nat-gateway-id "$id" >/dev/null
done
if [[ -n $nats ]]; then
	echo "Waiting for NAT gateways to finish deleting (a few minutes)"
	# shellcheck disable=SC2086 # one argument per ID
	aws ec2 wait nat-gateway-deleted --nat-gateway-ids $nats
fi

# After the NAT gateway, so its Elastic IP is disassociated.
for id in $(aws ec2 describe-addresses --filters "$planted_filter" --query 'Addresses[].AllocationId' --output text); do
	echo "Releasing Elastic IP $id"
	aws ec2 release-address --allocation-id "$id"
done

for id in $(aws ec2 describe-volumes --filters "$planted_filter" --query 'Volumes[].VolumeId' --output text); do
	echo "Deleting volume $id"
	aws ec2 delete-volume --volume-id "$id"
done

tofu destroy -auto-approve

echo "Still tagged $TAG_KEY (should be empty; deleted NAT gateways can linger for an hour):"
aws resourcegroupstaggingapi get-resources --tag-filters "Key=$TAG_KEY" \
	--query 'ResourceTagMappingList[].ResourceARN' --output text
