package httpmcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

const initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`

// listenOnRandomPort arranca el servidor en un puerto libre y devuelve su URL base junto con
// el canal por el que ListenAndServe reportará cómo terminó.
func listenOnRandomPort(t *testing.T) (*Server, string, <-chan error) {
	t.Helper()

	// Se reserva un puerto y se cierra enseguida para conocer uno libre. Basta para una
	// prueba; en producción el servidor abre el suyo directamente.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("no se pudo reservar un puerto: %v", err)
	}
	addr := probe.Addr().String()
	probe.Close()

	server, err := New(Config{
		Addr:       addr,
		Repo:       seededRepo(t),
		ServerInfo: testServerInfo(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()

	base := "http://" + addr
	for i := 0; i < 100; i++ {
		if resp, err := http.Get(base + "/health"); err == nil {
			resp.Body.Close()
			return server, base, done
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("el servidor no llegó a escuchar")
	return nil, "", nil
}

// Un cierre ordenado devuelve ErrServerClosed, no un error real. Confundirlos haría que el
// proceso saliera con código 1 en cada redespliegue normal.
func TestShutdownReturnsErrServerClosed(t *testing.T) {
	server, _, done := listenOnRandomPort(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("ListenAndServe devolvió %v, se esperaba http.ErrServerClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ListenAndServe no retornó tras el cierre")
	}
}

// Tras el cierre el puerto queda libre de inmediato: si no, un redespliegue chocaría con
// "address already in use" al intentar levantar la instancia nueva.
func TestShutdownFreesThePort(t *testing.T) {
	server, base, done := listenOnRandomPort(t)
	addr := strings.TrimPrefix(base, "http://")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	<-done

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("el puerto siguió ocupado tras cerrar: %v", err)
	}
	listener.Close()
}

func TestShutdownWaitsForInFlightRequest(t *testing.T) {
	server, base, done := listenOnRandomPort(t)
	addr := strings.TrimPrefix(base, "http://")

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("no se pudo conectar: %v", err)
	}
	defer conn.Close()

	headers := "POST /mcp HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Content-Type: application/json\r\n" +
		"Content-Length: " + strconv.Itoa(len(initBody)) + "\r\n" +
		"Expect: 100-continue\r\n" +
		"\r\n"

	if _, err := conn.Write([]byte(headers)); err != nil {
		t.Fatalf("no se pudieron enviar las cabeceras: %v", err)
	}

	reader := bufio.NewReader(conn)
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("no llegó la confirmación 100 Continue: %v", err)
	}
	if !strings.HasPrefix(line, "HTTP/1.1 100") {
		t.Fatalf("se esperaba 100 Continue, llegó: %q", line)
	}
	// Se consume la línea en blanco que cierra la respuesta provisional.
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatalf("respuesta provisional mal formada: %v", err)
	}

	// A partir de acá el handler está ejecutándose, bloqueado leyendo el cuerpo.
	shutdownDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownDone <- server.Shutdown(ctx)
	}()

	if _, err := conn.Write([]byte(initBody)); err != nil {
		t.Fatalf("no se pudo enviar el cuerpo: %v", err)
	}

	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("la respuesta se cortó durante el cierre: %v", err)
	}
	if !strings.HasPrefix(status, "HTTP/1.1 200") {
		t.Fatalf("la petición en vuelo no recibió 200: %q", status)
	}

	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("el cuerpo se cortó durante el cierre: %v", err)
	}

	// El cuerpo debe ser JSON completo, no una respuesta truncada a media escritura.
	_, body, found := strings.Cut(string(rest), "\r\n\r\n")
	if !found {
		t.Fatalf("respuesta sin cuerpo:\n%s", rest)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &parsed); err != nil {
		t.Fatalf("el cuerpo llegó incompleto o corrupto: %v\n%s", err, body)
	}
	if parsed["jsonrpc"] != "2.0" {
		t.Errorf("respuesta inesperada: %v", parsed)
	}

	if err := <-shutdownDone; err != nil {
		t.Errorf("Shutdown devolvió %v", err)
	}
	if err := <-done; !errors.Is(err, http.ErrServerClosed) {
		t.Errorf("ListenAndServe devolvió %v", err)
	}
}

// Tras iniciar el cierre no se aceptan conexiones nuevas.
func TestShutdownStopsAcceptingNewRequests(t *testing.T) {
	server, base, done := listenOnRandomPort(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	<-done

	client := &http.Client{Timeout: 2 * time.Second}
	if resp, err := client.Post(base+"/mcp", "application/json", strings.NewReader(initBody)); err == nil {
		resp.Body.Close()
		t.Error("el servidor siguió aceptando peticiones tras cerrarse")
	}
}
