// Package httpmcp expone el servidor MCP por HTTP, siguiendo el transporte
// "Streamable HTTP" de la especificación 2025-06-18.

package httpmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/auth"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/jsonrpc"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/mcp"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/storage"
)

const (
	// sessionHeader identifica la sesión MCP. Lo emite el servidor al responder initialize.
	sessionHeader = "Mcp-Session-Id"
	// versionHeader lo envía el cliente en cada petición posterior al handshake.
	versionHeader = "MCP-Protocol-Version"

	// maxBodyBytes acota el cuerpo de una petición. Sin tope, un cliente podría agotar la
	// memoria del servidor mandando un JSON enorme antes de que se rechace.
	maxBodyBytes = 4 << 20 // 4 MiB

	// sessionTTL es cuánto sobrevive una sesión inactiva. Sin caducidad, cada cliente que
	// se desconecta sin avisar dejaría estado colgado para siempre.
	sessionTTL = 30 * time.Minute
)

// Config son los parámetros del servidor HTTP.
type Config struct {
	Addr       string
	Path       string
	Repo       storage.Repository
	ServerInfo mcp.ServerInfo
	Tokens     *auth.TokenStore
	// Fallback se usa sólo cuando no hay credenciales configuradas (modo local).
	Fallback auth.Principal
	// AllowedOrigins son los orígenes de navegador aceptados. Vacío = ninguno.
	AllowedOrigins []string

	// RateLimit es el ritmo sostenido de peticiones por segundo y por cliente.
	// 0 o menos desactiva el límite.
	RateLimit float64
	// RateBurst es cuántas peticiones seguidas se toleran antes de aplicar el ritmo.
	RateBurst float64
	// TrustProxy hace que la IP del cliente se lea de X-Forwarded-For. Actívalo SÓLO si hay
	// un proxy de confianza delante: la cabecera la escribe el cliente y es falsificable.
	TrustProxy bool

	Logger *log.Logger
}

// Server es el servidor MCP sobre HTTP.
type Server struct {
	config   Config
	logger   *log.Logger
	mu       sync.Mutex
	sessions map[string]*session
	limiter  *rateLimiter
	http     *http.Server
}

type session struct {
	id        string
	mcp       *mcp.Session
	principal auth.Principal
	lastSeen  time.Time
	// mu serializa los mensajes de UNA sesión. El handshake y el estado del ciclo de vida
	// no toleran que dos peticiones de la misma sesión se crucen.
	mu sync.Mutex
}

// New construye el servidor y deja las rutas montadas.
func New(config Config) (*Server, error) {
	if config.Path == "" {
		config.Path = "/mcp"
	}
	if config.Logger == nil {
		config.Logger = log.New(io.Discard, "", 0)
	}
	if config.Repo == nil {
		return nil, errors.New("httpmcp: falta el repositorio")
	}

	s := &Server{
		config:   config,
		logger:   config.Logger,
		sessions: make(map[string]*session),
		limiter:  newRateLimiter(config.RateLimit, config.RateBurst),
	}

	mux := http.NewServeMux()
	mux.HandleFunc(config.Path, s.handleMCP)
	mux.HandleFunc("/health", s.handleHealth)

	s.http = &http.Server{
		Addr:              config.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	return s, nil
}

// ListenAndServe arranca el servidor y no retorna hasta que se cierra. Un apagado ordenado
// devuelve http.ErrServerClosed, que el llamador debe tratar como final normal.
func (s *Server) ListenAndServe() error {
	stop := s.startReaper()
	defer close(stop)

	s.logger.Printf("escuchando en http://%s%s", s.config.Addr, s.config.Path)
	if s.limiter != nil {
		s.logger.Printf("límite de peticiones: %.0f/s por cliente (ráfaga %.0f)", s.config.RateLimit, s.config.RateBurst)
	} else {
		s.logger.Printf("límite de peticiones desactivado")
	}
	return s.http.ListenAndServe()
}

// Close corta el servidor de inmediato, sin esperar a las peticiones en vuelo.
// Para un apagado ordenado usa Shutdown.
func (s *Server) Close() error { return s.http.Close() }

// Shutdown deja de aceptar conexiones nuevas y espera a que terminen las peticiones que ya
// estaban siendo atendidas, hasta que el contexto se cancele
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// Handler expone el enrutador, para poder probarlo sin abrir un puerto.
func (s *Server) Handler() http.Handler { return s.http.Handler }

// startReaper borra periódicamente las sesiones inactivas.
func (s *Server) startReaper() chan struct{} {
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				s.reapExpired()
				s.limiter.cleanup(sessionTTL)
			}
		}
	}()
	return stop
}

func (s *Server) reapExpired() {
	cutoff := time.Now().Add(-sessionTTL)

	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sess := range s.sessions {
		if sess.lastSeen.Before(cutoff) {
			delete(s.sessions, id)
			s.logger.Printf("sesión %s caducada por inactividad", id)
		}
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "método no permitido", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	active := len(s.sessions)
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"protocolVersion": mcp.ProtocolVersion,
		"server":          s.config.ServerInfo,
		"activeSessions":  active,
	})
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if allowed, wait := s.limiter.Allow(clientKey(r, s.config.TrustProxy)); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		http.Error(w, "demasiadas peticiones; espera un momento", http.StatusTooManyRequests)
		return
	}

	// Se valida ANTES de mirar credenciales: un navegador en una página hostil ya lleva
	// las cookies del usuario, así que rechazar por origen es la primera barrera.
	if !s.originAllowed(r) {
		http.Error(w, "origen no permitido", http.StatusForbidden)
		return
	}

	switch r.Method {
	case http.MethodPost:
		s.handlePost(w, r)
	case http.MethodDelete:
		s.handleDelete(w, r)
	case http.MethodGet:
		// La especificación permite no ofrecer el canal SSE. Se responde 405 con Allow,
		// que es exactamente cómo un cliente descubre que no hay stream que abrir.
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "este servidor no ofrece flujo SSE: no emite mensajes por su cuenta", http.StatusMethodNotAllowed)
	default:
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "método no permitido", http.StatusMethodNotAllowed)
	}
}

// originAllowed protege contra DNS rebinding, que es el ataque que convierte un servidor
// pensado para localhost en algo que cualquier página web puede usar.
func (s *Server) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Sin Origin no es un navegador: es curl o un cliente MCP. Ahí la defensa que
		// aplica es el token, no esta.
		return true
	}
	for _, allowed := range s.config.AllowedOrigins {
		if allowed == "*" || strings.EqualFold(allowed, origin) {
			return true
		}
	}
	return false
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticate(w, r); !ok {
		return
	}

	id := r.Header.Get(sessionHeader)
	if id == "" {
		http.Error(w, "falta la cabecera "+sessionHeader, http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	_, existed := s.sessions[id]
	delete(s.sessions, id)
	s.mu.Unlock()

	if !existed {
		http.Error(w, "sesión desconocida", http.StatusNotFound)
		return
	}

	s.logger.Printf("sesión %s terminada por el cliente", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authenticate(w, r)
	if !ok {
		return
	}

	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		http.Error(w, "Content-Type debe ser application/json", http.StatusUnsupportedMediaType)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, "no se pudo leer el cuerpo de la petición", http.StatusBadRequest)
		return
	}

	// La versión 2025-06-18 eliminó el batching de JSON-RPC. Se rechaza explícitamente en
	// vez de intentar interpretarlo, para que el cliente vea el motivo real.
	if trimmed := strings.TrimSpace(string(body)); strings.HasPrefix(trimmed, "[") {
		writeRPCError(w, http.StatusBadRequest,
			jsonrpc.InvalidRequest("MCP 2025-06-18 no admite lotes de JSON-RPC: envía un mensaje por petición"))
		return
	}

	parsed, errObj := jsonrpc.ParseMessage(body)
	if errObj != nil {
		writeRPCError(w, http.StatusBadRequest, errObj)
		return
	}

	isInitialize := parsed.Kind == jsonrpc.KindRequest && parsed.Request.Method == "initialize"

	sess, errStatus, errMsg := s.resolveSession(r, principal, isInitialize)
	if sess == nil {
		http.Error(w, errMsg, errStatus)
		return
	}

	// Cada sesión atiende un mensaje a la vez: el handshake es una máquina de estados y
	// dos peticiones simultáneas podrían dejarla a medio camino.
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.lastSeen = time.Now()

	if isInitialize {
		w.Header().Set(sessionHeader, sess.id)
	}

	switch parsed.Kind {
	case jsonrpc.KindRequest:
		s.logger.Printf("[%s] -> %s", sess.id, parsed.Request.Method)
		resp := sess.mcp.Dispatcher.Dispatch(parsed.Request)
		writeJSON(w, http.StatusOK, resp)

	case jsonrpc.KindNotification:
		s.logger.Printf("[%s] -> notificación %s", sess.id, parsed.Notification.Method)
		if err := sess.mcp.Dispatcher.DispatchNotification(parsed.Notification); err != nil {
			s.logger.Printf("[%s] error en notificación %s: %v", sess.id, parsed.Notification.Method, err)
		}
		// Una notificación no lleva respuesta: 202 es lo que indica la especificación.
		w.WriteHeader(http.StatusAccepted)

	default:
		http.Error(w, "un cliente no debe enviar respuestas al servidor", http.StatusBadRequest)
	}
}

// resolveSession devuelve la sesión de la petición, creándola si es un initialize.
// Ante un fallo devuelve nil junto con el estado HTTP y el mensaje que corresponde.
func (s *Server) resolveSession(r *http.Request, principal auth.Principal, isInitialize bool) (*session, int, string) {
	id := r.Header.Get(sessionHeader)

	if isInitialize && id == "" {
		return s.newSession(principal), 0, ""
	}

	if id == "" {
		return nil, http.StatusBadRequest,
			"falta la cabecera " + sessionHeader + ": inicia la sesión con initialize"
	}

	s.mu.Lock()
	sess, ok := s.sessions[id]
	s.mu.Unlock()

	if !ok {
		// 404 es la señal que la especificación define para "tu sesión ya no existe":
		// el cliente debe volver a hacer initialize en vez de reintentar a ciegas.
		return nil, http.StatusNotFound, "sesión desconocida o caducada: vuelve a enviar initialize"
	}

	// Una sesión pertenece a la identidad que la creó. Sin esta comprobación, quien
	// conociera un identificador de sesión podría usar los permisos de otro.
	if sess.principal != principal {
		return nil, http.StatusForbidden, "esta sesión pertenece a otra identidad"
	}

	if v := r.Header.Get(versionHeader); v != "" && v != sess.mcp.Lifecycle.NegotiatedVersion() {
		return nil, http.StatusBadRequest,
			fmt.Sprintf("%s=%q no coincide con la versión negociada en esta sesión", versionHeader, v)
	}

	return sess, 0, ""
}

func (s *Server) newSession(principal auth.Principal) *session {
	sess := &session{
		id:        newSessionID(),
		mcp:       mcp.NewSession(s.config.Repo, s.config.ServerInfo, principal.Role, principal.UserID),
		principal: principal,
		lastSeen:  time.Now(),
	}

	s.mu.Lock()
	s.sessions[sess.id] = sess
	s.mu.Unlock()

	s.logger.Printf("sesión %s creada para rol=%s usuario=%s", sess.id, principal.Role, principal.UserID)
	return sess
}

// authenticate resuelve la identidad de la petición. Si no hay credenciales configuradas
// el servidor está en modo local y todos comparten la identidad de respaldo.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	if s.config.Tokens.Empty() {
		return s.config.Fallback, true
	}

	header := r.Header.Get("Authorization")
	token, found := strings.CutPrefix(header, "Bearer ")
	if !found {
		// WWW-Authenticate no es decorativo: es cómo un cliente distingue "falta la
		// credencial" de "la credencial es inválida".
		w.Header().Set("WWW-Authenticate", `Bearer realm="restaurant-mcp-server"`)
		http.Error(w, "falta la cabecera Authorization: Bearer <token>", http.StatusUnauthorized)
		return auth.Principal{}, false
	}

	principal, ok := s.config.Tokens.Lookup(strings.TrimSpace(token))
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="restaurant-mcp-server", error="invalid_token"`)
		http.Error(w, "token inválido", http.StatusUnauthorized)
		return auth.Principal{}, false
	}

	return principal, true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeRPCError responde con un error JSON-RPC bien formado en vez de texto plano. El id va
// nulo porque estos fallos ocurren ANTES de poder leerlo: si el cuerpo no se pudo parsear,
// no hay id que devolver.
func writeRPCError(w http.ResponseWriter, status int, errObj *jsonrpc.ErrorObject) {
	writeJSON(w, status, jsonrpc.NewErrorResponse(jsonrpc.ID{}, errObj))
}

// IsLoopback indica si la dirección de escucha sólo es alcanzable desde esta máquina.
// Quien llama lo usa para exigir autenticación antes de exponerse a la red.
func IsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" {
		// ":8080" significa todas las interfaces.
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// NormalizeOrigins limpia la lista de orígenes permitidos y verifica que sean URLs.
func NormalizeOrigins(raw string) ([]string, error) {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if item == "*" {
			out = append(out, item)
			continue
		}
		u, err := url.Parse(item)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("origen inválido %q: se espera algo como https://mi-app.com", item)
		}
		out = append(out, strings.TrimSuffix(item, "/"))
	}
	return out, nil
}
