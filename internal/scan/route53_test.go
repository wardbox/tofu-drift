package scan

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
)

type fakeZones []types.HostedZone

func (f fakeZones) ListHostedZones(context.Context, *route53.ListHostedZonesInput, ...func(*route53.Options)) (*route53.ListHostedZonesOutput, error) {
	return &route53.ListHostedZonesOutput{HostedZones: f}, nil
}

func TestRoute53List(t *testing.T) {
	got, err := Route53Zones{fakeZones{
		{Id: aws.String("/hostedzone/Z123"), Name: aws.String("example.com.")},
		{Id: aws.String("/hostedzone/ZMAP"), Name: aws.String("svc.local."), LinkedService: &types.LinkedService{ServicePrincipal: aws.String("servicediscovery.amazonaws.com")}},
	}}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %+v, want 1 (service-linked zone skipped)", got)
	}
	if z := got[0]; z.Type != "aws_route53_zone" || z.Key != "Z123" || z.Name != "example.com." || z.Region != "global" || z.Created != nil {
		t.Errorf("zone: %+v", z)
	}
}
