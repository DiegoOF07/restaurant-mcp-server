# syntax=docker/dockerfile:1

# ---------- etapa de compilación ----------
FROM golang:1.25-alpine AS build

WORKDIR /src

# Las dependencias se copian y descargan antes que el código: mientras go.mod/go.sum no
# cambien, Docker reutiliza esta capa y no vuelve a bajar nada.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 produce un binario estático, que es lo que permite la imagen final sin
# sistema operativo. El driver de SQLite es Go puro, así que no hace falta ninguna
# biblioteca de C.
# -ldflags="-s -w" quita la tabla de símbolos y la información de depuración: ~30% menos.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/restaurant-mcp-server ./cmd/server

# El directorio de datos se prepara ACÁ porque la imagen final no tiene shell: no hay forma
# de hacer mkdir ni chown en ella. 65532 es el uid del usuario "nonroot" de distroless.
RUN mkdir -p /data && chown 65532:65532 /data

# ---------- imagen final ----------
# distroless/static: no trae shell, ni gestor de paquetes, ni siquiera libc. Nada que un
# atacante pueda usar si lograra ejecutar algo dentro, y nada que haya que parchear cada mes.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/restaurant-mcp-server /usr/local/bin/restaurant-mcp-server

# Sin este COPY, /data lo crearía Docker como propiedad de root y el proceso, que corre como
# nonroot, no podría escribir la base: el servidor arrancaría y moriría en el primer acceso.
COPY --from=build --chown=65532:65532 /data /data

# La base vive en un volumen: si se quedara en la capa de la imagen, cada redespliegue
# borraría el inventario.
VOLUME ["/data"]
ENV MCP_DB_PATH=/data/restaurant.db

# nonroot (uid 65532) viene de la imagen base. Un servidor que no necesita root no debe
# correr como root.
USER nonroot:nonroot

EXPOSE 8080

# 0.0.0.0 es correcto DENTRO del contenedor: quien decide la exposición real es el mapeo de
# puertos. Aun así el servidor se niega a arrancar en una dirección pública sin
# MCP_AUTH_TOKENS, así que un despliegue sin credenciales falla de inmediato.
ENTRYPOINT ["/usr/local/bin/restaurant-mcp-server"]
CMD ["--http", "0.0.0.0:8080"]
