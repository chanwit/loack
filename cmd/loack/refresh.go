package main

import (
	"github.com/spf13/cobra"

	"loack/provider"
	"loack/internal/state"
)

var refreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Update state to match the live AWS resources",
	Long: `refresh reads each tracked resource from AWS and updates state to match the
observed values. Resources that no longer exist in AWS are dropped from state.
No AWS resources are created, updated, or deleted. Kubernetes Secrets (local to
state) are left untouched.`,
	RunE: refreshCmdRun,
}

func init() {
	rootCmd.AddCommand(refreshCmd)
}

func refreshCmdRun(cmd *cobra.Command, args []string) error {
	if err := requireInit(); err != nil {
		return err
	}
	st, err := state.Load(rootArgs.state)
	if err != nil {
		return err
	}
	base, err := effectiveOptions()
	if err != nil {
		return err
	}

	updated, removed := 0, 0
	for _, r := range st.List() {
		if isK8sSecret(r.APIVersion, r.Kind) {
			continue
		}
		opts, oerr := targetOptions(base, r.Account, r.Region)
		if oerr != nil {
			errorf("%s: %v", r.Address, oerr)
			continue
		}
		prov, perr := providerFor(r.APIVersion)
		if perr != nil {
			errorf("%s: %v", r.Address, perr)
			continue
		}
		outf("%s.%s: Refreshing state...", r.Kind, r.Name)
		resp, rerr := prov.Call(cmd.Context(), provider.Request{
			Op: provider.OpRead, Object: r.Object, Options: opts,
		}, nil)
		if rerr != nil {
			errorf("%s: %v", r.Address, rerr)
			continue
		}
		if resp.Action == provider.ActAbsent {
			st.Remove(r.Address)
			removed++
			continue
		}
		r.Object = resp.Object
		if resp.ARN != "" {
			r.ARN = resp.ARN
		}
		st.Put(r)
		updated++
	}

	if err := st.Save(); err != nil {
		return err
	}
	blank()
	outf("Refresh complete. %d updated, %d removed from state.", updated, removed)
	return nil
}
