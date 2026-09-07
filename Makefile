
BINARY  := restaurant-mcp-server
PKG     := ./cmd/server
DIST    := dist

# CGO_ENABLED=0 es obligatorio: es lo que produce un binario estático,
# sin dependencias del sistema, y lo que permite compilar para otra plataforma desde ésta.
# Olvidarlo genera un binario que falla en la máquina de quien lo reciba, y el fallo aparece
# allá y no acá.
GOFLAGS := -trimpath -ldflags="-s -w"

# Se aplica sólo a los targets que producen binarios. NO puede ser global: el detector de
# carreras (-race) necesita cgo.
STATIC  := CGO_ENABLED=0

.DEFAULT_GOAL := help

## help: muestra esta ayuda
help:
	@echo "Tareas disponibles:"
	@sed -n 's/^## /  /p' $(MAKEFILE_LIST)

## build: compila el binario para esta máquina en bin/
build:
	$(STATIC) go build $(GOFLAGS) -o bin/$(BINARY) $(PKG)

## test: corre las pruebas con el detector de carreras
test:
	go test ./... -race -cover

## check: formato, análisis estático y pruebas (lo mismo que valida el CI)
check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "Sin formatear:"; echo "$$unformatted"; exit 1; fi
	go vet ./...
	go test ./... -race

## release: compila para Linux, Windows y macOS en dist/, listo para compartir
release: clean-dist
	@mkdir -p $(DIST)
	@set -e; \
	for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do \
	  os=$${target%/*}; arch=$${target#*/}; \
	  out="$(DIST)/$(BINARY)-$$os-$$arch"; \
	  if [ "$$os" = "windows" ]; then out="$$out.exe"; fi; \
	  GOOS=$$os GOARCH=$$arch $(STATIC) go build $(GOFLAGS) -o "$$out" $(PKG); \
	  echo "  $$out"; \
	done
	@echo "Listo. Comparte el archivo que corresponda al sistema de quien lo recibe."

## docker: construye la imagen
docker:
	docker build -t $(BINARY) .

## run: arranca el servidor por stdio con permiso de cocina
run: build
	MCP_USER_ROLE=cook ./bin/$(BINARY)

## serve: arranca el servidor por HTTP en local, con un token de prueba
serve: build
	MCP_AUTH_TOKENS="token-de-prueba:admin:local" ./bin/$(BINARY) --http 127.0.0.1:8080

clean-dist:
	@rm -rf $(DIST)

## clean: borra binarios y bases de datos locales
clean: clean-dist
	rm -rf bin/$(BINARY) bin/$(BINARY).exe
	rm -f bin/*.db bin/*.db-wal bin/*.db-shm

.PHONY: help build test check release docker run serve clean clean-dist
