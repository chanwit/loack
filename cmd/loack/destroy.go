package main

import (
	"github.com/spf13/cobra"
)

var destroyArgs struct {
	autoApprove bool
}

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy all resources tracked in state",
	Long: `destroy tears down every resource recorded in state, rehydrated from the
stored objects (the manifests are not needed). It shows the plan and asks for
confirmation unless --auto-approve is given.`,
	Example: `  loack destroy
  loack destroy --auto-approve`,
	RunE: destroyCmdRun,
}

func init() {
	destroyCmd.Flags().BoolVar(&destroyArgs.autoApprove, "auto-approve", false, "skip the interactive approval prompt")
	rootCmd.AddCommand(destroyCmd)
}

func destroyCmdRun(cmd *cobra.Command, args []string) error {
	if err := requireInit(); err != nil {
		return err
	}
	changes, err := computePlan(cmd.Context(), true)
	if err != nil {
		return err
	}
	_, _, del := showPlan(changes)
	if del == 0 {
		return nil
	}

	if !destroyArgs.autoApprove {
		if !confirm("Do you really want to destroy all resources?") {
			blank()
			outf("Destroy cancelled.")
			return nil
		}
	}
	blank()

	_, _, d, err := executePlan(cmd.Context(), changes)
	if err != nil {
		return err
	}
	destroySummary(d)
	return nil
}
