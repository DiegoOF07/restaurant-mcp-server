package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/domain"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/jsonrpc"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/mcp"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/storage"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/tools"
)

func main() {
	dbPath := flag.String("db", "", "ruta del archivo SQLite (por defecto restaurant.db junto al binario; usa :memory: para no persistir)")
	flag.Parse()

	logger := log.New(os.Stderr, "[restaurant-mcp-server] ", log.LstdFlags)

	repo, err := openRepository(*dbPath, logger)
	if err != nil {
		logger.Fatalf("no se pudo abrir el almacenamiento: %v", err)
	}
	defer repo.Close()

	dispatcher := jsonrpc.NewDispatcher()
	lifecycle := mcp.NewLifecycle(mcp.ServerInfo{
		Name:    "restaurant-mcp-server",
		Version: "0.1.0",
	})
	lifecycle.Register(dispatcher)

	registry := tools.NewRegistry(repo)
	tools.RegisterRestaurantTools(registry)

	// Identidad de la conexión. Con stdio el servidor es un subproceso lanzado por el host,
	// así que el rol llega por entorno. Si no viene o no se reconoce, se conserva el rol menos
	// privilegiado que NewRegistry ya dejó puesto: fallar cerrado.
	role, userID := identityFromEnv(logger)
	registry.SetIdentity(role, userID)
	logger.Printf("identidad activa: rol=%s usuario=%s", role, userID)
	mcp.RegisterTools(dispatcher, lifecycle, registry)

	if err := run(os.Stdin, os.Stdout, dispatcher, logger); err != nil && err != io.EOF {
		logger.Fatalf("fallo fatal en el bucle stdio: %v", err)
	}
}

// openRepository resuelve dónde vive la base y la deja lista para usar.
//
// Precedencia: la bandera --db, luego MCP_DB_PATH, y si ninguna está, un archivo
// restaurant.db JUNTO AL BINARIO. Anclarlo al ejecutable y no al directorio de trabajo es
// deliberado: el servidor lo lanza el host como subproceso, y el cwd que herede depende de
// desde dónde se ejecutó el CLI. Con una ruta relativa al cwd, el mismo comando abriría
// bases distintas según desde dónde se invoque, y el inventario "se perdería" sin explicación.
func openRepository(flagPath string, logger *log.Logger) (*storage.SQLiteRepository, error) {
	path := flagPath
	if path == "" {
		path = os.Getenv("MCP_DB_PATH")
	}
	if path == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("no se pudo ubicar el ejecutable para elegir la base: %w", err)
		}
		path = filepath.Join(filepath.Dir(exe), "restaurant.db")
	}

	repo, err := storage.OpenSQLite(path)
	if err != nil {
		return nil, err
	}

	empty, err := repo.IsEmpty()
	if err != nil {
		repo.Close()
		return nil, fmt.Errorf("no se pudo inspeccionar la base: %w", err)
	}
	// Sólo se siembra una base vacía. En una ya usada, resembrar reescribiría el catálogo
	// y confundiría cualquier ajuste de inventario previo.
	if empty {
		if err := repo.Seed(); err != nil {
			repo.Close()
			return nil, fmt.Errorf("no se pudo sembrar el catálogo inicial: %w", err)
		}
		logger.Printf("base nueva en %s: catálogo de demostración cargado", path)
	} else {
		logger.Printf("base existente en %s: se conserva el inventario guardado", path)
	}

	return repo, nil
}

// identityFromEnv lee el rol y el usuario de las variables de entorno.
// Un rol desconocido NO cae a uno con más permisos: se degrada al menos privilegiado
// y se deja constancia en stderr.
func identityFromEnv(logger *log.Logger) (domain.Role, string) {
	userID := os.Getenv("MCP_USER_ID")
	if userID == "" {
		userID = "unspecified"
	}

	raw := os.Getenv("MCP_USER_ROLE")
	if raw == "" {
		logger.Printf("MCP_USER_ROLE no está definida; se usa el rol %q (solo lectura)", domain.RoleWaiter)
		return domain.RoleWaiter, userID
	}

	role, ok := domain.ParseRole(raw)
	if !ok {
		logger.Printf("MCP_USER_ROLE=%q no es un rol válido; se usa %q (solo lectura)", raw, domain.RoleWaiter)
		return domain.RoleWaiter, userID
	}
	return role, userID
}

// run contiene el bucle principal, separado de main para poder probarlo con buffers
func run(in io.Reader, out io.Writer, dispatcher *jsonrpc.Dispatcher, logger *log.Logger) error {
	scanner := bufio.NewScanner(in)
	// Los mensajes MCP pueden crecer bastante por lo que se
	// amplía el buffer por línea más allá del default de bufio
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	writer := bufio.NewWriter(out)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue // líneas en blanco no son un mensaje
		}

		parsed, errObj := jsonrpc.ParseMessage(line)
		if errObj != nil {
			logger.Printf("mensaje inválido descartado: %s", errObj.Error())
			continue
		}

		switch parsed.Kind {
		case jsonrpc.KindRequest:
			logger.Printf("-> request id=%s method=%s", parsed.Request.ID.String(), parsed.Request.Method)
			resp := dispatcher.Dispatch(parsed.Request)
			if err := writeMessage(writer, resp); err != nil {
				return fmt.Errorf("no se pudo escribir la respuesta: %w", err)
			}
			logger.Printf("<- response id=%s ok=%t", resp.ID.String(), resp.Error == nil)

		case jsonrpc.KindNotification:
			logger.Printf("-> notification method=%s", parsed.Notification.Method)
			if err := dispatcher.DispatchNotification(parsed.Notification); err != nil {
				logger.Printf("error procesando notificación %s: %v", parsed.Notification.Method, err)
			}

		case jsonrpc.KindResponse:
			logger.Printf("respuesta inesperada recibida, se descarta (id=%s)", parsed.Response.ID.String())

		default:
			logger.Printf("mensaje de tipo desconocido descartado")
		}
	}

	return scanner.Err()
}

func writeMessage(w *bufio.Writer, resp jsonrpc.Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if err := w.WriteByte('\n'); err != nil {
		return err
	}
	return w.Flush()
}
