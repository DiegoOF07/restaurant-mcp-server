# restaurant-mcp-server

A Model Context Protocol (MCP) server for restaurant recipe and inventory
management, implemented from scratch in Go with **no MCP SDK**. Messages
are exchanged directly as JSON-RPC 2.0.

## Features

- JSON-RPC 2.0 message parsing and dispatch, transport-agnostic.
- MCP lifecycle: `initialize` (with protocol version negotiation),
  `notifications/initialized`, `ping`.
- `stdio` transport: one JSON-RPC message per line on stdin, responses on
  stdout only, diagnostic logs on stderr.
- Standard JSON-RPC error codes (`-32700`..`-32603`).

## Requirements

- Go 1.22+

## Installation

```bash
git clone https://github.com/TU_USUARIO/restaurant-mcp-server.git
cd restaurant-mcp-server
go build -o bin/restaurant-mcp-server ./cmd/stdio
```

## Running (stdio)

```bash
./bin/restaurant-mcp-server
```

Then send one JSON-RPC message per line on stdin, for example:

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test-client","version":"0.0.1"}}}
```

## MCP protocol version

`2025-06-18`. `initialize` requests for any other version are rejected with
a `-32602 Invalid params` error.

## Testing

```bash
go vet ./...
go test ./... -v
```

## Security considerations

- The server never writes anything other than JSON-RPC messages to stdout.
- All diagnostic logging goes to stderr.
- Error `data` fields never leak stack traces, SQL, or credentials.

## License

MIT — see [LICENSE](./LICENSE).
