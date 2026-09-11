package main

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"

	"github.com/elmissouri16/snow-core/internal/plugin/javascript"
	"github.com/spf13/cobra"
)

func pluginTestCmd() *cobra.Command {
	var fixturePath string
	cmd := &cobra.Command{Use: "test <path> --fixtures <file>", Short: "Test a local plugin in Goja using only declared mock host calls", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if disabled, _ := cmd.Flags().GetBool("no-plugins"); disabled {
			return errors.New("plugin test cannot run with --no-plugins")
		}
		if fixturePath == "" {
			return errors.New("plugin test requires --fixtures <file>")
		}
		suite, err := javascript.ReadFixturesFile(cmd.Context(), fixturePath)
		if err != nil {
			return err
		}
		p, err := javascript.ReadPackage(cmd.Context(), args[0], "", nil)
		if err != nil {
			return err
		}
		report, runErr := javascript.RunFixtures(cmd.Context(), p, suite)
		if jsonRequested(cmd) {
			if err := jsonv2.MarshalWrite(cmd.OutOrStdout(), report); err != nil {
				return err
			}
		} else {
			for _, test := range report.Tests {
				status := "PASS"
				if !test.Passed {
					status = "FAIL"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", status, test.Name)
				if test.Error != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", test.Error)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%d passed, %d failed (mock host only)\n", report.Passed, report.Failed)
		}
		if runErr != nil {
			return runErr
		}
		if report.Failed > 0 {
			return fmt.Errorf("%d plugin fixture tests failed", report.Failed)
		}
		return nil
	}}
	cmd.Flags().StringVar(&fixturePath, "fixtures", "", "bounded JSON fixture file (required)")
	cmd.Flags().Bool("json", false, "output a machine-readable test report")
	return cmd
}
