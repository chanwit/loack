package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"loack/internal/state"
)

var stateCmd = &cobra.Command{
	Use:   "state",
	Short: "Inspect and manage loack's on-disk state",
}

var stateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the resources tracked in state",
	RunE:  stateListRun,
}

var stateShowCmd = &cobra.Command{
	Use:   "show <address>",
	Short: "Show the recorded object for one tracked resource",
	Args:  cobra.ExactArgs(1),
	RunE:  stateShowRun,
}

var stateRmCmd = &cobra.Command{
	Use:   "rm <address>",
	Short: "Remove a resource from state without destroying it in AWS",
	Args:  cobra.MinimumNArgs(1),
	RunE:  stateRmRun,
}

var stateMvCmd = &cobra.Command{
	Use:   "mv <old-address> <new-address>",
	Short: "Move a tracked resource to a new state address",
	Args:  cobra.ExactArgs(2),
	RunE:  stateMvRun,
}

func init() {
	stateCmd.AddCommand(stateListCmd, stateShowCmd, stateRmCmd, stateMvCmd)
	rootCmd.AddCommand(stateCmd)
}

func stateListRun(cmd *cobra.Command, args []string) error {
	st, err := state.Load(rootArgs.state)
	if err != nil {
		return err
	}
	resources := st.List()
	if len(resources) == 0 {
		outf("No resources tracked in %s.", rootArgs.state)
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ADDRESS\tREGION\tARN\tAPPLIED")
	for _, r := range resources {
		arn := r.ARN
		if arn == "" {
			arn = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Address, r.Region, arn, r.AppliedAt.Format("2006-01-02 15:04:05Z"))
	}
	return w.Flush()
}

func stateShowRun(cmd *cobra.Command, args []string) error {
	st, err := state.Load(rootArgs.state)
	if err != nil {
		return err
	}
	r, ok := st.Get(args[0])
	if !ok {
		return fmt.Errorf("no resource at address %q (see 'loack state list')", args[0])
	}
	if len(r.Object) == 0 {
		outf("# %s (no stored object)", r.Address)
		return nil
	}
	y, err := yaml.JSONToYAML(r.Object)
	if err != nil {
		return err
	}
	showYAML(r.Address, y)
	return nil
}

func stateRmRun(cmd *cobra.Command, args []string) error {
	st, err := state.Load(rootArgs.state)
	if err != nil {
		return err
	}
	removed := 0
	for _, addr := range args {
		if _, ok := st.Get(addr); !ok {
			errorf("no resource at address %q", addr)
			continue
		}
		st.Remove(addr)
		outf("Removed %s from state (the AWS resource is left untouched).", addr)
		removed++
	}
	if removed == 0 {
		return nil
	}
	return st.Save()
}

func stateMvRun(cmd *cobra.Command, args []string) error {
	st, err := state.Load(rootArgs.state)
	if err != nil {
		return err
	}
	src, dst := args[0], args[1]
	r, ok := st.Get(src)
	if !ok {
		return fmt.Errorf("no resource at address %q", src)
	}
	if _, exists := st.Get(dst); exists {
		return fmt.Errorf("destination address %q already exists", dst)
	}
	st.Remove(src)
	r.Address = dst
	st.Put(r)
	if err := st.Save(); err != nil {
		return err
	}
	outf("Moved %s to %s.", src, dst)
	return nil
}
