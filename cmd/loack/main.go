// Command loack is a controller-less provisioner for ACK resources.
//
// It reads a Kubernetes-style ACK custom resource (KRM) and drives the ACK
// controller's own generated resource manager directly against the AWS API to
// perform a single, one-off reconcile -- like "terraform apply", but the
// desired state is an ACK CR and the provider logic is the controller's
// generated sdk.go code. No cluster and no controller pod are involved.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"loack/internal/state"
)

// version is stamped at build time via -ldflags (see Makefile).
var version = "dev"

var rootCmd = &cobra.Command{
	Use:     "loack",
	Version: version,
	Short: "Controller-less provisioner for ACK (AWS Controllers for Kubernetes) resources",
	Long: `loack provisions cloud resources from ACK custom resources, Terraform-style.

The YAML manifests in the working directory are the desired configuration. Run
'loack init' once, then 'loack plan' / 'loack apply' / 'loack destroy'. loack
reuses each ACK controller's generated resource manager to reconcile against the
AWS API -- no controller, no Kubernetes cluster.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// rootFlags are the AWS targeting options shared by every subcommand.
type rootFlags struct {
	region    string
	account   string
	partition string
	state     string
}

var rootArgs rootFlags

func init() {
	rootCmd.PersistentFlags().StringVar(&rootArgs.region, "region", os.Getenv("AWS_REGION"),
		"AWS region to target (defaults to $AWS_REGION)")
	rootCmd.PersistentFlags().StringVar(&rootArgs.account, "account", "",
		"AWS account ID (looked up via STS when empty)")
	rootCmd.PersistentFlags().StringVar(&rootArgs.partition, "partition", "aws",
		"AWS partition")
	rootCmd.PersistentFlags().StringVar(&rootArgs.state, "state", state.DefaultPath,
		"path to the loack state file")
}

// stateRefs returns a snapshot of every recorded object keyed by address. The
// core passes this to a provider so its generated ResolveReferences can resolve
// "...Ref" fields without a callback or a cluster.
func stateRefs() (map[string]json.RawMessage, error) {
	st, err := state.Load(rootArgs.state)
	if err != nil {
		return nil, err
	}
	refs := map[string]json.RawMessage{}
	for _, r := range st.List() {
		if len(r.Object) > 0 {
			refs[r.Address] = r.Object
		}
	}
	return refs, nil
}

// secretStoreFromState builds the secret map that resolves ACK
// SecretKeyReferences from native Kubernetes Secrets applied into loack state.
// Each Secret contributes "<namespace>/<name>/<key>" and "<name>/<key>".
func secretStoreFromState() (map[string]string, error) {
	st, err := state.Load(rootArgs.state)
	if err != nil {
		return nil, err
	}
	store := map[string]string{}
	for _, r := range st.List() {
		if !isK8sSecret(r.APIVersion, r.Kind) {
			continue
		}
		ns, name, values, perr := parseK8sSecret(r.Object)
		if perr != nil {
			return nil, fmt.Errorf("state secret %s: %w", r.Address, perr)
		}
		for k, v := range values {
			store[ns+"/"+name+"/"+k] = v
			store[name+"/"+k] = v
		}
	}
	return store, nil
}

// recordResource upserts an applied resource into state.
func recordResource(apiVersion, kind, namespace, name, region, account, arn string, object []byte) error {
	st, err := state.Load(rootArgs.state)
	if err != nil {
		return err
	}
	st.Put(state.Resource{
		APIVersion: apiVersion,
		Kind:       kind,
		Name:       name,
		Namespace:  namespace,
		Region:     region,
		Account:    account,
		ARN:        arn,
		AppliedAt:  time.Now().UTC(),
		Object:     object,
	})
	return st.Save()
}

// forgetResource removes a resource from state after it is deleted.
func forgetResource(apiVersion, kind, namespace, name string) error {
	st, err := state.Load(rootArgs.state)
	if err != nil {
		return err
	}
	addr := state.Address(apiVersion, kind, namespace, name)
	if _, ok := st.Get(addr); !ok {
		return nil
	}
	st.Remove(addr)
	return st.Save()
}

func main() {
	err := rootCmd.Execute()
	if theDispatcher != nil {
		theDispatcher.close() // shut down any provider subprocesses
	}
	if err != nil {
		errorf("%s", err)
		os.Exit(1)
	}
}
