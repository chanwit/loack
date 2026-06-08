//go:build !split

package main

import (
	"os"

	_ "loack/internal/allcontrollers" // all-in-one (loack-aio) links every controller
	"loack/provider"
	"loack/provisioner"
)

// buildDispatcher wires the providers the core dispatches to.
//
// By default it uses the in-process (Local) provider, which links every ACK
// controller compiled into this binary. If $LOACK_PROVIDER points at a provider
// binary, the core instead spawns it and talks the provider protocol over a
// pipe -- the out-of-process path (step 3). A fully split build would replace
// this file to wire only remote providers and link no controllers at all.
func buildDispatcher() (*dispatcher, error) {
	d := &dispatcher{byGroup: map[string]provider.Provider{}}

	if bin := os.Getenv("LOACK_PROVIDER"); bin != "" {
		p, err := provider.NewRemote(bin)
		if err != nil {
			return nil, err
		}
		if err := d.register(p); err != nil {
			return nil, err
		}
		return d, nil
	}

	if err := d.register(provisioner.NewLocal()); err != nil {
		return nil, err
	}
	return d, nil
}
