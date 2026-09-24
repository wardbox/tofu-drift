# hack/pricing

Regenerates the pricing tables embedded in the binary. Run it at release time, from the repo root:

```sh
go run ./hack/pricing
```

No AWS credentials needed: it streams the public [AWS Price List Bulk API](https://docs.aws.amazon.com/awsaccountbilling/latest/aboutv2/using-the-aws-price-list-bulk-api.html) CSV offer files (about 3 GB for EC2 across all regions, roughly a minute and a half) and writes:

- `internal/pricing/instances.json`: vCPU count per instance class, and on-demand USD/hour per region per class, for EC2 (Linux, shared tenancy), RDS (MySQL, Single-AZ) and ElastiCache (Redis). Regions are the ten in `docs/spec.md` §5; tofu-drift prices any other region at the us-east-1 rate and marks it `≈`.
- `internal/pricing/ebs.json`: USD per GB-month per volume type, refreshed for the same ten regions. Other regions, and `io2` (not in the offer files), are kept as hand-seeded.

Only on-demand prices are collected. Reserved instances and savings plans are ignored.

The script fails if a filter matches two different prices for one class, rather than silently picking one. When AWS adds a new row variant, tighten the filter in `main.go`.

Review the diff before committing; a large unexplained price change usually means a filter drifted.
