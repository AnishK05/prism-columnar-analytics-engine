package server

import (
	"net/http"
	"strings"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/catalog"
)

func (s *Server) handleTables(w http.ResponseWriter, r *http.Request) {
	cat, err := catalog.Load(catalog.ResolveDataDir(s.cfg.DataDir))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	items := make([]catalog.TableListItem, 0, len(cat.Names()))
	for _, name := range cat.Names() {
		tbl, err := cat.Table(name)
		if err != nil {
			continue
		}
		items = append(items, tbl.ListItem())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data_dir": cat.DataDir,
		"tables":   items,
	})
}

func (s *Server) handleTable(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	cat, err := catalog.Load(catalog.ResolveDataDir(s.cfg.DataDir))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}
	tbl, err := cat.Table(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, tbl.Info())
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "not found")
}
