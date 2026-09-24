#!/usr/bin/env bash
# Remove everything make-mess.sh planted, then destroy the stack, then list
# anything still tagged Project=tofu-drift-sandbox.
set -euo pipefail
cd "$(dirname "$0")"

mess=(--filters Name=tag:Mess,Values=true)
ids() { aws ec2 "$1" "${mess[@]}" --query "$2" --output text; }

# NAT first: it holds an EIP and blocks the subnet from being destroyed.
nats=$(ids describe-nat-gateways 'NatGateways[?State!=`deleted`].NatGatewayId')
for id in $nats; do aws ec2 delete-nat-gateway --nat-gateway-id "$id" >/dev/null; done
[ -n "$nats" ] && aws ec2 wait nat-gateway-deleted --nat-gateway-ids $nats

for id in $(ids describe-addresses 'Addresses[].AllocationId'); do aws ec2 release-address --allocation-id "$id"; done
for id in $(ids describe-volumes 'Volumes[].VolumeId'); do aws ec2 delete-volume --volume-id "$id"; done
for id in $(ids describe-security-groups 'SecurityGroups[].GroupId'); do aws ec2 delete-security-group --group-id "$id"; done
for id in $(ids describe-route-tables 'RouteTables[].RouteTableId'); do aws ec2 delete-route-table --route-table-id "$id"; done

tofu destroy -auto-approve

echo "== still tagged (tagging API lags a few minutes; empty is good)"
aws resourcegroupstaggingapi get-resources --tag-filters Key=Project,Values=tofu-drift-sandbox \
  --query 'ResourceTagMappingList[].ResourceARN' --output text
