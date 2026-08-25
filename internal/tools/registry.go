// Package tools implementa las herramientas MCP del restaurante y las conecta al dominio
package tools

import (
	"encoding/json"

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
type HandlerFunc func(repo storage.Repository, rawArgs json.RawMessage) (CallToolResult, *jsonrpc.ErrorObject)

// Tool es la definición completa de una herramienta expuesta por tools/list
type Tool struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	handler      HandlerFunc
}

// Registry contiene todas las herramientas disponibles y el repositorio sobre el que operan
type Registry struct {
	repo  storage.Repository
	tools map[string]Tool
	order []string // conserva el orden de registro para tools/list
}

// NewRegistry crea un registro vacío ligado a un Repository concreto
func NewRegistry(repo storage.Repository) *Registry {
	return &Registry{repo: repo, tools: make(map[string]Tool)}
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
		return CallToolResult{}, jsonrpc.InvalidParams("unknown tool: " + name)
	}
	return tool.handler(r.repo, rawArgs)
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