package scan

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
)

// Route53Zones lists hosted zones. Global. The Match Key is the zone ID
// without "/hostedzone/"; NAME is the domain. Zones a service created for
// itself (Cloud Map) are skipped.
type Route53Zones struct {
	Client route53.ListHostedZonesAPIClient
}

func (Route53Zones) Permissions() []string { return []string{"route53:ListHostedZones"} }

func (s Route53Zones) List(ctx context.Context) ([]LiveResource, error) {
	var out []LiveResource
	p := route53.NewListHostedZonesPaginator(s.Client, &route53.ListHostedZonesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, z := range page.HostedZones {
			if z.LinkedService != nil {
				continue
			}
			out = append(out, LiveResource{
				Type:   "aws_route53_zone",
				Key:    strings.TrimPrefix(aws.ToString(z.Id), "/hostedzone/"),
				Name:   aws.ToString(z.Name),
				Region: "global",
			})
		}
	}
	return out, nil
}
