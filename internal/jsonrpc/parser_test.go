package jsonrpc

import "testing"

func TestParseMessage_RequestWithStringID(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":"abc-1","method":"ping","params":{}}`)

	parsed, errObj := ParseMessage(raw)
	if errObj != nil {
		t.Fatalf("esperaba parseo exitoso, obtuve error: %v", errObj)
	}
	if parsed.Kind != KindRequest {
		t.Fatalf("esperaba KindRequest, obtuve %v", parsed.Kind)
	}
	if parsed.Request.Method != "ping" {
		t.Errorf("method esperado 'ping', obtuve %q", parsed.Request.Method)
	}
	if parsed.Request.ID.String() != "abc-1" {
		t.Errorf("id esperado 'abc-1', obtuve %q", parsed.Request.ID.String())
	}
}

func TestParseMessage_RequestWithIntID(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":42,"method":"tools/list"}`)

	parsed, errObj := ParseMessage(raw)
	if errObj != nil {
		t.Fatalf("esperaba parseo exitoso, obtuve error: %v", errObj)
	}
	if parsed.Request.ID.String() != "42" {
		t.Errorf("id esperado '42', obtuve %q", parsed.Request.ID.String())
	}
}

func TestParseMessage_Notification(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	parsed, errObj := ParseMessage(raw)
	if errObj != nil {
		t.Fatalf("esperaba parseo exitoso, obtuve error: %v", errObj)
	}
	if parsed.Kind != KindNotification {
		t.Fatalf("esperaba KindNotification, obtuve %v", parsed.Kind)
	}
	if parsed.Notification.Method != "notifications/initialized" {
		t.Errorf("method inesperado: %q", parsed.Notification.Method)
	}
}

func TestParseMessage_Response(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":"abc-1","result":{"ok":true}}`)

	parsed, errObj := ParseMessage(raw)
	if errObj != nil {
		t.Fatalf("esperaba parseo exitoso, obtuve error: %v", errObj)
	}
	if parsed.Kind != KindResponse {
		t.Fatalf("esperaba KindResponse, obtuve %v", parsed.Kind)
	}
}

func TestParseMessage_InvalidJSON(t *testing.T) {
	raw := []byte(`{not valid json`)

	_, errObj := ParseMessage(raw)
	if errObj == nil {
		t.Fatal("esperaba error de parseo, obtuve nil")
	}
	if errObj.Code != CodeParseError {
		t.Errorf("código esperado %d, obtuve %d", CodeParseError, errObj.Code)
	}
}

func TestParseMessage_WrongVersion(t *testing.T) {
	raw := []byte(`{"jsonrpc":"1.0","id":1,"method":"ping"}`)

	_, errObj := ParseMessage(raw)
	if errObj == nil {
		t.Fatal("esperaba error de invalid request, obtuve nil")
	}
	if errObj.Code != CodeInvalidRequest {
		t.Errorf("código esperado %d, obtuve %d", CodeInvalidRequest, errObj.Code)
	}
}

func TestParseMessage_MalformedEnvelope(t *testing.T) {
	// Ni method, ni result, ni error: no es request, notification ni response.
	raw := []byte(`{"jsonrpc":"2.0","id":1}`)

	_, errObj := ParseMessage(raw)
	if errObj == nil {
		t.Fatal("esperaba error de invalid request, obtuve nil")
	}
	if errObj.Code != CodeInvalidRequest {
		t.Errorf("código esperado %d, obtuve %d", CodeInvalidRequest, errObj.Code)
	}
}

func TestID_RoundTrip_PreservesType(t *testing.T) {
	stringID := NewStringID("session-42")
	data, err := stringID.MarshalJSON()
	if err != nil {
		t.Fatalf("error serializando id string: %v", err)
	}
	if string(data) != `"session-42"` {
		t.Errorf("esperaba id string entre comillas, obtuve %s", data)
	}

	intID := NewIntID(7)
	data, err = intID.MarshalJSON()
	if err != nil {
		t.Fatalf("error serializando id numérico: %v", err)
	}
	if string(data) != `7` {
		t.Errorf("esperaba id numérico sin comillas, obtuve %s", data)
	}
}
