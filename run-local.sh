#!/usr/bin/env bash
#
# run-local.sh — one command to run the query-optimizer on your own machine.
#
# For the analyst / non-CLI user: it builds the app, loads a demo database,
# starts the server in LOCAL mode (it drives the AI agent already installed on
# your machine — claude or cursor — so NO API key is needed), and opens your
# browser. Just run it and use the page.
#
#   ./run-local.sh
#
# Re-running is cheap: it skips steps whose output already exists (npm install,
# the demo database). Delete ./qopt-demo.db to start the data fresh.
set -euo pipefail

# Always operate from the repo root (this script's own directory), so it works
# no matter where it is launched from (e.g. a double-click).
cd "$(dirname "$0")"

PORT="${QOPT_PORT:-8080}"
DB="./qopt-demo.db"
URL="http://localhost:${PORT}"

say()  { printf "\033[1;34m▸\033[0m %s\n" "$1"; }
die()  { printf "\033[1;31m✗ %s\033[0m\n" "$1" >&2; exit 1; }

# --- 1. prerequisites ------------------------------------------------------
command -v go   >/dev/null 2>&1 || die "Go is not installed. Get it at https://go.dev/dl/"
command -v node >/dev/null 2>&1 || die "Node.js is not installed. Get it at https://nodejs.org/"
command -v npm  >/dev/null 2>&1 || die "npm is not installed (it ships with Node.js)."

# An agent CLI is what LOCAL mode uses instead of an API key. Warn but proceed:
# the page still loads; the user can install one and retry without rebuilding.
if   command -v claude       >/dev/null 2>&1; then say "AI agent: claude"
elif command -v cursor-agent >/dev/null 2>&1; then say "AI agent: cursor-agent"
elif command -v agent        >/dev/null 2>&1; then say "AI agent: agent"
else
  printf "\033[1;33m! No AI agent CLI found (claude / cursor-agent).\033[0m\n"
  printf "  The page will load, but optimizing needs one. Install 'claude' then re-run.\n"
fi

# --- 2. build the frontend -------------------------------------------------
say "Building the web UI…"
( cd frontend
  [ -d node_modules ] || npm install
  npm run build )

# --- 3. build the backend binaries ----------------------------------------
say "Building the server…"
( cd backend
  go build -o ../qopt-server ./cmd/qopt-server
  go build -o ../qopt-seed   ./cmd/qopt-seed )

# --- 4. seed the demo database (once) -------------------------------------
if [ ! -f "$DB" ]; then
  say "Loading demo data (5k customers, 300k orders)…"
  ./qopt-seed -engine sqlite -dsn "$DB" -file examples/seed-sqlite.sql
else
  say "Demo database already present ($DB)."
fi

# --- 5. start the server + open the browser -------------------------------
say "Starting on ${URL} …"
( sleep 1
  if   command -v open     >/dev/null 2>&1; then open "$URL"
  elif command -v xdg-open >/dev/null 2>&1; then xdg-open "$URL"
  else printf "  Open %s in your browser.\n" "$URL"
  fi ) &

exec env \
  QOPT_MODE=local \
  QOPT_ENGINE=sqlite \
  QOPT_DSN="$DB" \
  QOPT_STATIC_DIR=frontend/dist \
  QOPT_ADDR=":${PORT}" \
  ./qopt-server
