package jsonrpc

import "encoding/json"

// HandlerFunc procesa una solicitud y espera respuesta
// Devuelve el resultado a serializar o un ErrorObject ya formado
type HandlerFunc func(params json.RawMessage) (result any, errObj *ErrorObject)

// NotificationFunc procesa una notificación
type NotificationFunc func(params json.RawMessage) error

// Dispatcher enruta mensajes ya parseados hacia el handler registrado para su método
type Dispatcher struct {
	methods       map[string]HandlerFunc
	notifications map[string]NotificationFunc
}

// NewDispatcher crea un Dispatcher vacío.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		methods:       make(map[string]HandlerFunc),
		notifications: make(map[string]NotificationFunc),
	}
}

// RegisterMethod asocia un método JSON-RPC (que espera respuesta) a un handler
func (d *Dispatcher) RegisterMethod(name string, handler HandlerFunc) {
	d.methods[name] = handler
}

// RegisterNotification asocia una notificación JSON-RPC a un handler
func (d *Dispatcher) RegisterNotification(name string, handler NotificationFunc) {
	d.notifications[name] = handler
}

// Dispatch ejecuta el handler correspondiente a una Request y siempre
// devuelve una Response de éxito o de error, lista para serializar
func (d *Dispatcher) Dispatch(req Request) Response {
	handler, ok := d.methods[req.Method]
	if !ok {
		return NewErrorResponse(req.ID, MethodNotFound(req.Method))
	}

	result, errObj := handler(req.Params)
	if errObj != nil {
		return NewErrorResponse(req.ID, errObj)
	}

	resp, err := NewResultResponse(req.ID, result)
	if err != nil {
		return NewErrorResponse(req.ID, InternalError("no se pudo serializar el resultado"))
	}
	return resp
}

// DispatchNotification ejecuta el handler de una notificación. Al no existir
// un canal de respuesta en JSON-RPC para notificaciones, el error se devuelve únicamente para que el llamador lo registre en logs
func (d *Dispatcher) DispatchNotification(n Notification) error {
	handler, ok := d.notifications[n.Method]
	if !ok {
		return nil
	}
	return handler(n.Params)
}
