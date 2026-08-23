package jsonrpc

import (
	"encoding/json"
	"testing"
)

func TestDispatcher_MethodNotFound(t *testing.T) {
	d := NewDispatcher()
	req := Request{JSONRPC: Version, ID: NewStringID("1"), Method: "unknown/method"}

	resp := d.Dispatch(req)

	if resp.Error == nil {
		t.Fatal("esperaba error, obtuve respuesta exitosa")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("código esperado %d, obtuve %d", CodeMethodNotFound, resp.Error.Code)
	}
	if !resp.ID.Equal(req.ID) {
		t.Errorf("la respuesta debe conservar el mismo id que la solicitud")
	}
}

func TestDispatcher_SuccessfulCall(t *testing.T) {
	d := NewDispatcher()
	d.RegisterMethod("echo", func(params json.RawMessage) (any, *ErrorObject) {
		var p struct {
			Msg string `json:"msg"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, InvalidParams(err.Error())
		}
		return map[string]string{"echo": p.Msg}, nil
	})

	req := Request{
		JSONRPC: Version,
		ID:      NewIntID(1),
		Method:  "echo",
		Params:  json.RawMessage(`{"msg":"hola"}`),
	}

	resp := d.Dispatch(req)

	if resp.Error != nil {
		t.Fatalf("esperaba éxito, obtuve error: %v", resp.Error)
	}
	var result map[string]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("no se pudo decodificar el resultado: %v", err)
	}
	if result["echo"] != "hola" {
		t.Errorf("resultado inesperado: %v", result)
	}
}

func TestDispatcher_HandlerReturnsError(t *testing.T) {
	d := NewDispatcher()
	d.RegisterMethod("fail", func(params json.RawMessage) (any, *ErrorObject) {
		return nil, InvalidParams("parámetro obligatorio ausente")
	})

	resp := d.Dispatch(Request{JSONRPC: Version, ID: NewStringID("x"), Method: "fail"})

	if resp.Error == nil || resp.Error.Code != CodeInvalidParams {
		t.Fatalf("esperaba error InvalidParams, obtuve %+v", resp.Error)
	}
	if resp.Result != nil {
		t.Error("una respuesta de error no debe traer result")
	}
}

func TestDispatcher_ResponseHasResultXorError(t *testing.T) {
	d := NewDispatcher()
	d.RegisterMethod("ok", func(params json.RawMessage) (any, *ErrorObject) {
		return struct{}{}, nil
	})
	resp := d.Dispatch(Request{JSONRPC: Version, ID: NewIntID(1), Method: "ok"})

	hasResult := resp.Result != nil
	hasError := resp.Error != nil
	if hasResult == hasError {
		t.Fatalf("la respuesta debe tener exactamente uno de result/error, tiene result=%t error=%t", hasResult, hasError)
	}
}

func TestDispatcher_NotificationUnknownMethodIsIgnored(t *testing.T) {
	d := NewDispatcher()
	err := d.DispatchNotification(Notification{JSONRPC: Version, Method: "unknown/notification"})
	if err != nil {
		t.Errorf("una notificación a un método desconocido no debe producir error, obtuve: %v", err)
	}
}

func TestDispatcher_NotificationHandlerInvoked(t *testing.T) {
	d := NewDispatcher()
	called := false
	d.RegisterNotification("notifications/initialized", func(params json.RawMessage) error {
		called = true
		return nil
	})

	if err := d.DispatchNotification(Notification{JSONRPC: Version, Method: "notifications/initialized"}); err != nil {
		t.Fatalf("no esperaba error: %v", err)
	}
	if !called {
		t.Error("el handler de la notificación no fue invocado")
	}
}
