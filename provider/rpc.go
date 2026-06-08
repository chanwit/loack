package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// The wire protocol is a stream of JSON values. The core sends a Request; the
// provider replies with zero or more {type:"event"} messages (progress) followed
// by exactly one {type:"response"} message.

type message struct {
	Type     string    `json:"type"`
	Event    *Event    `json:"event,omitempty"`
	Response *Response `json:"response,omitempty"`
}

// Serve runs the provider protocol over stdin/stdout until stdin closes. A
// provider binary's main calls this with a Local provider. It first performs the
// plugin handshake (magic cookie + version negotiation), so a provider launched
// without the cookie -- e.g. run directly -- prints guidance and exits.
func Serve(p Provider) error {
	serverHandshake()
	dec := json.NewDecoder(bufio.NewReaderSize(os.Stdin, 1<<20))
	enc := json.NewEncoder(os.Stdout)
	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		hook := func(e Event) { _ = enc.Encode(message{Type: "event", Event: &e}) }
		resp, err := p.Call(context.Background(), req, hook)
		if err != nil {
			resp = Response{Err: err.Error()}
		}
		if err := enc.Encode(message{Type: "response", Response: &resp}); err != nil {
			return err
		}
	}
}

// Remote is a Provider backed by a separate provider binary, spoken to over its
// stdin/stdout. One request is in flight at a time (the core is sequential).
type Remote struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	enc   *json.Encoder
	dec   *json.Decoder
}

// NewRemote launches a provider binary and returns a client for it. The
// provider inherits the environment (AWS credentials) plus the plugin handshake
// vars, and writes its logs to the parent's stderr. NewRemote completes the
// handshake before returning, so a version/identity mismatch fails here rather
// than on the first request.
func NewRemote(path string, args ...string) (*Remote, error) {
	cmd := exec.Command(path, args...)
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), pluginEnv()...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting provider %s: %w", path, err)
	}

	// Read the handshake line from stdout, then reuse the SAME buffered reader
	// for the data stream so any bytes buffered after the line aren't lost.
	br := bufio.NewReaderSize(stdout, 1<<20)
	if _, err := clientHandshake(br); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("provider %s: %w", path, err)
	}

	return &Remote{
		cmd:   cmd,
		stdin: stdin,
		enc:   json.NewEncoder(stdin),
		dec:   json.NewDecoder(br),
	}, nil
}

func (r *Remote) Call(_ context.Context, req Request, hook Hook) (Response, error) {
	if err := r.enc.Encode(req); err != nil {
		return Response{}, fmt.Errorf("provider write: %w", err)
	}
	for {
		var msg message
		if err := r.dec.Decode(&msg); err != nil {
			return Response{}, fmt.Errorf("provider read: %w", err)
		}
		switch msg.Type {
		case "event":
			if hook != nil && msg.Event != nil {
				hook(*msg.Event)
			}
		case "response":
			if msg.Response == nil {
				return Response{}, errors.New("provider: empty response")
			}
			if msg.Response.Err != "" {
				return *msg.Response, errors.New(msg.Response.Err)
			}
			return *msg.Response, nil
		default:
			return Response{}, fmt.Errorf("provider: unexpected message %q", msg.Type)
		}
	}
}

// Close shuts the provider down gracefully: closing stdin makes its Serve loop
// hit EOF and exit. If it doesn't exit promptly, kill it. (go-plugin does the
// same graceful-then-kill.)
func (r *Remote) Close() error {
	_ = r.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- r.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		_ = r.cmd.Process.Kill()
		return <-done
	}
}
