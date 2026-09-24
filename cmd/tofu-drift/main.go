// Command tofu-drift reports drift on OpenTofu/Terraform-managed AWS resources
// and finds unmanaged or idle ones, with a monthly cost and carbon estimate.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Exit codes: 0 no findings, 1 findings present, 2 error.
const (
	exitClean    = 0
	exitFindings = 1
	exitError    = 2
)

// errFindings signals a successful scan that produced findings.
var errFindings = errors.New("findings present")

// version is set by goreleaser at release time.
var version = "dev"

func main() {
	root := &cobra.Command{
		Use:           "tofu-drift",
		Version:       version,
		Short:         "Find drift, unmanaged and idle AWS resources in an OpenTofu/Terraform state",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(scanCmd(), unmanagedCmd(), importGenCmd())

	err := root.Execute()
	switch {
	case err == nil:
		os.Exit(exitClean)
	case errors.Is(err, errFindings):
		os.Exit(exitFindings)
	default:
		fmt.Fprintln(os.Stderr, "tofu-drift:", err)
		os.Exit(exitError)
	}
}
