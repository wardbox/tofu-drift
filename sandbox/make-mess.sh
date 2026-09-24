#!/usr/bin/env bash
# Changes managed sandbox resources out of band (Drift) and plants an
# unattached volume, an unassociated Elastic IP and a NAT gateway
# (Unmanaged/Idle). Run after `tofu apply`; undo with ./teardown.sh.
set -euo pipefail
# shellcheck source=lib.sh
source "$(dirname "$0")/lib.sh"

confirm_account "Make a mess"
AWS_REGION=$(out region)
export AWS_REGION
prefix=$(out name_prefix)
planted() { echo "ResourceType=$1,Tags=[{Key=$TAG_KEY,Value=$prefix},{Key=$PLANTED_KEY,Value=true},{Key=Name,Value=$prefix-$2}]"; }

echo "Drift: instance tag"
aws ec2 create-tags --resources "$(out instance_id)" --tags Key=Owner,Value=someone-else

echo "Drift: security group ingress"
aws ec2 authorize-security-group-ingress --group-id "$(out security_group_id)" \
	--protocol tcp --port 22 --cidr 0.0.0.0/0 >/dev/null

echo "Drift: log group retention"
aws logs put-retention-policy --log-group-name "$(out log_group_name)" --retention-in-days 7

echo "Drift: Lambda timeout"
aws lambda update-function-configuration --function-name "$(out lambda_name)" --timeout 10 >/dev/null

echo "Drift: IAM role description"
aws iam update-role --role-name "$(out iam_role_name)" --description "edited by hand"

echo "Plant: unattached EBS volume"
aws ec2 create-volume --availability-zone "$(out availability_zone)" --volume-type gp3 --size 1 \
	--tag-specifications "$(planted volume stray-volume)" --query VolumeId --output text

echo "Plant: unassociated Elastic IP"
aws ec2 allocate-address --domain vpc \
	--tag-specifications "$(planted elastic-ip stray-eip)" --query AllocationId --output text

echo "Plant: NAT gateway (about \$0.05/h with its IP; run ./teardown.sh promptly)"
nat_eip=$(aws ec2 allocate-address --domain vpc \
	--tag-specifications "$(planted elastic-ip nat-eip)" --query AllocationId --output text)
aws ec2 create-nat-gateway --subnet-id "$(out subnet_id)" --allocation-id "$nat_eip" \
	--tag-specifications "$(planted natgateway stray-nat)" --query NatGateway.NatGatewayId --output text

echo "Done: 5 out-of-band changes (Drift) and 3 planted resources (Unmanaged/Idle)."
