package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wardbox/tofu-drift/internal/report"
	"github.com/wardbox/tofu-drift/internal/state"
)

func scanCmd() *cobra.Command {
	var statePath string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Report drift, unmanaged and idle resources",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if statePath == "" {
				return fmt.Errorf("--state is required (auto-detection lands in a later release)")
			}
			f, err := os.Open(statePath)
			if err != nil {
				return err
			}
			defer f.Close()
			managed, err := state.Parse(f)
			if err != nil {
				return err
			}
			r := report.New(report.Scan{StateSource: statePath}, managed)
			out := cmd.OutOrStdout()
			if asJSON {
				err = r.WriteJSON(out)
			} else {
				err = r.WriteTable(out)
			}
			if err != nil {
				return err
			}
			if len(r.Findings) > 0 {
				return errFindings
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&statePath, "state", "", "path to a state file")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a table")
	return cmd
}
