package mcp

import (
	"encoding/json"
	"testing"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/jsonrpc"
)

func newTestDispatcher() (*jsonrpc.Dispatcher, *Lifecycle) {
	d := jsonrpc.NewDispatcher()
	l := NewLifecycle(ServerInfo{Name: "test-server", Version: "0.0.0-test"})
	l.Register(d)
	return d, l
}

func TestInitialize_Success(t *testing.T) {
	d, l := newTestDispatcher()

	params, _ := json.Marshal(InitializeParams{
		ProtocolVersion: ProtocolVersion,
		ClientInfo:      ServerInfo{Name: "test-client", Version: "1.0.0"},
	})
	req := jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		ID:      jsonrpc.NewStringID("init-1"),
		Method:  "initialize",
		Params:  params,
	}

	resp := d.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("esperaba éxito, obtuve error: %v", resp.Error)
	}

	var result InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("no se pudo decodificar InitializeResult: %v", err)
	}
	if result.ProtocolVersion != ProtocolVersion {
		t.Errorf("versión esperada %q, obtuve %q", ProtocolVersion, result.ProtocolVersion)
	}
	if result.ServerInfo.Name != "test-server" {
		t.Errorf("serverInfo.name inesperado: %q", result.ServerInfo.Name)
	}

	// Antes de notifications/initialized, el ciclo de vida aún no está "Ready".
	if l.Ready() {
		t.Error("Lifecycle no debería estar Ready sin notifications/initialized")
	}
}

func TestInitialize_NegotiatesUnsupportedVersion(t *testing.T) {
	d, _ := newTestDispatcher()

	params, _ := json.Marshal(InitializeParams{ProtocolVersion: "2024-01-01"})
	req := jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		ID:      jsonrpc.NewIntID(1),
		Method:  "initialize",
		Params:  params,
	}

	// La especificación 2025-06-18 NO permite fallar acá: el servidor debe responder con una
	// versión que sí soporte y dejar que el cliente decida si continúa o corta.
	resp := d.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("no esperaba error, la versión se negocia: %v", resp.Error)
	}

	body, _ := json.Marshal(resp.Result)
	var result InitializeResult
	json.Unmarshal(body, &result)

	if result.ProtocolVersion != ProtocolVersion {
		t.Errorf("esperaba que el servidor ofreciera %q, obtuve %q", ProtocolVersion, result.ProtocolVersion)
	}
}

func TestInitialize_EchoesASupportedVersion(t *testing.T) {
	d, _ := newTestDispatcher()
	params, _ := json.Marshal(InitializeParams{ProtocolVersion: ProtocolVersion})
	req := jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		ID:      jsonrpc.NewIntID(1),
		Method:  "initialize",
		Params:  params,
	}

	resp := d.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("no esperaba error: %v", resp.Error)
	}

	body, _ := json.Marshal(resp.Result)
	var result InitializeResult
	json.Unmarshal(body, &result)

	if result.ProtocolVersion != ProtocolVersion {
		t.Errorf("esperaba %q, obtuve %q", ProtocolVersion, result.ProtocolVersion)
	}
}

func TestInitialize_RejectsMissingProtocolVersion(t *testing.T) {
	d, _ := newTestDispatcher()

	req := jsonrpc.Request{
		JSONRPC: jsonrpc.Version,
		ID:      jsonrpc.NewIntID(1),
		Method:  "initialize",
		Params:  json.RawMessage(`{}`),
	}

	resp := d.Dispatch(req)
	if resp.Error == nil || resp.Error.Code != jsonrpc.CodeInvalidParams {
		t.Fatalf("esperaba InvalidParams, obtuve %+v", resp.Error)
	}
}

func TestFullLifecycle_InitializeThenInitializedThenReady(t *testing.T) {
	d, l := newTestDispatcher()

	params, _ := json.Marshal(InitializeParams{ProtocolVersion: ProtocolVersion})
	d.Dispatch(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, ID: jsonrpc.NewIntID(1), Method: "initialize", Params: params,
	})

	if err := d.DispatchNotification(jsonrpc.Notification{
		JSONRPC: jsonrpc.Version, Method: "notifications/initialized",
	}); err != nil {
		t.Fatalf("no esperaba error: %v", err)
	}

	if !l.Ready() {
		t.Error("Lifecycle debería estar Ready tras initialize + notifications/initialized")
	}
}

func TestPing_DoesNotRequireInitialize(t *testing.T) {
	d, _ := newTestDispatcher()

	resp := d.Dispatch(jsonrpc.Request{
		JSONRPC: jsonrpc.Version, ID: jsonrpc.NewStringID("p1"), Method: "ping",
	})
	if resp.Error != nil {
		t.Fatalf("ping no debería fallar antes de initialize, obtuve: %v", resp.Error)
	}
}
