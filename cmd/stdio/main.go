package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/DiegoOF07/restaurant-mcp-server/internal/jsonrpc"
	"github.com/DiegoOF07/restaurant-mcp-server/internal/mcp"
)

func main() {
	logger := log.New(os.Stderr, "[restaurant-mcp-server] ", log.LstdFlags)

	dispatcher := jsonrpc.NewDispatcher()
	lifecycle := mcp.NewLifecycle(mcp.ServerInfo{
		Name:    "restaurant-mcp-server",
		Version: "0.1.0",
	})
	lifecycle.Register(dispatcher)

	if err := run(os.Stdin, os.Stdout, dispatcher, logger); err != nil && err != io.EOF {
		logger.Fatalf("fallo fatal en el bucle stdio: %v", err)
	}
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
			// Sin un id válido no hay a quién responder, se registra en stderr y se continúa
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
