package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var planArgs struct {
	destroy bool
	out     string
}

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Show the changes apply would make to match the configuration",
	Long: `plan refreshes live state and compares it against the YAML manifests in the
working directory, printing the create/update/destroy actions apply would take.
With --out it writes a saved plan that 'apply <file>' can execute exactly.`,
	Example: `  loack plan
  loack plan --destroy
  loack plan --out            # saves to plan.loack
  loack plan --out my.loack`,
	RunE: planCmdRun,
}

// defaultPlanFile is the conventional saved-plan filename, used when --out is
// given without a value.
const defaultPlanFile = "plan.loack"

func init() {
	planCmd.Flags().BoolVar(&planArgs.destroy, "destroy", false, "plan to destroy everything in state")
	planCmd.Flags().StringVar(&planArgs.out, "out", "", `write the plan to this file for 'apply <file>' (default "`+defaultPlanFile+`")`)
	planCmd.Flags().Lookup("out").NoOptDefVal = defaultPlanFile
	rootCmd.AddCommand(planCmd)
}

func planCmdRun(cmd *cobra.Command, args []string) error {
	if err := requireInit(); err != nil {
		return err
	}
	changes, err := computePlan(cmd.Context(), planArgs.destroy)
	if err != nil {
		return err
	}
	add, chg, del := showPlan(changes)

	if planArgs.out == "" {
		return nil
	}
	if add == 0 && chg == 0 && del == 0 {
		blank()
		outf("No changes, so no plan was saved.")
		return nil
	}
	if err := savePlanFile(planArgs.out, changes); err != nil {
		return err
	}
	savedPlanHint(planArgs.out)
	return nil
}

// savedPlan is the on-disk form of a plan: the change set plus the AWS targeting
// resolved at plan time.
type savedPlan struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	Region    string    `json:"region"`
	Account   string    `json:"account"`
	Partition string    `json:"partition"`
	Changes   []change  `json:"changes"`
}

const savedPlanVersion = 3

func savePlanFile(path string, changes []change) error {
	opts, err := effectiveOptions()
	if err != nil {
		return err
	}
	sp := savedPlan{
		Version:   savedPlanVersion,
		CreatedAt: time.Now().UTC(),
		Region:    opts.Region,
		Account:   opts.Account,
		Partition: opts.Partition,
		Changes:   changes,
	}
	data, err := json.MarshalIndent(sp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func loadPlanFile(path string) (*savedPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading plan %s: %w", path, err)
	}
	var sp savedPlan
	if err := json.Unmarshal(data, &sp); err != nil {
		return nil, fmt.Errorf("parsing plan %s: %w", path, err)
	}
	if sp.Version != savedPlanVersion {
		return nil, fmt.Errorf("unsupported plan version %d (want %d)", sp.Version, savedPlanVersion)
	}
	return &sp, nil
}
