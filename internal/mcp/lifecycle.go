// Package mcp implementa el ciclo de vida mínimo de Model Context Protocol (versión 2025-06-18) sobre el paquete jsonrpc
package mcp

import (
	"encoding/json"
	"sync"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/jsonrpc"
)

// ProtocolVersion es la versión preferida de este servidor: la que se ofrece cuando el cliente
// pide una que no soportamos.
const ProtocolVersion = "2025-06-18"

// SupportedVersions son todas las versiones de MCP que este servidor sabe hablar, de la más
// preferida a la menos. Hoy es una sola, pero la negociación ya está escrita para varias.
var SupportedVersions = []string{ProtocolVersion}

// supports indica si el servidor puede hablar la versión pedida.
func supports(version string) bool {
	for _, v := range SupportedVersions {
		if v == version {
			return true
		}
	}
	return false
}

// ServerInfo identifica a este servidor ante el cliente
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ServerCapabilities declara qué capacidades opcionales de MCP soporta este servidor
type ServerCapabilities struct {
	Tools *struct {
		ListChanged bool `json:"listChanged"`
	} `json:"tools,omitempty"`
}

// InitializeParams es el cuerpo esperado en la solicitud "initialize"
type InitializeParams struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    json.RawMessage `json:"capabilities"`
	ClientInfo      ServerInfo      `json:"clientInfo"`
}

// InitializeResult es la respuesta que el servidor da a "initialize"
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
}

// Lifecycle mantiene el estado mínimo del ciclo de vida de una conexión MCP
// Un servidor stdio atiende una sola conexión a la vez, así que este estado vive por proceso
type Lifecycle struct {
	mu                sync.Mutex
	initialized       bool
	confirmed         bool
	negotiatedVersion string
	serverInfo        ServerInfo
}

// NegotiatedVersion devuelve la versión acordada en initialize, o "" si aún no ocurrió.
func (l *Lifecycle) NegotiatedVersion() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.negotiatedVersion
}

// NewLifecycle crea el estado de ciclo de vida para un servidor identificado
// como serverInfo con nombre y versión que se anuncian al cliente
func NewLifecycle(serverInfo ServerInfo) *Lifecycle {
	return &Lifecycle{serverInfo: serverInfo}
}

// Register asocia los métodos de ciclo de vida al dispatcher dado.
func (l *Lifecycle) Register(d *jsonrpc.Dispatcher) {
	d.RegisterMethod("initialize", l.handleInitialize)
	d.RegisterMethod("ping", l.handlePing)
	d.RegisterNotification("notifications/initialized", l.handleInitialized)
}

func (l *Lifecycle) handleInitialize(params json.RawMessage) (any, *jsonrpc.ErrorObject) {
	var p InitializeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, jsonrpc.InvalidParams("no se pudo interpretar los parámetros de initialize: " + err.Error())
	}
	if p.ProtocolVersion == "" {
		return nil, jsonrpc.InvalidParams(`falta el campo requerido "protocolVersion"`)
	}
	// La especificación exige responder con una versión que SÍ soportemos en vez de fallar:
	// así un cliente con otra versión puede decidir si se adapta o corta la conexión.
	negotiated := p.ProtocolVersion
	if !supports(negotiated) {
		negotiated = ProtocolVersion
	}

	l.mu.Lock()
	l.initialized = true
	l.negotiatedVersion = negotiated
	l.mu.Unlock()

	return InitializeResult{
		ProtocolVersion: negotiated,
		Capabilities: ServerCapabilities{
			Tools: &struct {
				ListChanged bool `json:"listChanged"`
			}{ListChanged: false},
		},
		ServerInfo: l.serverInfo,
	}, nil
}

func (l *Lifecycle) handleInitialized(_ json.RawMessage) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.confirmed = true
	return nil
}

func (l *Lifecycle) handlePing(_ json.RawMessage) (any, *jsonrpc.ErrorObject) {
	return struct{}{}, nil
}

func (l *Lifecycle) Ready() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.initialized && l.confirmed
}
