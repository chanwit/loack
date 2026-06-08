package provider

// Plugin handshake: magic-cookie verification and protocol-version negotiation.
//
// Portions of this file are adapted from HashiCorp go-plugin (server.go and
// client.go), Copyright (c) HashiCorp, Inc., licensed under the Mozilla Public
// License v2.0: https://github.com/hashicorp/go-plugin
//
// What is ported: the magic-cookie guard (so a provider binary run directly
// prints guidance instead of hanging), the protocol-version negotiation, and
// the pipe-delimited handshake line "core|app|network|address|protocol". What
// differs: loack's transport is JSON-over-stdio, so the handshake line is read
// from the same stdout stream the data then flows over (network/address are the
// fixed sentinels "stdio"/"-"); go-plugin instead negotiates a socket and runs
// gRPC/net-rpc over it. AutoMTLS is intentionally omitted -- inherited stdio
// pipes are already private to parent and child, which is the threat AutoMTLS
// addresses for go-plugin's default TCP listener.

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	// coreProtocolVersion is the version of the plugin handshake/framing itself,
	// independent of the app schema. Bump only if the handshake line or framing
	// changes. (go-plugin: CoreProtocolVersion.)
	coreProtocolVersion = 1

	// appProtocolVersion is the version of loack's provider Request/Response
	// schema this build speaks. Bump on an incompatible protocol change so a
	// mismatched core/provider pair fails the handshake instead of misbehaving.
	appProtocolVersion = 1

	// magicCookieKey/Value guard against a provider binary being executed
	// directly: the core sets the env var, the provider checks it. (go-plugin:
	// MagicCookieKey/MagicCookieValue.)
	magicCookieKey   = "LOACK_PLUGIN_MAGIC_COOKIE"
	magicCookieValue = "loack-provider-handshake-3f2a1b7c9d6e5081-mpl2"

	// protocolVersionsEnv carries the core's supported app versions (CSV) to the
	// provider for negotiation. (go-plugin: PLUGIN_PROTOCOL_VERSIONS.)
	protocolVersionsEnv = "LOACK_PLUGIN_PROTOCOL_VERSIONS"
)

// pluginEnv is the environment a provider subprocess must be launched with: the
// magic cookie and the app protocol versions the core supports. Appended to the
// inherited environment so AWS credentials still pass through. (Adapted from
// go-plugin client.go.)
func pluginEnv() []string {
	return []string{
		magicCookieKey + "=" + magicCookieValue,
		protocolVersionsEnv + "=" + strconv.Itoa(appProtocolVersion),
	}
}

// serverHandshake runs the provider (server) side on stdio: verify the magic
// cookie, negotiate the app version against the core's list, and emit the
// handshake line on stdout. On the "run directly" case it prints go-plugin's
// guidance and exits 1 (this binary IS the plugin's entry point). Returns the
// negotiated app version. (Adapted from go-plugin server.go.)
func serverHandshake() int {
	if os.Getenv(magicCookieKey) != magicCookieValue {
		fmt.Fprint(os.Stderr,
			"This binary is a plugin. These are not meant to be executed directly.\n"+
				"Please execute the program that consumes these plugins, which will\n"+
				"load any plugins automatically\n")
		os.Exit(1)
	}

	negotiated := appProtocolVersion
	if list := os.Getenv(protocolVersionsEnv); list != "" {
		if negotiated = negotiateVersion(list); negotiated == 0 {
			fmt.Fprintf(os.Stderr,
				"Incompatible API version with plugin. Core versions: %s, plugin version: %d\n",
				list, appProtocolVersion)
			os.Exit(1)
		}
	}

	// handshake line: core|app|network|address|protocol (stdio transport).
	fmt.Printf("%d|%d|stdio|-|json\n", coreProtocolVersion, negotiated)
	_ = os.Stdout.Sync()
	return negotiated
}

// negotiateVersion returns appProtocolVersion if the core's CSV list of
// supported versions includes it, else 0. loack providers speak exactly one app
// version, so negotiation is membership rather than max-of-set.
func negotiateVersion(csv string) int {
	for _, s := range strings.Split(csv, ",") {
		if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && v == appProtocolVersion {
			return v
		}
	}
	return 0
}

// clientHandshake runs the core (client) side: read one handshake line from the
// provider's stdout via r (which is then reused for the data stream, so no bytes
// are lost), validate the core protocol version, and negotiate the app version.
// Returns the negotiated app version. (Adapted from go-plugin client.go.)
func clientHandshake(r *bufio.Reader) (int, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return 0, fmt.Errorf("reading provider handshake: %w", err)
	}
	parts := strings.Split(strings.TrimSpace(line), "|")
	if len(parts) < 4 {
		return 0, fmt.Errorf("unrecognized provider handshake %q (not a loack provider, or wrong version)", strings.TrimSpace(line))
	}

	core, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("parsing provider core protocol version: %w", err)
	}
	if core != coreProtocolVersion {
		return 0, fmt.Errorf("incompatible core plugin protocol: provider %d, core %d (rebuild the provider)", core, coreProtocolVersion)
	}

	app, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("parsing provider app protocol version: %w", err)
	}
	if app != appProtocolVersion {
		return 0, fmt.Errorf("incompatible provider API version: provider %d, core %d (rebuild the provider)", app, appProtocolVersion)
	}
	return app, nil
}
