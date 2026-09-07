package httpmcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/mcp"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/storage"
)

// Estos ayudantes son propios del paquete interno; el archivo server_test.go vive en el
// paquete externo httpmcp_test y no puede compartirlos.

func seededRepo(t *testing.T) storage.Repository {
	t.Helper()
	repo := storage.NewInMemoryRepository()
	repo.Seed()
	return repo
}

func testServerInfo() mcp.ServerInfo {
	return mcp.ServerInfo{Name: "test", Version: "0"}
}

// Las pruebas de este archivo son internas al paquete: comprueban la mecánica de la cubeta,
// que no está expuesta hacia afuera.

func TestRateLimiterAllowsUpToBurst(t *testing.T) {
	limiter := newRateLimiter(10, 3)

	for i := 1; i <= 3; i++ {
		if ok, _ := limiter.Allow("cliente"); !ok {
			t.Fatalf("la petición %d debería haber pasado: la ráfaga es de 3", i)
		}
	}

	ok, wait := limiter.Allow("cliente")
	if ok {
		t.Fatal("la cuarta petición debería haberse rechazado")
	}
	if wait <= 0 {
		t.Errorf("la espera sugerida debe ser positiva, se obtuvo %v", wait)
	}
}

func TestRateLimiterRefillsOverTime(t *testing.T) {
	// 100 fichas por segundo: una ficha cada 10 ms, así la prueba no tarda.
	limiter := newRateLimiter(100, 1)

	if ok, _ := limiter.Allow("cliente"); !ok {
		t.Fatal("la primera petición debería pasar")
	}
	if ok, _ := limiter.Allow("cliente"); ok {
		t.Fatal("la segunda inmediata debería rechazarse")
	}

	time.Sleep(30 * time.Millisecond)

	if ok, _ := limiter.Allow("cliente"); !ok {
		t.Error("tras esperar, la cubeta debería haberse repuesto")
	}
}

// Cada cliente tiene su cubeta: uno que abusa no debe afectar a los demás.
func TestRateLimiterIsPerClient(t *testing.T) {
	limiter := newRateLimiter(10, 1)

	if ok, _ := limiter.Allow("cliente-a"); !ok {
		t.Fatal("cliente-a debería pasar")
	}
	if ok, _ := limiter.Allow("cliente-a"); ok {
		t.Fatal("cliente-a ya gastó su ráfaga")
	}
	if ok, _ := limiter.Allow("cliente-b"); !ok {
		t.Error("cliente-b no debería verse afectado por cliente-a")
	}
}

func TestRateLimiterDisabled(t *testing.T) {
	limiter := newRateLimiter(0, 10)
	if limiter != nil {
		t.Fatal("un rate de 0 debe desactivar el limitador")
	}

	// Un limitador nil debe dejar pasar todo sin que quien lo use tenga que comprobarlo.
	for i := 0; i < 100; i++ {
		if ok, _ := limiter.Allow("cliente"); !ok {
			t.Fatal("un limitador desactivado no debe rechazar nada")
		}
	}
}

// Sin limpieza, cada IP que toque el servidor una sola vez dejaría una entrada viva para
// siempre: una fuga de memoria que se puede provocar desde fuera.
func TestRateLimiterCleansUpIdleBuckets(t *testing.T) {
	limiter := newRateLimiter(1000, 5)

	limiter.Allow("efimero")
	if len(limiter.buckets) != 1 {
		t.Fatalf("se esperaba 1 cubeta, hay %d", len(limiter.buckets))
	}

	// Se espera lo suficiente para que la cubeta vuelva a llenarse y quede inactiva.
	time.Sleep(20 * time.Millisecond)
	limiter.cleanup(10 * time.Millisecond)

	if len(limiter.buckets) != 0 {
		t.Errorf("la cubeta inactiva debería haberse borrado, quedan %d", len(limiter.buckets))
	}
}

// Borrar la cubeta de un cliente que todavía está limitado le regalaría una cubeta nueva y
// llena: justo lo contrario de limitarlo.
func TestCleanupKeepsBucketsStillInDebt(t *testing.T) {
	limiter := newRateLimiter(0.001, 2) // se repone muy despacio

	limiter.Allow("insistente")
	limiter.Allow("insistente")
	limiter.Allow("insistente") // rechazada: la cubeta quedó vacía

	limiter.cleanup(0) // caduca todo lo que se pueda

	if len(limiter.buckets) != 1 {
		t.Error("una cubeta a medio reponer no debe borrarse")
	}
}

func TestClientKey(t *testing.T) {
	t.Run("por defecto usa la dirección real", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		r.RemoteAddr = "203.0.113.7:54321"
		r.Header.Set("X-Forwarded-For", "1.2.3.4")

		// Sin trustProxy la cabecera se ignora: es falsificable, y hacerle caso permitiría
		// saltarse el límite rotando un valor inventado.
		if got := clientKey(r, false); got != "203.0.113.7" {
			t.Errorf("clientKey = %q, se esperaba la IP real", got)
		}
	})

	t.Run("con trustProxy toma el primero de X-Forwarded-For", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		r.RemoteAddr = "10.0.0.1:1234"
		r.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.9")

		if got := clientKey(r, true); got != "1.2.3.4" {
			t.Errorf("clientKey = %q, se esperaba el cliente original", got)
		}
	})

	t.Run("con trustProxy y sin cabecera cae a la dirección real", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		r.RemoteAddr = "10.0.0.1:1234"

		if got := clientKey(r, true); got != "10.0.0.1" {
			t.Errorf("clientKey = %q", got)
		}
	})
}

// El límite se aplica de verdad en el handler, y responde 429 con Retry-After.
func TestServerReturns429WhenRateLimited(t *testing.T) {
	server, err := New(Config{
		Repo:       seededRepo(t),
		ServerInfo: testServerInfo(),
		RateLimit:  1,
		RateBurst:  2,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	handler := server.Handler()

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`

	var lastCode int
	var retryAfter string
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "198.51.100.4:1111"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		lastCode = rec.Code
		retryAfter = rec.Header().Get("Retry-After")
	}

	if lastCode != http.StatusTooManyRequests {
		t.Fatalf("tras 5 peticiones con ráfaga 2, el código = %d, se esperaba 429", lastCode)
	}
	if retryAfter == "" {
		t.Error("un 429 debe decir cuánto esperar con Retry-After")
	}
}

// Un cliente que agota su cupo no debe afectar a otro.
func TestRateLimitDoesNotAffectOtherClients(t *testing.T) {
	server, err := New(Config{
		Repo:       seededRepo(t),
		ServerInfo: testServerInfo(),
		RateLimit:  1,
		RateBurst:  1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	handler := server.Handler()

	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	send := func(ip string) int {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = ip + ":1000"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	send("198.51.100.1")
	if code := send("198.51.100.1"); code != http.StatusTooManyRequests {
		t.Fatalf("el primer cliente debería estar limitado, código = %d", code)
	}
	if code := send("198.51.100.2"); code == http.StatusTooManyRequests {
		t.Error("el segundo cliente no debería estar limitado")
	}
}
