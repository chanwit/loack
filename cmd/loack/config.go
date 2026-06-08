package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"loack/provider"
	"loack/internal/state"
)

// workspaceConfig is persisted by `loack init` at .loack/config.json and records
// the working directory's defaults. Flags and environment variables override it.
type workspaceConfig struct {
	Region    string `json:"region,omitempty"`
	Account   string `json:"account,omitempty"`
	Partition string `json:"partition,omitempty"`
	// RoleAccountMap maps an AWS account ID to the IAM role ARN loack should
	// assume to provision into that account -- loack's analogue of ACK's
	// `ack-role-account-map`. Used for per-namespace (CARM) account targeting.
	RoleAccountMap map[string]string `json:"roleAccountMap,omitempty"`
}

func configPath() string { return filepath.Join(state.WorkDir, "config.json") }

func loadWorkspaceConfig() workspaceConfig {
	var c workspaceConfig
	if data, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(data, &c)
	}
	return c
}

func (c workspaceConfig) save() error {
	if err := os.MkdirAll(state.WorkDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), append(data, '\n'), 0o644)
}

// effectiveOptions resolves AWS targeting from flags > env (already folded into
// rootArgs defaults) > .loack/config.json, attaching the state-backed secret
// store so references and secrets resolve.
func effectiveOptions() (provider.Options, error) {
	cfg := loadWorkspaceConfig()
	region := rootArgs.region
	if region == "" {
		region = cfg.Region
	}
	account := rootArgs.account
	if account == "" {
		account = cfg.Account
	}
	partition := rootArgs.partition
	if partition == "" {
		partition = cfg.Partition
	}
	if partition == "" {
		partition = "aws"
	}
	secrets, err := secretStoreFromState()
	if err != nil {
		return provider.Options{}, err
	}
	return provider.Options{Region: region, Account: account, Partition: partition, Secrets: secrets}, nil
}

// targetOptions applies a resource's declared account/region (from a namespace's
// CARM annotations at plan/apply time, or from state at destroy time) on top of
// the workspace defaults. Targeting a non-default account requires a role to
// assume (CARM): it is looked up in roleAccountMap, and it is an error if absent.
func targetOptions(base provider.Options, account, region string) (provider.Options, error) {
	opts := base
	if region != "" {
		opts.Region = region
	}
	if account == "" || account == base.Account {
		return opts, nil // ambient credentials
	}
	cfg := loadWorkspaceConfig()
	roleARN := cfg.RoleAccountMap[account]
	if roleARN == "" {
		return opts, fmt.Errorf(
			"resource targets AWS account %s, but no role is mapped for it; "+
				"add roleAccountMap[%q] = <role-arn> to %s",
			account, account, configPath())
	}
	opts.Account = account
	opts.RoleARN = roleARN
	return opts, nil
}

// initialized reports whether `loack init` has been run in this directory.
func initialized() bool {
	_, err := os.Stat(state.WorkDir)
	return err == nil
}

func requireInit() error {
	if !initialized() {
		return fmt.Errorf("this directory is not initialized; run 'loack init' first")
	}
	return nil
}
