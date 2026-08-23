// Package jsonrpc implementa los tipos base de JSON-RPC 2.0 usados por MCP
// solo conoce el sobre "envelope" del protocolo JSON-RPC
package jsonrpc

import (
	"encoding/json"
	"fmt"
)

// Version es el valor fijo requerido por la especificación JSON-RPC 2.0
const Version = "2.0"

// ID representa el campo "id" de JSON-RPC, que puede ser string, número o
// estar ausente en el caso de una notificación
type ID struct {
	value any // Puede ser cualquier tipo de dato JSON
	set   bool
}

// NewStringID crea un ID de tipo string
func NewStringID(s string) ID { return ID{value: s, set: true} }

// NewIntID crea un ID de tipo entero
func NewIntID(n int64) ID { return ID{value: json.Number(fmt.Sprintf("%d", n)), set: true} }

// IsSet indica si el mensaje traía un id, false implica notificación
func (id ID) IsSet() bool { return id.set }

// String devuelve una representación legible del id
func (id ID) String() string {
	if !id.set {
		return "<none>"
	}
	return fmt.Sprintf("%v", id.value)
}

// Equal compara dos IDs por valor y tipo
func (id ID) Equal(other ID) bool {
	if id.set != other.set {
		return false
	}
	return fmt.Sprintf("%v", id.value) == fmt.Sprintf("%v", other.value)
}

// MarshalJSON serializa el ID preservando string vs número
func (id ID) MarshalJSON() ([]byte, error) {
	if !id.set {
		return []byte("null"), nil
	}
	return json.Marshal(id.value)
}

// UnmarshalJSON deserializa el ID detectando su tipo original
func (id *ID) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == nil {
		id.set = false
		id.value = nil
		return nil
	}
	switch v := raw.(type) {
	case string:
		id.value = v
	case float64:
		id.value = json.Number(fmt.Sprintf("%v", v))
	default:
		return fmt.Errorf("jsonrpc: id debe ser string o número, se recibió %T", raw)
	}
	id.set = true
	return nil
}

// Message es la forma cruda de cualquier mensaje JSON-RPC entrante, antes
// de clasificarlo como Request, Notification o Response
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *ID             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ErrorObject    `json:"error,omitempty"`
}

// IsRequest indica si el mensaje es una solicitud. Tiene method e id
func (m Message) IsRequest() bool {
	return m.Method != "" && m.ID != nil
}

// IsNotification indica si el mensaje es una notificación. Method sin id
func (m Message) IsNotification() bool {
	return m.Method != "" && m.ID == nil
}

// IsResponse indica si el mensaje es una respuesta. Result o error, sin method
func (m Message) IsResponse() bool {
	return m.Method == "" && (m.Result != nil || m.Error != nil)
}

// Request es una solicitud JSON-RPC saliente o entrante ya validada
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      ID              `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Notification es un mensaje sin id que no espera respuesta
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response es una respuesta JSON-RPC. Contiene Result XOR Error, nunca ambos
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      ID              `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ErrorObject    `json:"error,omitempty"`
}

// NewResultResponse construye una respuesta exitosa
func NewResultResponse(id ID, result any) (Response, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return Response{}, fmt.Errorf("jsonrpc: no se pudo serializar el resultado: %w", err)
	}
	return Response{JSONRPC: Version, ID: id, Result: raw}, nil
}

// NewErrorResponse construye una respuesta de error
func NewErrorResponse(id ID, errObj *ErrorObject) Response {
	return Response{JSONRPC: Version, ID: id, Error: errObj}
}
