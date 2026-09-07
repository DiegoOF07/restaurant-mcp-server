# restaurant-mcp-server

Servidor **MCP (Model Context Protocol)** para gestión de recetas e inventario de un
restaurante, escrito en Go **desde cero y sin ningún SDK de MCP**: los mensajes JSON-RPC 2.0
se parsean, despachan y serializan a mano.

Expone cinco herramientas que un asistente conversacional puede invocar para consultar el
menú, calcular disponibilidad real a partir del inventario, revisar alérgenos y registrar
ajustes de inventario auditados.

> 🇬🇧 *English version: [README.md](./README.md)*

---

## Índice

- [Por qué existe](#por-qué-existe)
- [Requisitos](#requisitos)
- [Compilación](#compilación)
- [Ejecución](#ejecución)
- [Herramientas expuestas](#herramientas-expuestas)
- [Control de acceso por roles](#control-de-acceso-por-roles)
- [Persistencia](#persistencia)
- [Protocolo MCP](#protocolo-mcp)
- [Transportes](#transportes)
- [Docker](#docker)
- [Casos de uso](#casos-de-uso)
- [Datos de demostración](#datos-de-demostración)
- [Estructura del proyecto](#estructura-del-proyecto)
- [Pruebas](#pruebas)
- [Consideraciones de seguridad](#consideraciones-de-seguridad)
- [Licencia](#licencia)

---

## Por qué existe

Un modelo de lenguaje no debe *calcular* cuántas porciones quedan ni *recordar* qué lleva un
platillo: se equivoca y no hay forma de auditarlo. Este servidor mueve esas decisiones al
dominio, donde sí se pueden probar.

El modelo decide **qué preguntar**; el servidor decide **cuál es la respuesta**. Por eso las
herramientas devuelven datos estructurados y no prosa, y por eso la descripción de
`get_dish_availability` dice explícitamente *«úsala siempre en vez de calcular tú mismo»*.

---

## Requisitos

- **Go 1.25 o superior.**

  El mínimo lo impone `modernc.org/sqlite`, el driver de SQLite escrito íntegramente en Go.
  Si tu `go version` es menor, con `GOTOOLCHAIN=auto` (el valor por defecto) Go descarga la
  cadena de herramientas necesaria automáticamente.

No hace falta compilador de C ni ninguna biblioteca del sistema.

---

## Compilación

```bash
git clone https://github.com/DiegoOF07/restaurant-mcp-server.git
cd restaurant-mcp-server
go build -o bin/restaurant-mcp-server ./cmd/server
```

### Compilación cruzada

El driver de SQLite es Go puro, así que con `CGO_ENABLED=0` el binario sale **estático** y se
puede compilar para cualquier plataforma desde cualquier otra:

```bash
# Windows, desde Linux o WSL
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o bin/restaurant-mcp-server.exe ./cmd/server

# macOS Apple Silicon
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -o bin/restaurant-mcp-server-macos ./cmd/server
```

El binario resultante ronda los 10 MB y no depende de nada instalado en la máquina destino:
se copia y funciona.

> **Si usas WSL**, compila y ejecuta el cliente en el **mismo** entorno. Un binario ELF
> compilado dentro de WSL no puede ejecutarse desde el `node.exe` de Windows aunque ambos vean
> el mismo disco: son dos sistemas operativos distintos. Ese es el origen del clásico error
> `spawn ENOENT (-4058)`.

---

## Ejecución

El servidor habla **stdio**: un mensaje JSON-RPC por línea en `stdin`, las respuestas en
`stdout`, y **todo** el diagnóstico en `stderr`.

```bash
./bin/restaurant-mcp-server                      # base junto al binario
./bin/restaurant-mcp-server --db /ruta/datos.db  # base en otra ubicación
./bin/restaurant-mcp-server --db :memory:        # sin persistencia (efímero)
```

Normalmente no se lanza a mano: lo lanza un host MCP como subproceso. Con el cliente de este
proyecto basta declararlo en `apps/cli/mcp.servers.json`.

### Variables de entorno

| Variable | Valores | Por defecto | Para qué |
|---|---|---|---|
| `MCP_USER_ROLE` | `waiter`, `cook`, `admin` | `waiter` | Rol bajo el que opera la conexión |
| `MCP_USER_ID` | texto libre | `unspecified` | Queda registrado en cada movimiento de inventario |
| `MCP_DB_PATH` | ruta o `:memory:` | `restaurant.db` junto al binario | Ubicación de la base |
| `MCP_HTTP_ADDR` | `host:puerto` | — | Servir por HTTP en vez de stdio |
| `MCP_AUTH_TOKENS` | `token:rol:usuario,...` | — | Credenciales para HTTP. Obligatorias para escuchar en una dirección pública |
| `MCP_ALLOWED_ORIGINS` | URLs separadas por coma | — | Orígenes de navegador permitidos por HTTP |

La bandera `--db` tiene prioridad sobre `MCP_DB_PATH`.

---

## Herramientas expuestas

| Herramienta | Rol requerido | Qué hace |
|---|---|---|
| `search_dishes` | cualquiera | Busca platillos del menú por nombre (coincidencia parcial). Nombre vacío = todos. |
| `search_ingredients` | cualquiera | Busca ingredientes por nombre o identificador; devuelve unidad base, alérgeno y existencia actual. |
| `get_dish_availability` | cualquiera | Calcula cuántas porciones se pueden preparar **ahora** con el inventario real. |
| `get_recipe_details` | cualquiera | Ingredientes, cantidades por porción y alérgenos de un platillo. |
| `adjust_inventory` | `cook` o `admin` | Registra una pérdida, daño o corrección. Idempotente y auditada. |

### `search_ingredients` es el puente entre el lenguaje y los identificadores

El usuario dice *«descuenta dos kilos de queso»*, pero las herramientas exigen
`ingredientId: "cheese"`. Sin una forma de traducir un nombre a un identificador, el asistente
tendría que adivinarlo — y adivinar identificadores es exactamente lo que no queremos. Esta
herramienta cierra ese hueco, y de paso devuelve la unidad base para que el asistente no
confunda gramos con unidades.

### `get_dish_availability` responde con el cuello de botella

No devuelve un número suelto, sino qué ingrediente limita la producción:

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

Así el asistente puede decir *«no alcanza, faltan 120 g de queso»* en lugar de un simple «no».

### `adjust_inventory` es la única operación que escribe

Tiene tres protecciones, cada una en una capa distinta:

1. **`idempotencyKey` obligatoria.** Repetir la llamada con la misma clave devuelve el
   movimiento original y **no** vuelve a descontar. Sobrevive a reinicios del servidor.
2. **Restricción de rol** (ver abajo), validada en el servidor.
3. **Confirmación del usuario**, que es responsabilidad del host. Es una capa *adicional*, no
   un sustituto: un cliente modificado podría saltársela, y por eso el permiso se valida acá.

---

## Control de acceso por roles

Tres roles, con permisos crecientes:

| Rol | Puede leer | Puede ajustar inventario |
|---|---|---|
| `waiter` | ✅ | ❌ |
| `cook` | ✅ | ✅ |
| `admin` | ✅ | ✅ |

**El permiso se valida en el servidor**, en `Registry.Call()`, antes de llegar al handler. Que
la interfaz también pregunte «¿confirmas?» es una capa distinta: el host es código cliente, y
un cliente distinto podría no aplicar ninguna restricción.

**Falla cerrado.** Sin `MCP_USER_ROLE`, o con un valor no reconocido, la conexión se queda en
`waiter` (solo lectura) y se deja constancia en `stderr`. Un rol desconocido nunca escala a uno
con más permisos.

Una denegación se devuelve como **error de negocio** (`isError: true`), no como error de
protocolo JSON-RPC. Así el asistente puede explicárselo al usuario en lenguaje natural
(«pedíselo a alguien de cocina») en lugar de que el host reviente con una excepción.

```
el rol "waiter" no está autorizado para ejecutar "adjust_inventory";
se requiere uno de: cook, admin
```

---

## Persistencia

El inventario se guarda en **SQLite**, en un único archivo que se puede copiar, respaldar o
inspeccionar con cualquier cliente estándar.

**Dónde vive la base.** Por defecto, `restaurant.db` **junto al binario** — no en el directorio
de trabajo. Es deliberado: el servidor lo lanza el host como subproceso y hereda un `cwd` que
depende de desde dónde se ejecutó el cliente. Con una ruta relativa al `cwd`, el mismo comando
abriría bases distintas según desde dónde se invoque, y el inventario «se perdería» sin
explicación.

**Siembra automática.** Una base vacía se llena con el catálogo de demostración. Una base que
ya tiene datos se respeta tal cual: reiniciar el servidor no borra el trabajo del turno.

### Las invariantes viven en el esquema, no sólo en el código

```sql
idempotency_key    TEXT NOT NULL UNIQUE,
resulting_quantity INTEGER NOT NULL CHECK (resulting_quantity >= 0)
```

El doble descuento y el inventario negativo los bloquea el motor, no un `if` en Go. Si algún
día alguien escribe en la base por otra vía, las reglas siguen puestas. Cada ajuste ocurre
dentro de una transacción: leer la existencia, escribirla y registrar el movimiento son una
sola operación atómica.

### Por qué `modernc.org/sqlite` y no `mattn/go-sqlite3`

El driver clásico necesita **cgo**, lo que obligaría a tener un toolchain de C en cada máquina
y haría imposible compilar para Windows desde Linux. Con el driver en Go puro el servidor sigue
siendo un único ejecutable estático. El costo es tamaño: el binario pasa de ~3 MB a ~10 MB.
Para un proyecto cuyo objetivo declarado es la portabilidad, es un intercambio claro.

---

## Protocolo MCP

Versión implementada: **`2025-06-18`**.

### Negociación de versión

Ante un `initialize` con una versión distinta, el servidor **no falla**: responde con una
versión que sí soporta y deja que el cliente decida si continúa o se desconecta, tal como
exige la especificación.

```jsonc
// petición
{"protocolVersion": "2099-01-01", ...}
// respuesta — sin error
{"protocolVersion": "2025-06-18", ...}
```

### Métodos implementados

| Método | Tipo | Notas |
|---|---|---|
| `initialize` | petición | Negocia versión y anuncia capacidades |
| `notifications/initialized` | notificación | Cierra el handshake; hasta recibirla, `tools/*` se rechaza |
| `ping` | petición | Comprobación de vida |
| `tools/list` | petición | Devuelve las cinco herramientas con su JSON Schema |
| `tools/call` | petición | Ejecuta una herramienta |

Se usan los códigos de error estándar de JSON-RPC (`-32700` a `-32603`).


---

## Transportes

El mismo servidor, el mismo dominio y el mismo control de roles, alcanzable de dos formas.

| | **stdio** (por defecto) | **HTTP** (`--http`) |
|---|---|---|
| Cómo corre | El host lo lanza como subproceso | Corre por su cuenta; los clientes se conectan |
| Quién lo alcanza | Sólo quien lo lanzó | Cualquiera que llegue al puerto |
| Sesiones | Una por proceso | Muchas a la vez, aisladas entre sí |
| La identidad sale de | La variable `MCP_USER_ROLE` | El token |
| Sirve para | Desarrollo local, un cliente de escritorio | Compartir con compañeros, desplegar |

```bash
./bin/restaurant-mcp-server                              # stdio
./bin/restaurant-mcp-server --http 127.0.0.1:8080        # HTTP, sólo local
```

### El transporte HTTP

Implementa el transporte **Streamable HTTP** de MCP (2025-06-18):

| Método | Comportamiento |
|---|---|
| `POST /mcp` | Un mensaje JSON-RPC por petición. Las solicitudes reciben `200` con la respuesta; las notificaciones, `202` sin cuerpo. |
| `DELETE /mcp` | Termina la sesión y libera su estado. |
| `GET /mcp` | `405`. La especificación permite omitir el flujo SSE a un servidor que no inicia mensajes, y éste declara `listChanged: false`. |
| `GET /health` | Comprobación de vida para plataformas de despliegue. No es parte de MCP. |

La respuesta de `initialize` trae la cabecera `Mcp-Session-Id`; toda petición posterior debe
repetirla. Una sesión desconocida o caducada recibe `404`, que la especificación define como
«vuelve a inicializar», no como «reintenta». Las sesiones caducan a los 30 minutos de
inactividad.

Los **lotes de JSON-RPC se rechazan**: la versión 2025-06-18 los eliminó.

### Por qué el rol viene del token y no de una cabecera

Con stdio el servidor es un subproceso que lanza el propio usuario, así que confiar en
`MCP_USER_ROLE` es razonable: quien puede fijar la variable ya podía ejecutar el binario.

Por HTTP ese razonamiento se cae. Cualquiera que alcance el puerto podría mandar
`X-Role: admin` y ascenderse solo. Por eso cada token queda ligado a un rol de antemano:

```bash
MCP_AUTH_TOKENS="unTokenLargo:admin:diego,otroToken:waiter:ana"
```

El rol es el que dice el token. Las cabeceras que afirmen otra cosa se ignoran — hay una
prueba que manda cinco cabeceras de rol distintas y confirma que al mesero se le sigue
denegando.

**El servidor se niega a arrancar** en una dirección alcanzable por red si no hay
credenciales configuradas. Así un despliegue mal configurado falla en el primer intento en
vez de servir el inventario a internet en silencio. `--insecure` lo permite igualmente, sólo
para pruebas locales.

### Otras protecciones

- **Validación de `Origin`** contra DNS rebinding, que es el ataque que convierte un servidor
  «sólo localhost» en algo que cualquier página web puede manejar. Los orígenes se habilitan
  con `--origins` / `MCP_ALLOWED_ORIGINS`; una petición sin `Origin` no viene de un navegador
  y se juzga por su token.
- **La sesión pertenece a quien la creó**: conocer su identificador no basta para usar los
  permisos de otro (`403`).
- **Identificadores de sesión con `crypto/rand`** (128 bits), porque un identificador de
  sesión es, de hecho, una credencial.
- **Tope de 4 MiB** en el cuerpo, para que un JSON enorme no agote la memoria.

---

## Docker

```bash
docker build -t restaurant-mcp .

docker run -d -p 127.0.0.1:8080:8080 \
  -e MCP_AUTH_TOKENS="unTokenLargoYAleatorio:admin:diego" \
  -v restaurant-data:/data \
  restaurant-mcp
```

O con Compose:

```bash
MCP_AUTH_TOKENS="unTokenLargoYAleatorio:admin:diego" docker compose up --build
```

La imagen pesa **12 MB** y contiene el binario y nada más: se construye sobre
`distroless/static`, que no trae shell, ni gestor de paquetes, ni siquiera libc. Corre como
**nonroot**, y la base vive en un volumen para que redesplegar no borre el inventario.

Que el binario sea estático es lo que hace todo esto posible: no hay runtime que instalar.

---

## Casos de uso

### 1. Verificar disponibilidad antes de aceptar un pedido

> **Mesero:** «¿Puedo vender dos hamburguesas especiales?»

El asistente llama a `get_dish_availability` con `servings: 2`. Sólo hay 40 g de queso y cada
porción lleva 80 g, así que la respuesta es `maximumServings: 0` y señala el queso como cuello
de botella. El mesero se entera **antes** de comprometerse con el cliente.

### 2. Responder una consulta de alérgenos sin inventar

> **Mesero:** «¿El pastel de chocolate lleva algo con gluten?»

`get_recipe_details` devuelve los alérgenos **registrados** de cada ingrediente. El asistente
tiene prohibido deducirlos por su cuenta: en una pregunta sobre alergias, una respuesta
inventada es un riesgo real para el comensal.

### 3. Registrar una pérdida de inventario

> **Cocinero:** «Se cayó una bandeja, descuenta 10 gramos de queso.»

El asistente resuelve `queso → cheese` con `search_ingredients`, el host pide confirmación y
`adjust_inventory` aplica el descuento con una `idempotencyKey` única. Si la red falla y el
cliente reintenta, la clave repetida devuelve el movimiento original **sin volver a descontar**.

### 4. El mismo intento, con el rol equivocado

> **Mesero:** «Descuenta 10 gramos de queso.»

El host pregunta y el mesero confirma — pero el servidor deniega igual, porque `waiter` no
tiene permiso. La confirmación del usuario no otorga permisos.

### Probarlo a mano, sin cliente

```bash
{
  echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"prueba","version":"1"}}}'
  echo '{"jsonrpc":"2.0","method":"notifications/initialized"}'
  echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_dish_availability","arguments":{"dishId":"special-burger","servings":2}}}'
} | MCP_USER_ROLE=cook ./bin/restaurant-mcp-server --db :memory:
```

`notifications/initialized` no es opcional: sin ella, `tools/call` se rechaza con
`-32600 Invalid Request`.

---

## Datos de demostración

Una base nueva se siembra con dos platillos y ocho ingredientes.

**Platillos**

| ID | Nombre | Receta (por porción) |
|---|---|---|
| `special-burger` | Hamburguesa Especial | 1 pan, 1 carne, 80 g queso, 20 g lechuga |
| `chocolate-cake` | Pastel de Chocolate | 150 g harina, 100 g chocolate, 50 ml leche, 20 g nueces |

**Ingredientes e inventario inicial**

| ID | Nombre | Unidad base | Alérgeno | Existencia |
|---|---|---|---|---|
| `cheese` | Queso cheddar | g | lácteos | **40** |
| `bun` | Pan de hamburguesa | unit | gluten | 10 |
| `patty` | Carne de res | unit | — | 10 |
| `lettuce` | Lechuga | g | — | 500 |
| `flour` | Harina de trigo | g | gluten | 2000 |
| `chocolate` | Chocolate amargo | g | lácteos | 1000 |
| `milk` | Leche entera | ml | lácteos | 2000 |
| `walnuts` | Nueces | g | frutos secos | 200 |

El queso está deliberadamente bajo: con 40 g alcanza para **una** hamburguesa pero no para dos.
Es el escenario que hace visible que la disponibilidad se calcula de verdad.

**Unidades.** Todo se almacena en la unidad base del ingrediente (`g`, `ml` o `unit`) como
entero. Las herramientas aceptan `kg` y `l` y convierten al recibirlas, así que nunca se acumula
error de punto flotante en el inventario.

---

## Estructura del proyecto

```
cmd/server/         Punto de entrada: elección de transporte, base e identidad
internal/
  domain/           Modelo de negocio, roles, unidades y errores. Sin dependencias externas.
  jsonrpc/          Parseo, despacho y errores de JSON-RPC 2.0. No sabe nada de MCP.
  mcp/              Ciclo de vida MCP y adaptación de tools/* al registro.
  storage/          Repository (interfaz) + implementaciones en memoria y en SQLite.
  auth/             Token -> identidad, para conexiones remotas.
  httpmcp/          Transporte Streamable HTTP: sesiones, autenticación y Origin.
  tools/            Las cinco herramientas, sus esquemas y el control de roles.
```

Las capas dependen sólo hacia adentro: `jsonrpc` no sabe qué es MCP y `domain` no sabe que
existe una base de datos. `storage.Repository` es lo que permite que las pruebas rápidas corran
en memoria y el servidor real use SQLite sin que ninguna herramienta se entere.

---

## Pruebas

```bash
go vet ./...
go test ./...
```

Las pruebas de comportamiento del repositorio corren **contra las dos implementaciones** con los
mismos casos. Eso es lo que hace que probar en memoria por rapidez siga diciendo algo cierto
sobre el SQLite que corre en producción; si una implementación se desvía, el mismo caso pasa en
una y falla en la otra.

---

## Consideraciones de seguridad

- El servidor **nunca** escribe en `stdout` algo que no sea un mensaje JSON-RPC. Cualquier otra
  cosa corrompería el flujo del protocolo. Todo el diagnóstico va a `stderr`.
- Los campos `data` de los errores no filtran trazas de pila, SQL ni credenciales.
- Los permisos se validan en el servidor, no en el cliente.
- Las entradas se validan contra el JSON Schema declarado de cada herramienta antes de tocar el
  dominio.
- Todas las consultas usan parámetros ligados; no se construye SQL concatenando texto.
- El archivo de base de datos hereda los permisos del sistema de archivos: si llegara a contener
  datos reales, protégelo como cualquier otro archivo con información del negocio.

---

## Licencia

MIT — ver [LICENSE](./LICENSE).
