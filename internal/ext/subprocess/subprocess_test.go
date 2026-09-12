package subprocess

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/rpc"
	"testing"

	"github.com/sirius/cogdebt/internal/ext"
)

// fake is the plugin side of the boundary.
type fake struct{ closed bool }

func (f *fake) Manifest() ext.Manifest {
	return ext.Manifest{
		Name: "fake", Version: "1.0.0", ABIVersion: ext.ABIVersion, Kind: ext.KindRetrieval,
		Provides: []ext.ToolSpec{{
			Name: "echo", Description: "echoes its input",
			Schema: json.RawMessage(`{"type":"object"}`),
		}},
	}
}

func (f *fake) Invoke(_ context.Context, tool string, in json.RawMessage) (json.RawMessage, error) {
	switch tool {
	case "echo":
		return in, nil
	case "fault":
		return nil, &ext.Fault{Code: ext.FaultNotFound, Message: "call setup first", Retry: true}
	default:
		return nil, errors.New("plain breakage")
	}
}

func (f *fake) Close() error { f.closed = true; return nil }

// pair wires an rpcServer to an rpcClient over a real socket, exercising the
// actual encoding without spawning a process.
func pair(t *testing.T, impl ext.Extension) *rpcClient {
	t.Helper()

	srv := rpc.NewServer()
	if err := srv.RegisterName("Plugin", &rpcServer{impl: impl}); err != nil {
		t.Fatalf("register: %v", err)
	}
	a, b := net.Pipe()
	go srv.ServeConn(a)
	t.Cleanup(func() { a.Close(); b.Close() })

	return &rpcClient{client: rpc.NewClient(b)}
}

func TestManifestCrossesTheBoundary(t *testing.T) {
	c := pair(t, &fake{})
	m := c.Manifest()

	if m.Name != "fake" || len(m.Provides) != 1 {
		t.Fatalf("manifest did not survive the boundary: %+v", m)
	}
	// The host calls Manifest every turn; each call must not be a round trip.
	c.client.Close()
	if again := c.Manifest(); again.Name != "fake" {
		t.Fatal("Manifest is not cached; every turn would pay a round trip and break on a dead connection")
	}
}

func TestFaultSurvivesAsData(t *testing.T) {
	c := pair(t, &fake{})

	_, err := c.Invoke(t.Context(), "fault", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("want a fault, got success")
	}
	var f *ext.Fault
	if !errors.As(err, &f) {
		t.Fatalf("error %v (%T) is not a *ext.Fault; the model would lose the message it needs to correct itself", err, err)
	}
	if f.Code != ext.FaultNotFound || f.Message != "call setup first" || !f.Retry {
		t.Fatalf("fault was mangled in transit: %+v", f)
	}
}

func TestPayloadRoundTrip(t *testing.T) {
	c := pair(t, &fake{})

	// Multi-byte text is in the payload on purpose: learners write in their own
	// language, and a codec that mangles it would corrupt every question.
	in := json.RawMessage(`{"nested":{"list":[1,2,3],"unicode":"naïve · 破綻点 · ✅"}}`)
	out, err := c.Invoke(t.Context(), "echo", in)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if string(out) != string(in) {
		t.Fatalf("payload changed in transit:\n sent: %s\n got:  %s", in, out)
	}
}

func TestPlainErrorIsNotDisguisedAsFault(t *testing.T) {
	c := pair(t, &fake{})

	_, err := c.Invoke(t.Context(), "boom", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("want an error")
	}
	var f *ext.Fault
	if errors.As(err, &f) {
		t.Fatal("a plain error arrived as a Fault; genuine breakage must stay distinguishable from an expected failure")
	}
}

func TestDeadTransportReportsUnavailable(t *testing.T) {
	c := pair(t, &fake{})
	c.client.Close()

	_, err := c.Invoke(t.Context(), "echo", json.RawMessage(`{}`))
	var f *ext.Fault
	if !errors.As(err, &f) || f.Code != ext.FaultUnavailable {
		t.Fatalf("a dead plugin process must surface as an unavailable Fault, got %v", err)
	}
}

func TestHandshakePinsTheABIVersion(t *testing.T) {
	// A plugin built against a different ABI must fail at the handshake, before
	// any of its code runs.
	if Handshake.ProtocolVersion != uint(ext.ABIVersion) {
		t.Fatalf("handshake protocol %d is not pinned to ABIVersion %d", Handshake.ProtocolVersion, ext.ABIVersion)
	}
}
