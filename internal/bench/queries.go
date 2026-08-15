package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Query is one PrismBench entry from bench/queries.json.
type Query struct {
	ID       string `json:"id"`
	File     string `json:"file"`
	Ordered  bool   `json:"ordered"`
	Oracle   bool   `json:"oracle"`
	Showcase string `json:"showcase"`
	SQL      string `json:"-"`
}

type queryFile struct {
	Queries []Query `json:"queries"`
}

// LoadQueries reads bench/queries.json and the SQL files it points at.
func LoadQueries(path string) ([]Query, error) {
	if path == "" {
		path = filepath.Join("bench", "queries.json")
	}
	resolved, err := resolveQueriesPath(path)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("read queries: %w", err)
	}
	var spec queryFile
	if err := json.Unmarshal(b, &spec); err != nil {
		return nil, fmt.Errorf("parse queries: %w", err)
	}
	root := filepath.Dir(resolved)
	if filepath.Base(root) == "bench" {
		root = filepath.Dir(root)
	}
	for i := range spec.Queries {
		q := &spec.Queries[i]
		sqlPath := q.File
		if !filepath.IsAbs(sqlPath) {
			sqlPath = filepath.Join(root, sqlPath)
		}
		src, err := os.ReadFile(sqlPath)
		if err != nil {
			return nil, fmt.Errorf("query %s: %w", q.ID, err)
		}
		q.SQL = string(src)
	}
	return spec.Queries, nil
}

func resolveQueriesPath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	if _, err := os.Stat(path); err == nil {
		abs, err := filepath.Abs(path)
		if err != nil {
			return path, nil
		}
		return abs, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("read queries %s: %w", path, err)
	}
	dir := wd
	for i := 0; i < 8; i++ {
		cand := filepath.Join(dir, path)
		if _, err := os.Stat(cand); err == nil {
			return cand, nil
		}
		if filepath.Base(path) != "queries.json" {
			cand = filepath.Join(dir, "bench", "queries.json")
			if _, err := os.Stat(cand); err == nil {
				return cand, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("read queries: %s not found (run from the repo root)", path)
}

// ScaleDataDir maps --scale to a tables root.
func ScaleDataDir(scale, override string) string {
	if override != "" {
		return override
	}
	switch scale {
	case "testdata":
		return filepath.Join("testdata", "tables")
	default:
		return filepath.Join("data", "tables")
	}
}
