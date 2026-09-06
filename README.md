# restaurant-mcp-server

A **Model Context Protocol (MCP)** server for restaurant recipe and inventory management,
written in Go **from scratch with no MCP SDK**: JSON-RPC 2.0 messages are parsed, dispatched
and serialized by hand.

It exposes five tools that a conversational assistant can invoke to browse the menu, compute
real availability from current stock, check allergens, and record audited inventory
adjustments.

> Academic project — CC3067 Redes, Universidad del Valle de Guatemala.
>
> 🇪🇸 *Versión en español: [README.es.md](./README.es.md)*
>
> Note: the server's user-facing strings (tool descriptions and error messages) are in
> **Spanish** by design — the assistant is built for a Spanish-speaking restaurant. This
> document is in English.

---

## Table of contents

- [Why it exists](#why-it-exists)
- [Requirements](#requirements)
- [Installation](#installation)
- [Running it](#running-it)
- [Implemented features](#implemented-features)
- [Tools](#tools)
- [Role-based access control](#role-based-access-control)
- [Persistence](#persistence)
- [MCP protocol](#mcp-protocol)
- [Usage examples](#usage-examples)
- [Demo data](#demo-data)
- [Project layout](#project-layout)
- [Testing](#testing)
- [Security considerations](#security-considerations)
- [License](#license)

---

## Why it exists

A language model should not *compute* how many servings are left, nor *remember* what goes
into a dish: it gets those wrong, and there is no way to audit it. This server moves those
decisions into a domain layer where they can actually be tested.

The model decides **what to ask**; the server decides **what the answer is**. That is why the
tools return structured data rather than prose, and why the description of
`get_dish_availability` explicitly says *"always use this instead of computing it yourself."*

---

## Requirements

- **Go 1.25 or newer.**

  The minimum is imposed by `modernc.org/sqlite`, the SQLite driver written entirely in Go.
  If your `go version` is older, `GOTOOLCHAIN=auto` (the default) downloads the required
  toolchain automatically.

No C compiler and no system libraries are needed.

---

## Installation

```bash
git clone https://github.com/DiegoOF07/restaurant-mcp-server.git
cd restaurant-mcp-server
go build -o bin/restaurant-mcp-server ./cmd/stdio
```

### Cross-compiling

The SQLite driver is pure Go, so with `CGO_ENABLED=0` the binary is **statically linked** and
can be built for any platform from any other:

```bash
# Windows, from Linux or WSL
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o bin/restaurant-mcp-server.exe ./cmd/stdio

# macOS Apple Silicon
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -o bin/restaurant-mcp-server-macos ./cmd/stdio
```

The resulting binary is around 10 MB and depends on nothing installed on the target machine:
copy it and it runs.

> **If you use WSL**, build and run the client in the **same** environment. An ELF binary built
> inside WSL cannot be launched by Windows' `node.exe`, even though both see the same disk:
> they are two different operating systems. This is the usual cause of the classic
> `spawn ENOENT (-4058)` error.

---

## Running it

The server speaks **stdio**: one JSON-RPC message per line on `stdin`, responses on `stdout`,
and **all** diagnostics on `stderr`.

```bash
./bin/restaurant-mcp-server                      # database next to the binary
./bin/restaurant-mcp-server --db /path/data.db   # database elsewhere
./bin/restaurant-mcp-server --db :memory:        # no persistence (ephemeral)
```

You normally don't launch it by hand: an MCP host launches it as a subprocess. With this
project's client, just declare it in `apps/cli/mcp.servers.json`.

### Environment variables

| Variable | Values | Default | Purpose |
|---|---|---|---|
| `MCP_USER_ROLE` | `waiter`, `cook`, `admin` | `waiter` | Role the connection operates under |
| `MCP_USER_ID` | free text | `unspecified` | Recorded on every inventory movement |
| `MCP_DB_PATH` | path or `:memory:` | `restaurant.db` next to the binary | Database location |

The `--db` flag takes precedence over `MCP_DB_PATH`.

---

## Implemented features

- **JSON-RPC 2.0** parsing, dispatch and error handling, written from scratch and independent
  of the transport. Standard error codes (`-32700` … `-32603`).
- **MCP lifecycle**: `initialize` with **version negotiation**, `notifications/initialized`,
  `ping`. `tools/*` is rejected until the handshake completes.
- **stdio transport**: one message per line; `stdout` carries protocol traffic only.
- **Five tools** with declared JSON Schemas, returning both human-readable text and structured
  content.
- **Role-based access control**, enforced server-side and failing closed.
- **SQLite persistence** through a pure-Go driver, with atomic transactions, idempotency and
  non-negative stock enforced as schema constraints.
- **Auditable inventory movements**: every adjustment records who performed it, why, and the
  quantities before and after.
- **Unit conversion** (`kg`→`g`, `l`→`ml`) with integer base units, so no floating-point error
  ever accumulates in stock levels.

---

## Tools

| Tool | Required role | What it does |
|---|---|---|
| `search_dishes` | any | Finds menu dishes by name (partial, case-insensitive). Empty name returns all. |
| `search_ingredients` | any | Finds ingredients by name or ID; returns base unit, allergen and current stock. |
| `get_dish_availability` | any | Computes how many servings can be prepared **right now** from real stock. |
| `get_recipe_details` | any | Ingredients, per-serving quantities and allergens for a dish. |
| `adjust_inventory` | `cook` or `admin` | Records a loss, damage or correction. Idempotent and audited. |

### `search_ingredients` bridges language and identifiers

A user says *"subtract two kilos of cheese"*, but the tools require `ingredientId: "cheese"`.
Without a way to translate a name into an identifier, the assistant would have to guess one —
and guessing identifiers is exactly what we want to avoid. This tool closes that gap, and
returns the base unit so the assistant doesn't confuse grams with pieces.

### `get_dish_availability` answers with the bottleneck

It doesn't return a bare number, but which ingredient limits production:

```json
{
  "available": false,
  "requestedServings": 2,
  "maximumServings": 0,
  "missingIngredients": [
    { "ingredientId": "cheese", "required": 160, "available": 40, "unit": "g" }
  ]
}
```

That lets the assistant say *"not enough — you're 120 g of cheese short"* rather than a flat no.

### `adjust_inventory` is the only write operation

It has three safeguards, each at a different layer:

1. **A mandatory `idempotencyKey`.** Repeating the call with the same key returns the original
   movement and does **not** subtract again. This survives server restarts.
2. **A role restriction** (see below), enforced by the server.
3. **User confirmation**, which is the host's responsibility. That is an *additional* layer,
   not a substitute: a modified client could skip it, which is precisely why the permission is
   checked here.

---

## Role-based access control

Three roles with increasing privileges:

| Role | Can read | Can adjust inventory |
|---|---|---|
| `waiter` | ✅ | ❌ |
| `cook` | ✅ | ✅ |
| `admin` | ✅ | ✅ |

**Permissions are enforced on the server**, in `Registry.Call()`, before the handler runs. The
interface also asking "are you sure?" is a separate layer: the host is client code, and a
different client might apply no restrictions at all.

**It fails closed.** With no `MCP_USER_ROLE`, or an unrecognized value, the connection stays at
`waiter` (read-only) and the fallback is logged to `stderr`. An unknown role never escalates to
one with more privileges.

A denial is returned as a **business error** (`isError: true`), not a JSON-RPC protocol error.
That way the assistant can explain it to the user in natural language ("ask someone from the
kitchen") instead of the host blowing up with an exception.

```
el rol "waiter" no está autorizado para ejecutar "adjust_inventory";
se requiere uno de: cook, admin
```

---

## Persistence

Inventory is stored in **SQLite**, in a single file you can copy, back up, or inspect with any
standard client.

**Where the database lives.** By default, `restaurant.db` **next to the binary** — not in the
working directory. This is deliberate: the server is launched as a subprocess by the host and
inherits whatever `cwd` the client had. With a path relative to `cwd`, the same command would
open different databases depending on where it was invoked from, and inventory would appear to
"vanish" with no explanation.

**Automatic seeding.** An empty database is populated with the demo catalog. A database that
already holds data is left alone: restarting the server does not wipe the shift's work.

### Invariants live in the schema, not only in the code

```sql
idempotency_key    TEXT NOT NULL UNIQUE,
resulting_quantity INTEGER NOT NULL CHECK (resulting_quantity >= 0)
```

Double subtraction and negative stock are blocked by the engine, not by an `if` in Go. If
someone ever writes to the database through another path, the rules still hold. Each adjustment
runs inside a transaction: reading stock, writing it back and recording the movement are a
single atomic operation.

### Why `modernc.org/sqlite` and not `mattn/go-sqlite3`

The classic driver requires **cgo**, which would demand a C toolchain on every machine and make
it impossible to build for Windows from Linux. With the pure-Go driver the server remains a
single static executable. The cost is size: the binary grows from ~3 MB to ~10 MB. For a
project whose stated goal is portability, that is a clear trade.

---

## MCP protocol

Implemented version: **`2025-06-18`**.

### Version negotiation

Given an `initialize` request with a different version, the server **does not fail**: it
responds with a version it does support and lets the client decide whether to continue or
disconnect, as the specification requires.

```jsonc
// request
{"protocolVersion": "2099-01-01", ...}
// response — no error
{"protocolVersion": "2025-06-18", ...}
```

### Implemented methods

| Method | Type | Notes |
|---|---|---|
| `initialize` | request | Negotiates the version and advertises capabilities |
| `notifications/initialized` | notification | Completes the handshake; until it arrives, `tools/*` is rejected |
| `ping` | request | Liveness check |
| `tools/list` | request | Returns the five tools with their JSON Schemas |
| `tools/call` | request | Executes a tool |

---

## Usage examples

### 1. Check availability before accepting an order

> **Waiter:** "Can I sell two special burgers?"

The assistant calls `get_dish_availability` with `servings: 2`. There are only 40 g of cheese
and each serving takes 80 g, so the answer is `maximumServings: 0`, with cheese flagged as the
bottleneck. The waiter finds out **before** committing to the customer.

### 2. Answer an allergen question without inventing anything

> **Waiter:** "Does the chocolate cake contain gluten?"

`get_recipe_details` returns each ingredient's **recorded** allergens. The assistant is
forbidden from inferring them: on an allergy question, a made-up answer is a real risk to the
diner.

### 3. Record an inventory loss

> **Cook:** "A tray dropped — subtract 10 grams of cheese."

The assistant resolves `queso → cheese` via `search_ingredients`, the host asks for
confirmation, and `adjust_inventory` applies the change with a unique `idempotencyKey`. If the
connection drops and the client retries, the repeated key returns the original movement
**without subtracting again**.

### 4. The same attempt, with the wrong role

> **Waiter:** "Subtract 10 grams of cheese."

The host asks and the waiter confirms — but the server denies it anyway, because `waiter` has
no permission. User confirmation does not grant privileges.

### Trying it by hand, without a client

```bash
{
  echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}'
  echo '{"jsonrpc":"2.0","method":"notifications/initialized"}'
  echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_dish_availability","arguments":{"dishId":"special-burger","servings":2}}}'
} | MCP_USER_ROLE=cook ./bin/restaurant-mcp-server --db :memory:
```

`notifications/initialized` is not optional: without it, `tools/call` is rejected with
`-32600 Invalid Request`.

---

## Demo data

A fresh database is seeded with two dishes and eight ingredients.

**Dishes**

| ID | Name | Recipe (per serving) |
|---|---|---|
| `special-burger` | Hamburguesa Especial | 1 bun, 1 patty, 80 g cheese, 20 g lettuce |
| `chocolate-cake` | Pastel de Chocolate | 150 g flour, 100 g chocolate, 50 ml milk, 20 g walnuts |

**Ingredients and starting stock**

| ID | Name | Base unit | Allergen | Stock |
|---|---|---|---|---|
| `cheese` | Queso cheddar | g | dairy | **40** |
| `bun` | Pan de hamburguesa | unit | gluten | 10 |
| `patty` | Carne de res | unit | — | 10 |
| `lettuce` | Lechuga | g | — | 500 |
| `flour` | Harina de trigo | g | gluten | 2000 |
| `chocolate` | Chocolate amargo | g | dairy | 1000 |
| `milk` | Leche entera | ml | dairy | 2000 |
| `walnuts` | Nueces | g | tree nuts | 200 |

Cheese is deliberately low: 40 g is enough for **one** burger but not two. That is the scenario
that makes it visible that availability is genuinely computed.

**Units.** Everything is stored in the ingredient's base unit (`g`, `ml` or `unit`) as an
integer. Tools accept `kg` and `l` and convert on the way in, so floating-point error never
accumulates in stock levels.

---

## Project layout

```
cmd/stdio/          Entry point: stdio loop and database selection
internal/
  domain/           Business model, roles, units and errors. No external dependencies.
  jsonrpc/          JSON-RPC 2.0 parsing, dispatch and errors. Knows nothing about MCP.
  mcp/              MCP lifecycle and adaptation of tools/* to the registry.
  storage/          Repository interface + in-memory and SQLite implementations.
  tools/            The five tools, their schemas and role enforcement.
```

Dependencies point inward only: `jsonrpc` doesn't know what MCP is, and `domain` doesn't know a
database exists. `storage.Repository` is what lets fast tests run in memory while the real
server uses SQLite, without any tool noticing the difference.

---

## Testing

```bash
go vet ./...
go test ./...
```

The repository's behavioral tests run **against both implementations** with the same cases.
That is what keeps the fast in-memory tests meaningful about the SQLite that runs in
production; if one implementation drifts, the same case passes in one and fails in the other.

---

## Security considerations

- The server **never** writes anything to `stdout` other than JSON-RPC messages. Anything else
  would corrupt the protocol stream. All diagnostics go to `stderr`.
- Error `data` fields never leak stack traces, SQL, or credentials.
- Permissions are enforced on the server, not the client.
- Inputs are validated against each tool's declared JSON Schema before reaching the domain.
- All queries use bound parameters; SQL is never built by string concatenation.
- The database file inherits filesystem permissions: if it ever holds real data, protect it
  like any other file containing business information.

---

## License

MIT — see [LICENSE](./LICENSE).
