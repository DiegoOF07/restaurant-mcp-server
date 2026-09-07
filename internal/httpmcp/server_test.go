package httpmcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/auth"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/domain"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/httpmcp"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/mcp"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/storage"
)

const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`

func newServer(t *testing.T, config httpmcp.Config) http.Handler {
	t.Helper()

	repo := storage.NewInMemoryRepository()
	repo.Seed()

	config.Repo = repo
	config.ServerInfo = mcp.ServerInfo{Name: "test", Version: "0"}
	if config.Fallback.Role == "" {
		config.Fallback = auth.Principal{Role: domain.RoleAdmin, UserID: "test"}
	}

	server, err := httpmcp.New(config)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return server.Handler()
}

// post envía un mensaje y devuelve la respuesta cruda.
func post(t *testing.T, handler http.Handler, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// handshake abre una sesión completa y devuelve su identificador.
func handshake(t *testing.T, handler http.Handler, headers map[string]string) string {
	t.Helper()

	rec := post(t, handler, initializeBody, headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("initialize devolvió %d: %s", rec.Code, rec.Body.String())
	}

	sessionID := rec.Header().Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("initialize no devolvió Mcp-Session-Id")
	}

	withSession := map[string]string{"Mcp-Session-Id": sessionID}
	for k, v := range headers {
		withSession[k] = v
	}
	if rec := post(t, handler, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, withSession); rec.Code != http.StatusAccepted {
		t.Fatalf("notifications/initialized devolvió %d, se esperaba 202", rec.Code)
	}
	return sessionID
}

func TestInitializeCreatesSession(t *testing.T) {
	handler := newServer(t, httpmcp.Config{})

	rec := post(t, handler, initializeBody, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("código = %d, se esperaba 200", rec.Code)
	}
	if got := rec.Header().Get("Mcp-Session-Id"); got == "" {
		t.Error("falta la cabecera Mcp-Session-Id")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"protocolVersion":"2025-06-18"`) {
		t.Errorf("respuesta inesperada: %s", rec.Body.String())
	}
}

// Cada sesión debe recibir un identificador distinto; reutilizarlos permitiría a un cliente
// aterrizar en la sesión de otro.
func TestSessionIDsAreUnique(t *testing.T) {
	handler := newServer(t, httpmcp.Config{})

	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		id := post(t, handler, initializeBody, nil).Header().Get("Mcp-Session-Id")
		if seen[id] {
			t.Fatalf("identificador de sesión repetido: %q", id)
		}
		seen[id] = true
	}
}

func TestToolsCallWithinSession(t *testing.T) {
	handler := newServer(t, httpmcp.Config{})
	sessionID := handshake(t, handler, nil)

	rec := post(t, handler,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_dish_availability","arguments":{"dishId":"special-burger","servings":2}}}`,
		map[string]string{"Mcp-Session-Id": sessionID})

	if rec.Code != http.StatusOK {
		t.Fatalf("código = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"maximumServings":0`) {
		t.Errorf("el dominio no calculó la disponibilidad: %s", rec.Body.String())
	}
}

// Sin sesión no se puede llamar a una herramienta: si se permitiera, el handshake dejaría
// de ser obligatorio por HTTP y el estado del ciclo de vida no significaría nada.
func TestToolsCallWithoutSessionIsRejected(t *testing.T) {
	handler := newServer(t, httpmcp.Config{})

	rec := post(t, handler, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("código = %d, se esperaba 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Mcp-Session-Id") {
		t.Errorf("el error debería nombrar la cabecera que falta: %s", rec.Body.String())
	}
}

// 404 es la señal que la especificación define para "tu sesión ya no existe": el cliente
// debe volver a hacer initialize, no reintentar la misma petición.
func TestUnknownSessionReturns404(t *testing.T) {
	handler := newServer(t, httpmcp.Config{})

	rec := post(t, handler, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		map[string]string{"Mcp-Session-Id": "0123456789abcdef0123456789abcdef"})

	if rec.Code != http.StatusNotFound {
		t.Fatalf("código = %d, se esperaba 404", rec.Code)
	}
}

func TestDeleteTerminatesSession(t *testing.T) {
	handler := newServer(t, httpmcp.Config{})
	sessionID := handshake(t, handler, nil)

	req := httptest.NewRequest(http.MethodDelete, "/mcp", nil)
	req.Header.Set("Mcp-Session-Id", sessionID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE devolvió %d, se esperaba 204", rec.Code)
	}

	// Tras borrarla, la sesión ya no debe existir.
	after := post(t, handler, `{"jsonrpc":"2.0","id":9,"method":"tools/list"}`,
		map[string]string{"Mcp-Session-Id": sessionID})
	if after.Code != http.StatusNotFound {
		t.Errorf("la sesión sobrevivió al DELETE: %d", after.Code)
	}
}

// La versión 2025-06-18 eliminó el batching. Se rechaza con un motivo explícito en vez de
// intentar interpretarlo a medias.
func TestBatchIsRejected(t *testing.T) {
	handler := newServer(t, httpmcp.Config{})

	rec := post(t, handler, `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("código = %d, se esperaba 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "lotes") {
		t.Errorf("el error debería explicar que no se admiten lotes: %s", rec.Body.String())
	}
}

func TestGetReturns405WithAllow(t *testing.T) {
	handler := newServer(t, httpmcp.Config{})

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("código = %d, se esperaba 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, "POST") {
		t.Errorf("Allow = %q, debería anunciar POST", allow)
	}
}

func TestHealthEndpoint(t *testing.T) {
	handler := newServer(t, httpmcp.Config{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("código = %d", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("respuesta no es JSON: %v", err)
	}
	if payload["status"] != "ok" {
		t.Errorf("payload = %v", payload)
	}
}

func TestMalformedJSONReturnsRPCError(t *testing.T) {
	handler := newServer(t, httpmcp.Config{})

	rec := post(t, handler, `{esto no es json`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("código = %d, se esperaba 400", rec.Code)
	}
	// El cuerpo debe ser un error JSON-RPC bien formado, no texto suelto: el cliente lo
	// parsea igual que cualquier otra respuesta.
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("el error no vino como JSON-RPC: %v (%s)", err, rec.Body.String())
	}
	if resp["error"] == nil {
		t.Errorf("falta el campo error: %s", rec.Body.String())
	}
}

func TestRejectsNonJSONContentType(t *testing.T) {
	handler := newServer(t, httpmcp.Config{})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(initializeBody))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("código = %d, se esperaba 415", rec.Code)
	}
}

func TestAuthentication(t *testing.T) {
	tokens, err := auth.ParseTokens("tok-cocina:cook:ana,tok-mesa:waiter:luis")
	if err != nil {
		t.Fatalf("ParseTokens: %v", err)
	}
	handler := newServer(t, httpmcp.Config{Tokens: tokens})

	t.Run("sin token se rechaza y se anuncia el esquema", func(t *testing.T) {
		rec := post(t, handler, initializeBody, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("código = %d, se esperaba 401", rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Error("falta WWW-Authenticate: el cliente no puede saber qué credencial mandar")
		}
	})

	t.Run("token inválido se rechaza", func(t *testing.T) {
		rec := post(t, handler, initializeBody, map[string]string{"Authorization": "Bearer inventado"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("código = %d, se esperaba 401", rec.Code)
		}
	})

	t.Run("token válido abre sesión", func(t *testing.T) {
		rec := post(t, handler, initializeBody, map[string]string{"Authorization": "Bearer tok-cocina"})
		if rec.Code != http.StatusOK {
			t.Fatalf("código = %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// El rol sale del token, nunca de una cabecera que el cliente controle. Este es el caso que
// separa "seguro por HTTP" de "cualquiera se asciende a administrador".
func TestRoleComesFromTokenNotFromHeaders(t *testing.T) {
	tokens, err := auth.ParseTokens("tok-mesa:waiter:luis")
	if err != nil {
		t.Fatalf("ParseTokens: %v", err)
	}
	handler := newServer(t, httpmcp.Config{Tokens: tokens})

	// El cliente intenta ascenderse con cabeceras inventadas.
	headers := map[string]string{
		"Authorization":   "Bearer tok-mesa",
		"X-MCP-Role":      "admin",
		"MCP-User-Role":   "admin",
		"X-User-Role":     "admin",
		"Mcp-User-Role":   "admin",
		"Mcp-Role":        "admin",
		"X-Forwarded-For": "127.0.0.1",
	}
	sessionID := handshake(t, handler, headers)
	headers["Mcp-Session-Id"] = sessionID

	rec := post(t, handler,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"adjust_inventory","arguments":{"ingredientId":"cheese","operation":"subtract","quantity":10,"unit":"g","idempotencyKey":"k1"}}}`,
		headers)

	if rec.Code != http.StatusOK {
		t.Fatalf("código = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"isError":true`) {
		t.Fatalf("el mesero logró ajustar inventario mandando cabeceras: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no está autorizado") {
		t.Errorf("respuesta inesperada: %s", rec.Body.String())
	}
}

// Una sesión pertenece a quien la creó: conocer su identificador no debe bastar para usar
// los permisos de otro.
func TestSessionCannotBeHijackedByAnotherIdentity(t *testing.T) {
	tokens, err := auth.ParseTokens("tok-admin:admin:ana,tok-mesa:waiter:luis")
	if err != nil {
		t.Fatalf("ParseTokens: %v", err)
	}
	handler := newServer(t, httpmcp.Config{Tokens: tokens})

	adminSession := handshake(t, handler, map[string]string{"Authorization": "Bearer tok-admin"})

	rec := post(t, handler, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, map[string]string{
		"Authorization":  "Bearer tok-mesa",
		"Mcp-Session-Id": adminSession,
	})

	if rec.Code != http.StatusForbidden {
		t.Fatalf("código = %d, se esperaba 403: %s", rec.Code, rec.Body.String())
	}
}

func TestOriginValidation(t *testing.T) {
	handler := newServer(t, httpmcp.Config{AllowedOrigins: []string{"https://app.permitida.com"}})

	t.Run("origen permitido pasa", func(t *testing.T) {
		rec := post(t, handler, initializeBody, map[string]string{"Origin": "https://app.permitida.com"})
		if rec.Code != http.StatusOK {
			t.Fatalf("código = %d", rec.Code)
		}
	})

	t.Run("origen desconocido se rechaza", func(t *testing.T) {
		rec := post(t, handler, initializeBody, map[string]string{"Origin": "https://sitio-malicioso.com"})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("código = %d, se esperaba 403", rec.Code)
		}
	})

	t.Run("sin Origin pasa: no es un navegador", func(t *testing.T) {
		if rec := post(t, handler, initializeBody, nil); rec.Code != http.StatusOK {
			t.Fatalf("código = %d", rec.Code)
		}
	})
}

func TestIsLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8080": true,
		"localhost:8080": true,
		"[::1]:8080":     true,
		":8080":          false,
		"0.0.0.0:8080":   false,
		"192.168.1.5:80": false,
	}
	for addr, want := range cases {
		if got := httpmcp.IsLoopback(addr); got != want {
			t.Errorf("IsLoopback(%q) = %t, se esperaba %t", addr, got, want)
		}
	}
}
