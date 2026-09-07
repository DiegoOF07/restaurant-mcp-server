// Command server expone el servidor MCP del restaurante por stdio o por HTTP.
//
// Ambos transportes comparten exactamente el mismo dominio, las mismas herramientas y el
// mismo control de roles: lo único que cambia es cómo entran y salen los bytes.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/auth"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/domain"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/httpmcp"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/jsonrpc"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/mcp"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/storage"
)

var serverInfo = mcp.ServerInfo{Name: "restaurant-mcp-server", Version: "0.3.0"}

const shutdownGrace = 10 * time.Second

func main() {
	dbPath := flag.String("db", "", "ruta del archivo SQLite (por defecto restaurant.db junto al binario; usa :memory: para no persistir)")
	httpAddr := flag.String("http", "", "dirección para servir por HTTP, por ejemplo :8080 o 127.0.0.1:8080. Sin esta bandera se usa stdio")
	httpPath := flag.String("path", "/mcp", "ruta del endpoint MCP cuando se sirve por HTTP")
	origins := flag.String("origins", "", "orígenes de navegador permitidos, separados por coma (vacío = ninguno)")
	insecure := flag.Bool("insecure", false, "permite escuchar en una dirección pública SIN autenticación. Sólo para pruebas")
	rateLimit := flag.Float64("rate-limit", 20, "peticiones por segundo permitidas a cada cliente (0 desactiva el límite)")
	rateBurst := flag.Float64("rate-burst", 40, "peticiones seguidas toleradas antes de aplicar el ritmo")
	trustProxy := flag.Bool("trust-proxy", false, "lee la IP del cliente de X-Forwarded-For. Actívalo SÓLO si hay un proxy de confianza delante")
	showVersion := flag.Bool("version", false, "muestra la versión y termina")
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s %s (protocolo MCP %s)\n", serverInfo.Name, serverInfo.Version, mcp.ProtocolVersion)
		return
	}

	logger := log.New(os.Stderr, "[restaurant-mcp-server] ", log.LstdFlags)

	repo, err := openRepository(*dbPath, logger)
	if err != nil {
		logger.Fatalf("no se pudo abrir el almacenamiento: %v", err)
	}
	defer repo.Close()

	// La identidad de respaldo es la de stdio: la que se usa cuando no hay credenciales.
	role, userID := identityFromEnv(logger)

	addr := *httpAddr
	if addr == "" {
		addr = os.Getenv("MCP_HTTP_ADDR")
	}

	if addr != "" {
		opts := httpOptions{
			addr:       addr,
			path:       *httpPath,
			rawOrigins: *origins,
			insecure:   *insecure,
			rateLimit:  *rateLimit,
			rateBurst:  *rateBurst,
			trustProxy: *trustProxy,
		}
		if err := runHTTP(repo, opts, role, userID, logger); err != nil {
			logger.Fatalf("fallo del servidor HTTP: %v", err)
		}
		return
	}

	runStdio(repo, role, userID, logger)
}

// runStdio atiende una única conexión por entrada y salida estándar.
func runStdio(repo storage.Repository, role domain.Role, userID string, logger *log.Logger) {
	logger.Printf("transporte stdio; identidad activa: rol=%s usuario=%s", role, userID)

	session := mcp.NewSession(repo, serverInfo, role, userID)
	if err := stdioLoop(os.Stdin, os.Stdout, session.Dispatcher, logger); err != nil && err != io.EOF {
		logger.Fatalf("fallo fatal en el bucle stdio: %v", err)
	}
}

// httpOptions agrupa las banderas del modo HTTP, para no arrastrar ocho parámetros sueltos.
type httpOptions struct {
	addr       string
	path       string
	rawOrigins string
	insecure   bool
	rateLimit  float64
	rateBurst  float64
	trustProxy bool
}

// runHTTP atiende muchas conexiones concurrentes, cada una con su propia sesión MCP.
func runHTTP(
	repo storage.Repository,
	opts httpOptions,
	fallbackRole domain.Role,
	fallbackUser string,
	logger *log.Logger,
) error {
	addr, path, rawOrigins, insecure := opts.addr, opts.path, opts.rawOrigins, opts.insecure
	tokens, err := auth.ParseTokens(os.Getenv("MCP_AUTH_TOKENS"))
	if err != nil {
		return fmt.Errorf("MCP_AUTH_TOKENS: %w", err)
	}

	if rawOrigins == "" {
		rawOrigins = os.Getenv("MCP_ALLOWED_ORIGINS")
	}
	allowedOrigins, err := httpmcp.NormalizeOrigins(rawOrigins)
	if err != nil {
		return fmt.Errorf("orígenes permitidos: %w", err)
	}

	if !httpmcp.IsLoopback(addr) && tokens.Empty() && !insecure {
		return fmt.Errorf(
			"te dispones a escuchar en %q, que es alcanzable desde la red, sin ninguna credencial.\n"+
				"  Define MCP_AUTH_TOKENS=\"unTokenSecreto:admin:tuUsuario\" para exigir autenticación,\n"+
				"  o usa una dirección local (127.0.0.1:8080),\n"+
				"  o pasa --insecure si de verdad quieres exponerlo sin protección",
			addr,
		)
	}

	if tokens.Empty() {
		logger.Printf("ATENCIÓN: sin credenciales configuradas; toda conexión actuará como rol=%s usuario=%s",
			fallbackRole, fallbackUser)
	} else {
		logger.Printf("autenticación por token activa (%d credenciales configuradas)", tokens.Count())
	}
	if len(allowedOrigins) == 0 {
		logger.Printf("no hay orígenes de navegador permitidos; sólo clientes que no envían Origin")
	}

	server, err := httpmcp.New(httpmcp.Config{
		Addr:           addr,
		Path:           path,
		Repo:           repo,
		ServerInfo:     serverInfo,
		Tokens:         tokens,
		Fallback:       auth.Principal{Role: fallbackRole, UserID: fallbackUser},
		AllowedOrigins: allowedOrigins,
		RateLimit:      opts.rateLimit,
		RateBurst:      opts.rateBurst,
		TrustProxy:     opts.trustProxy,
		Logger:         logger,
	})
	if err != nil {
		return err
	}

	return serveUntilSignal(server, logger)
}

func serveUntilSignal(server *httpmcp.Server, logger *log.Logger) error {
	// El canal se prepara ANTES de arrancar: si la señal llegara mientras el servidor sube,
	// registrarse después la perdería y el proceso moriría sin apagarse bien.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	errs := make(chan error, 1)
	go func() {

		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
			return
		}
		errs <- nil
	}()

	select {
	case err := <-errs:
		return err

	case sig := <-signals:
		logger.Printf("señal %s recibida; cerrando (hasta %s para las peticiones en curso)", sig, shutdownGrace)

		ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			logger.Printf("el cierre ordenado no terminó a tiempo: %v", err)
			return server.Close()
		}

		logger.Printf("cerrado correctamente")
		return nil
	}
}

// usage documenta también las variables de entorno
func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprintf(out, `%s %s — servidor MCP de recetas e inventario (protocolo %s)

USO
  %s [banderas]

  Sin --http habla MCP por stdio: un mensaje JSON-RPC por línea en stdin,
  las respuestas en stdout y el diagnóstico en stderr.

BANDERAS
`, serverInfo.Name, serverInfo.Version, mcp.ProtocolVersion, os.Args[0])

	flag.PrintDefaults()

	fmt.Fprintf(out, `
VARIABLES DE ENTORNO
  MCP_USER_ROLE        waiter | cook | admin. Rol de la conexión con stdio.
                       Por defecto waiter (solo lectura): sin cook o admin,
                       adjust_inventory se deniega.
  MCP_USER_ID          Queda registrado en cada movimiento de inventario.
  MCP_DB_PATH          Ruta de la base. Por defecto restaurant.db junto al
                       binario. Usa :memory: para no dejar archivos.
  MCP_HTTP_ADDR        Equivalente a --http.
  MCP_AUTH_TOKENS      Credenciales del modo HTTP, con el formato
                       "token:rol:usuario,token:rol:usuario". Obligatorias
                       para escuchar en una dirección alcanzable por red:
                       por HTTP el rol se deriva del token, nunca de una
                       cabecera que el cliente pueda elegir.
  MCP_ALLOWED_ORIGINS  Orígenes de navegador permitidos, separados por coma.

EJEMPLOS
  # Local, por stdio, con permiso para ajustar inventario
  MCP_USER_ROLE=cook %s

  # Remoto, por HTTP, con dos credenciales
  MCP_AUTH_TOKENS="tokenA:admin:diego,tokenB:waiter:ana" %s --http 0.0.0.0:8080

  # Comprobar que responde
  curl http://127.0.0.1:8080/health
`, os.Args[0], os.Args[0])
}

// openRepository resuelve dónde vive la base y la deja lista para usar
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

// stdioLoop contiene el bucle principal, separado de main para poder probarlo con buffers.
func stdioLoop(in io.Reader, out io.Writer, dispatcher *jsonrpc.Dispatcher, logger *log.Logger) error {
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
