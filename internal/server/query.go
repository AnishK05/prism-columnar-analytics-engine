package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/catalog"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/engine"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/plan"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/sql"
)

type queryRequest struct {
	SQL     string `json:"sql"`
	Engine  string `json:"engine"`
	Explain bool   `json:"explain"`
	Limit   *int64 `json:"limit"`
	Jobs    *int   `json:"jobs"`
	Analyze bool   `json:"analyze"`
}

var columnPos = regexp.MustCompile(`(?i)column (\d+)`)

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeQuery(w, r)
	if !ok {
		return
	}
	cat, err := catalog.Load(catalog.ResolveDataDir(s.cfg.DataDir))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	parsed, err := sql.Parse(req.SQL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), errorPos(err))
		return
	}
	bound, err := sql.Bind(parsed, cat)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFound(err) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error(), errorPos(err))
		return
	}
	kind, err := engine.ParseKind(req.Engine)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	capRows := resolveCap(req.Limit, bound.IsAgg)
	jobs := s.cfg.Jobs
	if req.Jobs != nil {
		jobs = *req.Jobs
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.Timeout)
	defer cancel()
	res, err := engine.Run(ctx, req.SQL, engine.Opts{
		Catalog:     cat,
		Engine:      kind,
		Jobs:        jobs,
		ReturnLimit: capRows,
	})
	if err != nil {
		writeQueryErr(w, ctx, err)
		return
	}
	defer res.Record.Release()
	if !req.Explain {
		res.Profile.Plan = nil
	}
	body, err := res.JSONCap(0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(body, '\n'))
}

func (s *Server) handleExplain(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeQuery(w, r)
	if !ok {
		return
	}
	cat, err := catalog.Load(catalog.ResolveDataDir(s.cfg.DataDir))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	parsed, err := sql.Parse(req.SQL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), errorPos(err))
		return
	}
	bound, err := sql.Bind(parsed, cat)
	if err != nil {
		status := http.StatusBadRequest
		if isNotFound(err) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error(), errorPos(err))
		return
	}
	kind, err := engine.ParseKind(req.Engine)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	jobs := engine.ResolveJobs(s.cfg.Jobs)
	if req.Jobs != nil {
		jobs = engine.ResolveJobs(*req.Jobs)
	}
	in := plan.Input{
		Table:    bound.Table,
		Where:    bound.Where,
		ScanCols: bound.ScanCols,
		GroupBy:  bound.GroupBy,
		Aggs:     bound.Aggs,
		Project:  bound.Project,
		Order:    bound.Order,
		Limit:    bound.Limit,
		IsAgg:    bound.IsAgg,
	}
	if req.Analyze {
		ctx, cancel := context.WithTimeout(r.Context(), s.cfg.Timeout)
		defer cancel()
		res, err := engine.RunInput(ctx, in, engine.Opts{Catalog: cat, Engine: kind, Jobs: jobs})
		if err != nil {
			writeQueryErr(w, ctx, err)
			return
		}
		res.Record.Release()
		writeJSON(w, http.StatusOK, map[string]any{
			"plan": res.Node.JSON(),
			"text": res.Node.Text(),
		})
		return
	}
	node := plan.Build(in)
	node.SetJobs(jobs)
	writeJSON(w, http.StatusOK, map[string]any{
		"plan": node.JSON(),
		"text": node.Text(),
	})
}

func decodeQuery(w http.ResponseWriter, r *http.Request) (queryRequest, bool) {
	var req queryRequest
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer body.Close()
	dec := json.NewDecoder(body)
	if err := dec.Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "sql is required", nil)
			return req, false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error(), nil)
		return req, false
	}
	req.SQL = strings.TrimSpace(req.SQL)
	if req.SQL == "" {
		writeError(w, http.StatusBadRequest, "sql is required", nil)
		return req, false
	}
	return req, true
}

func resolveCap(limit *int64, isAgg bool) int64 {
	if limit == nil {
		if isAgg {
			return hardRowCap
		}
		return defaultScanLimit
	}
	if *limit <= 0 {
		return hardRowCap
	}
	if *limit > hardRowCap {
		return hardRowCap
	}
	return *limit
}

func writeQueryErr(w http.ResponseWriter, ctx context.Context, err error) {
	if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
		writeError(w, http.StatusGatewayTimeout, "query timeout", nil)
		return
	}
	msg := err.Error()
	status := http.StatusBadRequest
	if isNotFound(err) {
		status = http.StatusNotFound
	}
	writeError(w, status, msg, errorPos(err))
}

func errorPos(err error) *int {
	if err == nil {
		return nil
	}
	m := columnPos.FindStringSubmatch(err.Error())
	if len(m) < 2 {
		return nil
	}
	n, convErr := strconv.Atoi(m[1])
	if convErr != nil {
		return nil
	}
	return &n
}

func (s *Server) handleBench(w http.ResponseWriter, r *http.Request) {
	path := s.cfg.BenchFile
	if path == "" {
		path = findBenchSample()
	}
	if path == "" {
		writeError(w, http.StatusNotFound, "bench sample not found (run prism bench or check bench/results/sample.json)", nil)
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

func findBenchSample() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	dir := wd
	for i := 0; i < 8; i++ {
		cand := filepath.Join(dir, "bench", "results", "sample.json")
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
