package mcp

import (
	"encoding/json"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/jsonrpc"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/tools"
)

// listToolsResult es la forma de la respuesta de "tools/list".
type listToolsResult struct {
	Tools []tools.Tool `json:"tools"`
}

// callToolParams es el cuerpo esperado en la solicitud "tools/call".
type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// RegisterTools conecta tools/list y tools/call al dispatcher
func RegisterTools(d *jsonrpc.Dispatcher, lifecycle *Lifecycle, registry *tools.Registry) {
	d.RegisterMethod("tools/list", func(params json.RawMessage) (any, *jsonrpc.ErrorObject) {
		if !lifecycle.Ready() {
			return nil, jsonrpc.InvalidRequest("tools/list called before completing MCP initialization")
		}
		return listToolsResult{Tools: registry.List()}, nil
	})

	d.RegisterMethod("tools/call", func(params json.RawMessage) (any, *jsonrpc.ErrorObject) {
		if !lifecycle.Ready() {
			return nil, jsonrpc.InvalidRequest("tools/call called before completing MCP initialization")
		}

		var p callToolParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, jsonrpc.InvalidParams("could not parse tools/call params: " + err.Error())
		}
		if p.Name == "" {
			return nil, jsonrpc.InvalidParams(`missing required field "name"`)
		}

		result, errObj := registry.Call(p.Name, p.Arguments)
		if errObj != nil {
			return nil, errObj
		}
		return result, nil
	})
}
