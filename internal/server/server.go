// Package server is the prismd HTTP API (Phase 12).
package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/version"
)

const (
	defaultScanLimit int64 = 100
	hardRowCap       int64 = 100_000
	maxBodyBytes           = 1 << 20
)

// Config is prismd runtime options.
type Config struct {
	DataDir    string
	Timeout    time.Duration
	Jobs       int
	CORSOrigin string
	BenchFile  string
}

// Server serves /health, /tables, /query, /explain, /bench.
type Server struct {
	cfg Config
}

// New builds a server. DataDir is resolved like the CLI.
func New(cfg Config) *Server {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.CORSOrigin == "" {
		cfg.CORSOrigin = "*"
	}
	return &Server{cfg: cfg}
}

// Handler is the HTTP entry point (CORS + routes).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /tables", s.handleTables)
	mux.HandleFunc("GET /tables/{name}", s.handleTable)
	mux.HandleFunc("POST /query", s.handleQuery)
	mux.HandleFunc("POST /explain", s.handleExplain)
	mux.HandleFunc("GET /bench", s.handleBench)
	return withCORS(s.cfg.CORSOrigin, mux)
}

func withCORS(origin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"version":  version.Version,
		"data_dir": s.dataDir(),
	})
}

func (s *Server) dataDir() string {
	if s.cfg.DataDir != "" {
		return s.cfg.DataDir
	}
	if d := os.Getenv("PRISM_DATA_DIR"); d != "" {
		return d
	}
	return filepath.Join("data", "tables")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

type errorBody struct {
	Error string `json:"error"`
	Pos   *int   `json:"pos,omitempty"`
}

func writeError(w http.ResponseWriter, status int, msg string, pos *int) {
	writeJSON(w, status, errorBody{Error: msg, Pos: pos})
}
