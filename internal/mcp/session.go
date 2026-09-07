package mcp

import (
	"github.com/DiegoOF07/restaurant-mcp-server/internal/domain"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/jsonrpc"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/storage"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/tools"
)

// Session agrupa todo el estado que pertenece a UNA conexión MCP: el estado del handshake,
// la identidad bajo la que se ejecutan las llamadas y el enrutador de métodos.
//
// Con stdio existe una sola sesión por proceso, así que esta separación no se notaba. Con
// HTTP hay muchas a la vez, cada una con su propio rol y su propio handshake a medio
// terminar, y confundirlas sería un fallo de seguridad: la sesión de un mesero podría
// heredar el handshake de un administrador. Lo único que comparten es el repositorio.
type Session struct {
	Dispatcher *jsonrpc.Dispatcher
	Lifecycle  *Lifecycle
	Registry   *tools.Registry
}

// NewSession arma una sesión completa y deja el dispatcher listo para recibir mensajes.
// El repositorio se comparte entre sesiones a propósito: el inventario es uno solo, y sus
// operaciones ya son seguras para uso concurrente.
func NewSession(repo storage.Repository, info ServerInfo, role domain.Role, userID string) *Session {
	dispatcher := jsonrpc.NewDispatcher()

	lifecycle := NewLifecycle(info)
	lifecycle.Register(dispatcher)

	registry := tools.NewRegistry(repo)
	tools.RegisterRestaurantTools(registry)
	registry.SetIdentity(role, userID)

	RegisterTools(dispatcher, lifecycle, registry)

	return &Session{Dispatcher: dispatcher, Lifecycle: lifecycle, Registry: registry}
}
