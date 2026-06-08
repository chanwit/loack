package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var applyArgs struct {
	autoApprove bool
}

var applyCmd = &cobra.Command{
	Use:   "apply [saved-plan]",
	Short: "Reconcile AWS to match the configuration in this directory",
	Long: `apply reconciles AWS to match the YAML manifests in the working directory:
resources in the config are created or updated, and resources in state but no
longer in the config are destroyed.

By default apply shows the plan and asks for confirmation. Pass a saved plan
(from 'plan --out') to execute exactly that plan without prompting.`,
	Example: `  loack apply
  loack apply --auto-approve
  loack plan --out && loack apply plan.loack`,
	Args: cobra.MaximumNArgs(1),
	RunE: applyCmdRun,
}

func init() {
	applyCmd.Flags().BoolVar(&applyArgs.autoApprove, "auto-approve", false, "skip the interactive approval prompt")
	rootCmd.AddCommand(applyCmd)
}

func applyCmdRun(cmd *cobra.Command, args []string) error {
	if err := requireInit(); err != nil {
		return err
	}

	// Saved plan: execute exactly, no prompt.
	if len(args) == 1 {
		sp, err := loadPlanFile(args[0])
		if err != nil {
			return err
		}
		a, c, d, err := executePlan(cmd.Context(), sp.Changes)
		if err != nil {
			return err
		}
		applySummary(a, c, d)
		return nil
	}

	changes, err := computePlan(cmd.Context(), false)
	if err != nil {
		return err
	}
	add, chg, del := showPlan(changes)
	if add == 0 && chg == 0 && del == 0 {
		applySummary(0, 0, 0)
		return nil
	}

	if !applyArgs.autoApprove {
		if !confirm("Do you want to perform these actions?") {
			blank()
			outf("Apply cancelled.")
			return nil
		}
	}
	blank()

	a, c, d, err := executePlan(cmd.Context(), changes)
	if err != nil {
		return err
	}
	applySummary(a, c, d)
	return nil
}

var _ = fmt.Sprint
