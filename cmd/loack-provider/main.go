// Command loack-provider is a loack provider binary: it serves the provider
// protocol (JSON over stdin/stdout) backed by the ACK controllers linked into
// it. This build links all wired controllers; a per-service provider would link
// only its own (see PORT_GUIDELINE / the provider split design).
//
// It is launched by the loack core, not run directly by users.
package main

import (
	"fmt"
	"os"

	_ "loack/internal/allcontrollers" // register every wired controller
	"loack/provider"
	"loack/provisioner"
)

func main() {
	if err := provider.Serve(provisioner.NewLocal()); err != nil {
		fmt.Fprintln(os.Stderr, "loack-provider:", err)
		os.Exit(1)
	}
}
