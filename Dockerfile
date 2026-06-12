# Multi-stage build for the HOSTED deployment (case 3): one image that serves
# both the built web UI and the API. In hosted mode the server holds the
# Anthropic API key (QOPT_API_KEY) and points at a fixed demo database, so the
# public page needs no AI tools and no credentials of its own.
#
#   docker build -t query-optimizer .
#   docker run -p 8080:8080 -e QOPT_MODE=hosted -e QOPT_API_KEY=sk-ant-... \
#              -e QOPT_ENGINE=sqlite -e QOPT_DSN=/data/qopt-demo.db query-optimizer
#
# (For a one-command MySQL-backed stack with the data pre-seeded, use
#  docker-compose.yml instead.)

# --- stage 1: build the web UI --------------------------------------------
FROM node:20-alpine AS web
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# --- stage 2: build the Go server -----------------------------------------
FROM golang:1.25-alpine AS server
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# CGO off → fully static binaries (modernc.org/sqlite is pure Go).
RUN CGO_ENABLED=0 go build -o /out/qopt-server ./cmd/qopt-server \
 && CGO_ENABLED=0 go build -o /out/qopt-seed   ./cmd/qopt-seed

# --- stage 3: minimal runtime --------------------------------------------
FROM alpine:3.20
WORKDIR /app
COPY --from=server /out/qopt-server /app/qopt-server
COPY --from=server /out/qopt-seed   /app/qopt-seed
COPY --from=web    /web/dist        /app/web
COPY examples/                      /app/examples/

# Defaults suit a hosted deploy; override QOPT_MODE/QOPT_API_KEY/QOPT_DSN at run.
ENV QOPT_ENGINE=sqlite \
    QOPT_DSN=/data/qopt-demo.db \
    QOPT_STATIC_DIR=/app/web \
    QOPT_ADDR=:8080
EXPOSE 8080

# Seed the SQLite demo db on first boot if it is missing, then serve. For MySQL
# deploys the compose file seeds instead and this is a no-op (db file unused).
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh
ENTRYPOINT ["/app/docker-entrypoint.sh"]
