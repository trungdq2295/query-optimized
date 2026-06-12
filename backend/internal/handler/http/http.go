// Package http is the web delivery adapter: it turns HTTP requests into
// use-case calls, mirroring what the cli handler does for the terminal. The
// SERVER holds the engine + connection string (a secret), so request bodies
// carry only the SQL — never a DSN. This is what makes a public hosted instance
// safe: one fixed demo database, credentials never crossing the wire.
//
// Endpoints:
//
//	GET  /health    — liveness + which mode/engine the server runs
//	POST /optimize  — Server-Sent Events: progress steps, then a final result
//	POST /recheck   — prove an applied system change against its baseline
//	POST /explain   — diagnose only (EXPLAIN text)
package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/trudoan/query-optimizer/internal/domain"
	"github.com/trudoan/query-optimizer/internal/usecase/optimize"
)

// Server wires the optimize use case to HTTP. engine + the DSN behind uc are
// fixed at construction; the mode string is reported for diagnostics only.
type Server struct {
	uc        *optimize.UseCase
	engine    string
	mode      string // "hosted" | "local" — informational
	timeoutS  int
	origin    string // CORS allow-origin
	staticDir string // optional: serve the built frontend from here
}

// New builds the server. origin is the CORS Access-Control-Allow-Origin value
// (e.g. the frontend dev URL, or "*"). staticDir, if non-empty, makes this one
// process also serve the built frontend (SPA) — so a packaged local app or a
// hosted deploy needs no separate web server.
func New(uc *optimize.UseCase, engine, mode, origin, staticDir string, timeoutS int) *Server {
	return &Server{uc: uc, engine: engine, mode: mode, timeoutS: timeoutS, origin: origin, staticDir: staticDir}
}

// Handler returns the router with CORS applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/optimize", s.handleOptimize)
	mux.HandleFunc("/recheck", s.handleRecheck)
	mux.HandleFunc("/explain", s.handleExplain)
	if s.staticDir != "" {
		// Catch-all: anything not matched above is a frontend asset (or the SPA
		// index for client-side routes). API routes win because they are exact.
		mux.HandleFunc("/", s.serveStatic)
	}
	return s.cors(mux)
}

// serveStatic serves a file from staticDir, falling back to index.html so the
// single-page app loads on any path. http.ServeFile rejects ".." traversal.
func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	clean := filepath.Clean(r.URL.Path)
	path := filepath.Join(s.staticDir, clean)
	if fi, err := os.Stat(path); err != nil || fi.IsDir() {
		path = filepath.Join(s.staticDir, "index.html")
	}
	http.ServeFile(w, r, path)
}

// cors adds permissive-but-scoped CORS headers and answers preflight requests.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", s.origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"mode":   s.mode,
		"engine": s.engine,
	})
}

// handleOptimize streams the run as Server-Sent Events: one "progress" event
// per phase, then a terminal "result" event (or "error").
func (s *Server) handleOptimize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errBody("POST only"))
		return
	}
	var body struct {
		SQL string `json:"sql"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SQL == "" {
		writeJSON(w, http.StatusBadRequest, errBody("body must be {\"sql\": \"...\"}"))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errBody("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	emit := func(p domain.Progress) {
		writeSSE(w, "progress", p)
		flusher.Flush()
	}

	res, err := s.uc.Optimize(r.Context(), s.engine, body.SQL, s.timeoutS, emit)
	if err != nil {
		writeSSE(w, "error", errBody(err.Error()))
		flusher.Flush()
		return
	}
	writeSSE(w, "result", res)
	flusher.Flush()
}

func (s *Server) handleRecheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errBody("POST only"))
		return
	}
	var body struct {
		BaselineID string `json:"baseline_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.BaselineID == "" {
		writeJSON(w, http.StatusBadRequest, errBody("body must be {\"baseline_id\": \"...\"}"))
		return
	}
	res, err := s.uc.Recheck(r.Context(), body.BaselineID, s.timeoutS)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleExplain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, errBody("POST only"))
		return
	}
	var body struct {
		SQL     string `json:"sql"`
		Analyze bool   `json:"analyze"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SQL == "" {
		writeJSON(w, http.StatusBadRequest, errBody("body must be {\"sql\": \"...\"}"))
		return
	}
	plan, err := s.uc.Explain(r.Context(), s.engine, body.SQL, body.Analyze, s.timeoutS)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"plan": plan})
}

// --- small helpers ---------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeSSE writes one Server-Sent Event frame: "event: <name>\ndata: <json>\n\n".
func writeSSE(w http.ResponseWriter, event string, v any) {
	data, _ := json.Marshal(v)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}

func errBody(msg string) map[string]string { return map[string]string{"error": msg} }
