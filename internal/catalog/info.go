package catalog

// FieldJSON is one schema field in describe / HTTP payloads.
type FieldJSON struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// TableInfo is the JSON describe payload shared by the CLI and prismd.
type TableInfo struct {
	Table           string      `json:"table"`
	Dir             string      `json:"dir"`
	Files           int         `json:"files"`
	Rows            int64       `json:"rows"`
	RowGroups       int         `json:"row_groups"`
	CompressedBytes int64       `json:"compressed_bytes"`
	MinTSMs         *int64      `json:"min_ts_ms,omitempty"`
	MaxTSMs         *int64      `json:"max_ts_ms,omitempty"`
	TSClustering    string      `json:"ts_clustering,omitempty"`
	Schema          []FieldJSON `json:"schema"`
}

// TableListItem is one row of GET /tables.
type TableListItem struct {
	Name            string `json:"name"`
	Files           int    `json:"files"`
	Rows            int64  `json:"rows"`
	RowGroups       int    `json:"row_groups"`
	CompressedBytes int64  `json:"compressed_bytes"`
}

// Info is the describe JSON for one table.
func (t *Table) Info() TableInfo {
	out := TableInfo{
		Table:           t.Name,
		Dir:             t.Dir,
		Files:           len(t.Files),
		Rows:            t.NumRows,
		RowGroups:       t.NumRowGroups,
		CompressedBytes: t.CompressedBytes,
	}
	for _, f := range t.Fields {
		out.Schema = append(out.Schema, FieldJSON{Name: f.Name, Type: f.Type})
	}
	if minTS, maxTS, ok := t.TimestampRangeMS("ts"); ok {
		out.MinTSMs = &minTS
		out.MaxTSMs = &maxTS
	}
	if _, ok := t.FieldType("ts"); ok {
		_, detail := t.Clustering("ts")
		out.TSClustering = detail
	}
	return out
}

// ListItem is the compact GET /tables row.
func (t *Table) ListItem() TableListItem {
	return TableListItem{
		Name:            t.Name,
		Files:           len(t.Files),
		Rows:            t.NumRows,
		RowGroups:       t.NumRowGroups,
		CompressedBytes: t.CompressedBytes,
	}
}
