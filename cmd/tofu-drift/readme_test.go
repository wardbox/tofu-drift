package main

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// coveredTypes lists the OpenTofu types each registered scanner covers.
// A new scanner fails TestReadmeCoverage until it is added here and to the
// README's Coverage table.
var coveredTypes = map[string][]string{
	"scan.EBS":                  {"aws_ebs_volume"},
	"scan.EC2":                  {"aws_instance"},
	"scan.VPCs":                 {"aws_vpc"},
	"scan.Subnets":              {"aws_subnet"},
	"scan.RouteTables":          {"aws_route_table"},
	"scan.SecurityGroups":       {"aws_security_group"},
	"scan.EIPs":                 {"aws_eip"},
	"scan.NATGateways":          {"aws_nat_gateway"},
	"scan.ENIs":                 {"aws_network_interface"},
	"scan.Snapshots":            {"aws_ebs_snapshot"},
	"scan.AMIs":                 {"aws_ami"},
	"scan.LoadBalancers":        {"aws_lb"},
	"scan.ClassicLoadBalancers": {"aws_elb"},
	"scan.RDSInstances":         {"aws_db_instance", "aws_rds_cluster_instance"},
	"scan.RDSSnapshots":         {"aws_db_snapshot"},
	"scan.DynamoDBTables":       {"aws_dynamodb_table"},
	"scan.ElastiCache":          {"aws_elasticache_cluster", "aws_elasticache_replication_group"},
	"scan.S3Buckets":            {"aws_s3_bucket"},
	"scan.AutoScalingGroups":    {"aws_autoscaling_group"},
	"scan.EKSClusters":          {"aws_eks_cluster"},
	"scan.ECSClusters":          {"aws_ecs_cluster"},
	"scan.ECSServices":          {"aws_ecs_service"},
	"scan.IAMRoles":             {"aws_iam_role"},
	"scan.IAMUsers":             {"aws_iam_user"},
	"scan.Route53Zones":         {"aws_route53_zone"},
	"scan.LambdaFunctions":      {"aws_lambda_function"},
	"scan.LogGroups":            {"aws_cloudwatch_log_group"},
}

func TestReadmeCoverage(t *testing.T) {
	var registered []string
	for _, s := range realScanners(aws.Config{}) {
		name := fmt.Sprintf("%T", s)
		types, ok := coveredTypes[name]
		if !ok {
			t.Errorf("%s is registered but not in coveredTypes; add it there and a row to the README Coverage table", name)
		}
		registered = append(registered, types...)
	}

	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, _ := strings.Cut(string(readme), "\n## Coverage\n")
	section, _, _ = strings.Cut(section, "\n## ")
	var documented []string
	for line := range strings.Lines(section) {
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			continue
		}
		for _, m := range regexp.MustCompile("`(aws_[a-z0-9_]+)`").FindAllStringSubmatch(cells[2], -1) {
			documented = append(documented, m[1])
		}
	}

	for _, typ := range registered {
		if !slices.Contains(documented, typ) {
			t.Errorf("%s is scanned but has no row in the README Coverage table", typ)
		}
	}
	for _, typ := range documented {
		if !slices.Contains(registered, typ) {
			t.Errorf("README Coverage table lists %s, but no registered scanner covers it", typ)
		}
	}
}
