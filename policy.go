// Package tofudrift holds files that live at the repo root and ship in the binary.
package tofudrift

import _ "embed"

// IAMPolicy is the read-only IAM policy a scan needs, from iam-policy.json.
//
//go:embed iam-policy.json
var IAMPolicy []byte
