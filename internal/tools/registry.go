// Package tools implementa las herramientas MCP del restaurante y las conecta al dominio
package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/domain"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/jsonrpc"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/storage"
)

// ContentBlock es un bloque de contenido de un resultado de tools/call
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// CallToolResult es la forma de la respuesta de "tools/call"
type CallToolResult struct {
	Content           []ContentBlock `json:"content"`
	StructuredContent any            `json:"structuredContent,omitempty"`
	IsError           bool           `json:"isError,omitempty"`
}

// HandlerFunc ejecuta la lógica de una herramienta. Devuelve el resultado
// ya armado y el propio handler decide si es éxito o isError:true
type HandlerFunc func(ctx CallContext, rawArgs json.RawMessage) (CallToolResult, *jsonrpc.ErrorObject)

// CallContext lleva todo lo que un handler necesita además de sus argumentos:
// el repositorio y la identidad de quien ejecuta la llamada.
type CallContext struct {
	Repo   storage.Repository
	Role   domain.Role
	UserID string
}

// Tool es la definición completa de una herramienta expuesta por tools/list
type Tool struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`

	// RequiredRoles limita quién puede ejecutar la herramienta. Vacío = cualquier rol.
	// No se serializa: es una regla del servidor, no parte del contrato que ve el cliente.
	RequiredRoles domain.RoleSet `json:"-"`

	handler HandlerFunc
}

// Registry contiene todas las herramientas disponibles, el repositorio sobre el que operan
// y la identidad bajo la que se ejecutan las llamadas de esta conexión.
type Registry struct {
	repo   storage.Repository
	role   domain.Role
	userID string
	tools  map[string]Tool
	order  []string // conserva el orden de registro para tools/list
}

// NewRegistry crea un registro vacío ligado a un Repository concreto.
// El rol arranca en el MENOS privilegiado: si nadie configura una identidad, el servidor
// queda de solo lectura en vez de permitir mutaciones sin supervisión.
func NewRegistry(repo storage.Repository) *Registry {
	return &Registry{repo: repo, role: domain.RoleWaiter, userID: "unspecified", tools: make(map[string]Tool)}
}

// SetIdentity fija el rol y el identificador del usuario de esta conexión.
func (r *Registry) SetIdentity(role domain.Role, userID string) {
	r.role = role
	if userID != "" {
		r.userID = userID
	}
}

// Role devuelve el rol activo de esta conexión.
func (r *Registry) Role() domain.Role {
	return r.role
}

func (r *Registry) register(t Tool) {
	r.tools[t.Name] = t
	r.order = append(r.order, t.Name)
}

// List devuelve las herramientas en el formato esperado por tools/list, sin el campo interno "handler"
func (r *Registry) List() []Tool {
	list := make([]Tool, 0, len(r.tools))
	for _, name := range r.order {
		list = append(list, r.tools[name])
	}
	return list
}

// Call ejecuta la herramienta 'name' con los argumentos crudos dados
func (r *Registry) Call(name string, rawArgs json.RawMessage) (CallToolResult, *jsonrpc.ErrorObject) {
	tool, ok := r.tools[name]
	if !ok {
		return CallToolResult{}, jsonrpc.InvalidParams("herramienta desconocida: " + name)
	}

	// El permiso se valida ACÁ, en el servidor. Que la interfaz también pida confirmación
	// es una capa distinta y no sustituye a esta.
	if !tool.RequiredRoles.Allows(r.role) {
		return errorResult(fmt.Sprintf(
			"el rol %q no está autorizado para ejecutar %q; se requiere uno de: %s",
			r.role, name, strings.Join(tool.RequiredRoles.Names(), ", "),
		)), nil
	}

	return tool.handler(CallContext{Repo: r.repo, Role: r.role, UserID: r.userID}, rawArgs)
}

// errorResult construye un CallToolResult de error de negocio con un
// mensaje de texto legible, sin filtrar detalles internos
func errorResult(message string) CallToolResult {
	return CallToolResult{
		Content: []ContentBlock{{Type: "text", Text: message}},
		IsError: true,
	}
}

// okResult construye un CallToolResult exitoso a partir de un resultado
// estructurado y un resumen de texto
func okResult(summary string, structured any) CallToolResult {
	return CallToolResult{
		Content:           []ContentBlock{{Type: "text", Text: summary}},
		StructuredContent: structured,
	}
}
