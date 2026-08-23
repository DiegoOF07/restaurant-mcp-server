package jsonrpc

import (
	"encoding/json"
	"fmt"
)

// Kind clasifica un mensaje ya parseado
type Kind int

const (
	KindUnknown Kind = iota
	KindRequest
	KindNotification
	KindResponse
)

// Parsed es el resultado de interpretar una línea de entrada
type Parsed struct {
	Kind         Kind
	Request      Request
	Notification Notification
	Response     Response
}

// ParseMessage decodifica una línea JSON-RPC cruda y la clasifica
func ParseMessage(raw []byte) (Parsed, *ErrorObject) {
	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		return Parsed{}, ParseError(err.Error())
	}

	if msg.JSONRPC != Version {
		return Parsed{}, InvalidRequest(
			fmt.Sprintf(`campo "jsonrpc" debe ser %q, se recibió %q`, Version, msg.JSONRPC),
		)
	}

	switch {
	case msg.IsRequest():
		return Parsed{
			Kind: KindRequest,
			Request: Request{
				JSONRPC: msg.JSONRPC,
				ID:      *msg.ID,
				Method:  msg.Method,
				Params:  msg.Params,
			},
		}, nil

	case msg.IsNotification():
		return Parsed{
			Kind: KindNotification,
			Notification: Notification{
				JSONRPC: msg.JSONRPC,
				Method:  msg.Method,
				Params:  msg.Params,
			},
		}, nil

	case msg.IsResponse():
		id := ID{}
		if msg.ID != nil {
			id = *msg.ID
		}
		return Parsed{
			Kind: KindResponse,
			Response: Response{
				JSONRPC: msg.JSONRPC,
				ID:      id,
				Result:  msg.Result,
				Error:   msg.Error,
			},
		}, nil

	default:
		return Parsed{}, InvalidRequest(
			"el mensaje no tiene una forma válida de request, notification o response",
		)
	}
}
