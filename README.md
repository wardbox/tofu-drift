# tofu-drift

## Estimates

Every $/mo and kgCO₂/mo figure is an estimate, meant for ranking rather than billing.

- **Cost** comes from static on-demand tables embedded in the binary and refreshed at release time by [`hack/pricing/`](hack/pricing/README.md). Nothing is queried live. Reserved instances and savings plans are ignored, so committed-use discounts are not reflected. A running instance costs its on-demand hourly rate × 730 plus its volumes; a stopped instance costs only the EBS volumes created with it. Those volumes are folded into the instance's row; volumes attached later get their own rows. Instance prices exist for ten regions; any other region uses the us-east-1 price, shown with `≈`.
- **Carbon** follows the [Cloud Carbon Footprint methodology](https://www.cloudcarbonfootprint.org/docs/methodology): compute is vCPU × the midpoint of CCF's AWS min/max watts per vCPU × 730 h × PUE × regional grid intensity; storage is TB-hours × CCF's SSD/HDD coefficient. Network is ignored. Coefficients live in `internal/carbon/carbon.json`.
