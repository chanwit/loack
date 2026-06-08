# Third-party notices

loack includes source code adapted from third-party projects.

## HashiCorp go-plugin

- Project: https://github.com/hashicorp/go-plugin
- Copyright (c) HashiCorp, Inc.
- License: Mozilla Public License, version 2.0 (MPL-2.0) — https://www.mozilla.org/MPL/2.0/

`provider/handshake.go` adapts code from go-plugin's `server.go` and `client.go`
(v1.6.3): the magic-cookie guard, protocol-version negotiation, and the
pipe-delimited handshake line. The transport differs — loack speaks
JSON-over-stdio rather than go-plugin's gRPC/net-rpc over a negotiated socket —
and AutoMTLS is omitted. That file carries an in-file attribution header.

Per MPL-2.0, the adapted portions remain under MPL-2.0; a copy of the license is
available at the URL above.
