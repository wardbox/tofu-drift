#!/usr/bin/env bash
# Induce Drift on the sandbox stack and plant Unmanaged/Idle resources.
# Run once after `tofu apply`, from sandbox/. Planted resources carry
# Mess=true so teardown.sh can find them.
set -euo pipefail
cd "$(dirname "$0")"

out() { tofu output -raw "$1"; }
vpc=$(out vpc_id)
subnet=$(out subnet_id)
instance=$(out instance_id)
sg=$(out security_group_id)
bucket=$(out bucket)
log_group=$(out log_group)
az=$(aws ec2 describe-subnets --subnet-ids "$subnet" --query 'Subnets[0].AvailabilityZone' --output text)

tags() { echo "ResourceType=$1,Tags=[{Key=Project,Value=tofu-drift-sandbox},{Key=Mess,Value=true},{Key=Name,Value=$2}]"; }

echo "== drift"
aws ec2 create-tags --resources "$instance" --tags Key=Owner,Value=someone-else
aws ec2 authorize-security-group-ingress --group-id "$sg" --protocol tcp --port 8080 --cidr 10.0.0.0/8 >/dev/null
aws logs put-retention-policy --log-group-name "$log_group" --retention-in-days 30
aws s3api put-bucket-tagging --bucket "$bucket" \
  --tagging 'TagSet=[{Key=Project,Value=tofu-drift-sandbox},{Key=Team,Value=clickops}]'

echo "== unmanaged and idle"
aws ec2 create-volume --availability-zone "$az" --size 20 --volume-type gp3 \
  --tag-specifications "$(tags volume mess-orphan-volume)" --query VolumeId --output text
aws ec2 create-security-group --group-name mess-orphan-sg --description "tofu-drift sandbox mess" \
  --vpc-id "$vpc" --tag-specifications "$(tags security-group mess-orphan-sg)" --query GroupId --output text
aws ec2 create-route-table --vpc-id "$vpc" \
  --tag-specifications "$(tags route-table mess-orphan-rtb)" --query RouteTable.RouteTableId --output text
aws ec2 allocate-address --domain vpc \
  --tag-specifications "$(tags elastic-ip mess-orphan-eip)" --query AllocationId --output text
nat_eip=$(aws ec2 allocate-address --domain vpc \
  --tag-specifications "$(tags elastic-ip mess-nat-eip)" --query AllocationId --output text)
aws ec2 create-nat-gateway --subnet-id "$subnet" --allocation-id "$nat_eip" \
  --tag-specifications "$(tags natgateway mess-nat)" --query NatGateway.NatGatewayId --output text

echo "Done. The NAT gateway bills ~\$0.045/h until teardown.sh runs."
