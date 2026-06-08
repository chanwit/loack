package main

import (
	"context"
	"os"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/spf13/cobra"

	"loack/internal/state"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize the working directory",
	Long: `init prepares the current directory for loack: it creates the .loack working
directory (holding state, backups, and config), records the resolved AWS
targeting, and verifies credentials. Safe to re-run.`,
	RunE: initCmdRun,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func initCmdRun(cmd *cobra.Command, args []string) error {
	if err := os.MkdirAll(state.WorkDir, 0o755); err != nil {
		return err
	}

	// Verify credentials and resolve region/account.
	ctx := context.Background()
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(rootArgs.region))
	if err != nil {
		return err
	}
	region := rootArgs.region
	if region == "" {
		region = awsCfg.Region
	}
	account := rootArgs.account
	if account == "" {
		if out, ierr := sts.NewFromConfig(awsCfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{}); ierr == nil {
			account = *out.Account
		}
	}

	cfg := workspaceConfig{Region: region, Account: account, Partition: rootArgs.partition}
	if err := cfg.save(); err != nil {
		return err
	}

	// Ensure an (empty) state file exists.
	st, err := state.Load(rootArgs.state)
	if err != nil {
		return err
	}
	if err := st.Save(); err != nil {
		return err
	}

	outf("%s loack has been initialized in %s/", green("✓"), state.WorkDir)
	blank()
	if account != "" {
		outf("  account:   %s", account)
	}
	if region != "" {
		outf("  region:    %s", region)
	}
	outf("  state:     %s", rootArgs.state)
	if disp, derr := providers(); derr == nil {
		outf("  providers: %s", disp.summaryLine())
	}
	blank()
	outf("Put your resource manifests (*.yaml) in this directory, then run 'loack plan'.")
	return nil
}
