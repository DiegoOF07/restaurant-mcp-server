package jsonrpc

import "fmt"

// Códigos de error estándar de JSON-RPC 2.0
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// ErrorObject es el objeto "error" de una respuesta JSON-RPC
type ErrorObject struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implementa la interfaz error, para que un *ErrorObject pueda propagarse
// como cualquier otro error de Go sin perder el código JSON-RPC original.
func (e *ErrorObject) Error() string {
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// ParseError se usa cuando el JSON recibido no pudo decodificarse
func ParseError(detail string) *ErrorObject {
	return &ErrorObject{Code: CodeParseError, Message: "Parse error", Data: detail}
}

// InvalidRequest se usa cuando el mensaje no cumple la forma de JSON-RPC 2.0
func InvalidRequest(detail string) *ErrorObject {
	return &ErrorObject{Code: CodeInvalidRequest, Message: "Invalid Request", Data: detail}
}

// MethodNotFound se usa cuando no existe un handler registrado para el método
func MethodNotFound(method string) *ErrorObject {
	return &ErrorObject{Code: CodeMethodNotFound, Message: "Method not found", Data: method}
}

// InvalidParams se usa cuando los parámetros no cumplen el schema esperado por el método
func InvalidParams(detail string) *ErrorObject {
	return &ErrorObject{Code: CodeInvalidParams, Message: "Invalid params", Data: detail}
}

// InternalError se usa para fallos inesperados del servidor
func InternalError(detail string) *ErrorObject {
	return &ErrorObject{Code: CodeInternalError, Message: "Internal error", Data: detail}
}
