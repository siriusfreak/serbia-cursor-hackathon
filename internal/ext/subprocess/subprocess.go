// Package subprocess runs a plugin in its own process.
//
// This is the payoff for keeping the ABI narrow. Because Extension moves only
// bytes, the same plugin code runs in-process or behind an RPC boundary with no
// changes: everything here is transport, and nothing here is domain.
//
// The wire format is net/rpc rather than gRPC. Our ABI is already
// (string, []byte) -> ([]byte, error), so protobuf would add a code generation
// step and buy nothing.
//
// Host side:  subprocess.LoadDir(registry, "./plugins", log)
// Plugin side: func main() { subprocess.Serve(myplugin.New()) }
package subprocess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/rpc"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	hclog "github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"

	"github.com/sirius/cogdebt/internal/ext"
)

// pluginKey names the single service a plugin binary serves.
const pluginKey = "extension"

// Handshake stops the host from talking to an unrelated executable, and pins
// the protocol to the ABI version. A plugin built against a different ABI fails
// here, before any of its code runs.
var Handshake = goplugin.HandshakeConfig{
	ProtocolVersion:  uint(ext.ABIVersion),
	MagicCookieKey:   "COGDEBT_PLUGIN",
	MagicCookieValue: "cogdebt-extension-v1",
}

// Serve runs e as a plugin process. Call it from a plugin binary's main; it
// blocks until the host disconnects.
func Serve(e ext.Extension) {
	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         goplugin.PluginSet{pluginKey: &extPlugin{impl: e}},
	})
}

// extPlugin adapts ext.Extension to go-plugin's two-sided interface.
type extPlugin struct{ impl ext.Extension }

func (p *extPlugin) Server(*goplugin.MuxBroker) (any, error) {
	return &rpcServer{impl: p.impl}, nil
}

func (p *extPlugin) Client(_ *goplugin.MuxBroker, c *rpc.Client) (any, error) {
	return &rpcClient{client: c}, nil
}

// InvokeArgs and InvokeReply are the wire types.
//
// They are exported because they have to be: net/rpc silently refuses to
// register a method whose argument types are unexported, so the service would
// load, answer Manifest (builtin types only) and fail on every real call.
//
// A Fault travels as data rather than as an error string, so the model still
// receives a message it can act on after the value has crossed a process
// boundary.
type InvokeArgs struct {
	Tool string
	In   []byte
}

type InvokeReply struct {
	Out   []byte
	Fault *ext.Fault
	Err   string
}

type rpcServer struct{ impl ext.Extension }

func (s *rpcServer) Manifest(_ any, reply *[]byte) error {
	raw, err := json.Marshal(s.impl.Manifest())
	if err != nil {
		return err
	}
	*reply = raw
	return nil
}

func (s *rpcServer) Invoke(args InvokeArgs, reply *InvokeReply) error {
	out, err := s.impl.Invoke(context.Background(), args.Tool, args.In)
	switch {
	case err == nil:
		reply.Out = out
	default:
		var f *ext.Fault
		if errors.As(err, &f) {
			reply.Fault = f
		} else {
			reply.Err = err.Error()
		}
	}
	return nil
}

func (s *rpcServer) Close(_ any, _ *struct{}) error { return s.impl.Close() }

// rpcClient is the host-side proxy. It satisfies ext.Extension, so the registry
// and the ADK adapter cannot tell it apart from an in-process plugin.
type rpcClient struct {
	client   *rpc.Client
	manifest *ext.Manifest
}

func (c *rpcClient) Manifest() ext.Manifest {
	// Manifest must stay cheap and stable: the host calls it every turn, and
	// each call here is a round trip. Fetch once, then serve from memory.
	if c.manifest != nil {
		return *c.manifest
	}
	var raw []byte
	if err := c.client.Call("Plugin.Manifest", new(any), &raw); err != nil {
		c.manifest = &ext.Manifest{}
		return *c.manifest
	}
	var m ext.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		m = ext.Manifest{}
	}
	c.manifest = &m
	return m
}

func (c *rpcClient) Invoke(_ context.Context, tool string, in json.RawMessage) (json.RawMessage, error) {
	var reply InvokeReply
	if err := c.client.Call("Plugin.Invoke", InvokeArgs{Tool: tool, In: in}, &reply); err != nil {
		// The transport broke, most likely the subprocess died. Say so in a way
		// the model can act on rather than retrying forever.
		return nil, &ext.Fault{
			Code:    ext.FaultUnavailable,
			Message: fmt.Sprintf("plugin process for %q is not responding", tool),
		}
	}
	switch {
	case reply.Fault != nil:
		return nil, reply.Fault
	case reply.Err != "":
		return nil, errors.New(reply.Err)
	}
	return reply.Out, nil
}

func (c *rpcClient) Close() error {
	return c.client.Call("Plugin.Close", new(any), &struct{}{})
}

// managed pairs the proxy with the subprocess that backs it, so closing the
// Extension also reaps the process.
type managed struct {
	ext.Extension
	client *goplugin.Client
}

func (m *managed) Close() error {
	err := m.Extension.Close()
	m.client.Kill()
	return err
}

// Load starts the plugin binary at path and returns it as an Extension.
func Load(path string) (ext.Extension, error) {
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig: Handshake,
		Plugins:         goplugin.PluginSet{pluginKey: &extPlugin{}},
		Cmd:             exec.Command(path),
		// go-plugin logs its handshake chatter at debug; silence it and let
		// the host's own logger report what matters.
		Logger: hclog.NewNullLogger(),
		Stderr: io.Discard,
	})

	conn, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("start %s: %w", filepath.Base(path), err)
	}
	raw, err := conn.Dispense(pluginKey)
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("dispense %s: %w", filepath.Base(path), err)
	}
	proxy, ok := raw.(ext.Extension)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("%s served an unexpected type %T", filepath.Base(path), raw)
	}
	return &managed{Extension: proxy, client: client}, nil
}

// LoadDir starts every executable in dir, registers it, and returns the plugin
// names it loaded.
//
// A missing directory is not an error: out-of-process plugins are optional, and
// the app must run with none present. A binary that fails to start is logged
// and skipped, exactly like a rejected in-process plugin.
func LoadDir(reg *ext.Registry, dir string, log *slog.Logger) []string {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warn("plugin directory unreadable", "dir", dir, "err", err)
		}
		return nil
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Mode()&0o111 == 0 {
			continue // not executable
		}
		names = append(names, e.Name())
	}
	sort.Strings(names) // deterministic load order

	var loaded []string
	for _, name := range names {
		path := filepath.Join(dir, name)
		e, err := Load(path)
		if err != nil {
			log.Error("out-of-process plugin not started", "path", path, "err", err)
			continue
		}
		if err := reg.Load(e); err != nil {
			log.Error("out-of-process plugin rejected", "path", path, "err", err)
			_ = e.Close()
			continue
		}
		log.Info("out-of-process plugin loaded", "path", path)
		loaded = append(loaded, e.Manifest().Name)
	}
	return loaded
}

var _ ext.Extension = (*rpcClient)(nil)
