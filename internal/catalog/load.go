package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/parquetscan"
)

// Field is one column in a unified table schema.
type Field struct {
	Name string
	Type string
}

// ColStats is cached zone-map data for one column in one row group.
type ColStats struct {
	Name            string
	NullCount       *int64
	HasMinMax       bool
	Min             string
	Max             string
	MinBound        parquetscan.Bound
	MaxBound        parquetscan.Bound
	CompressedBytes int64
	NumValues       int64
}

// RowGroup is cached stats for one Parquet row group.
type RowGroup struct {
	File            string
	FileIndex       int
	Index           int
	NumRows         int64
	CompressedBytes int64
	Columns         []ColStats
}

// Table is an in-memory catalog entry for a directory of Parquet files.
type Table struct {
	Name            string
	Dir             string
	Fields          []Field
	Files           []string
	NumRows         int64
	NumRowGroups    int
	CompressedBytes int64
	RowGroups       []RowGroup
}

// Catalog is rebuilt by walking a data dir. Nothing is persisted.
type Catalog struct {
	DataDir string
	tables  map[string]*Table
	order   []string
}

// Load walks dataDir for table subdirectories of *.parquet files, unifies
// schemas, and caches per-row-group min/max stats from footers.
func Load(dataDir string) (*Catalog, error) {
	c := &Catalog{
		DataDir: dataDir,
		tables:  map[string]*Table{},
	}
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, fmt.Errorf("open data dir %s: %w", dataDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		files, err := TableFiles(dataDir, name)
		if err != nil {
			continue // empty dir is not a table
		}
		tbl, err := loadTable(name, TableDir(dataDir, name), files)
		if err != nil {
			return nil, err
		}
		c.tables[name] = tbl
		c.order = append(c.order, name)
	}
	sort.Strings(c.order)
	return c, nil
}

func loadTable(name, dir string, files []string) (*Table, error) {
	tbl := &Table{Name: name, Dir: dir, Files: files}
	var fingerprint string
	for fi, path := range files {
		info, err := parquetscan.InspectFile(path)
		if err != nil {
			return nil, fmt.Errorf("table %q: %w", name, err)
		}
		fp := schemaFingerprint(info.SchemaFields, info.SchemaTypes)
		if fi == 0 {
			fingerprint = fp
			for i := range info.SchemaFields {
				tbl.Fields = append(tbl.Fields, Field{Name: info.SchemaFields[i], Type: info.SchemaTypes[i]})
			}
		} else if fp != fingerprint {
			return nil, fmt.Errorf("table %q: schema mismatch in %s (expected %s)", name, path, fingerprint)
		}
		tbl.NumRows += info.NumRows
		tbl.NumRowGroups += info.NumRowGroups
		tbl.CompressedBytes += info.CompressedBytes
		for _, rg := range info.RowGroups {
			out := RowGroup{
				File:            path,
				FileIndex:       fi,
				Index:           rg.Index,
				NumRows:         rg.NumRows,
				CompressedBytes: rg.CompressedBytes,
			}
			for _, col := range rg.Columns {
				out.Columns = append(out.Columns, ColStats{
					Name:            col.Name,
					NullCount:       col.NullCount,
					HasMinMax:       col.HasMinMax,
					Min:             col.Min,
					Max:             col.Max,
					MinBound:        col.MinBound,
					MaxBound:        col.MaxBound,
					CompressedBytes: col.CompressedBytes,
					NumValues:       col.NumValues,
				})
			}
			tbl.RowGroups = append(tbl.RowGroups, out)
		}
	}
	return tbl, nil
}

func schemaFingerprint(names, types []string) string {
	parts := make([]string, len(names))
	for i := range names {
		parts[i] = names[i] + ":" + types[i]
	}
	return strings.Join(parts, ",")
}

// Names returns sorted table names.
func (c *Catalog) Names() []string {
	out := make([]string, len(c.order))
	copy(out, c.order)
	return out
}

// Table returns a loaded table or an error if it is missing.
func (c *Catalog) Table(name string) (*Table, error) {
	t, ok := c.tables[name]
	if !ok {
		if len(c.order) == 0 {
			return nil, fmt.Errorf("table %q not found in %s (no tables loaded)", name, c.DataDir)
		}
		return nil, fmt.Errorf("table %q not found (have %s)", name, strings.Join(c.order, ", "))
	}
	return t, nil
}

// FieldType returns the Arrow type string for a column, if present.
func (t *Table) FieldType(name string) (string, bool) {
	for _, f := range t.Fields {
		if f.Name == name {
			return f.Type, true
		}
	}
	return "", false
}

// ColStats returns stats for a column in a row group, if present.
func (rg RowGroup) ColStats(name string) (ColStats, bool) {
	for _, c := range rg.Columns {
		if c.Name == name {
			return c, true
		}
	}
	return ColStats{}, false
}

// Clustering reports whether a column's min/max is non-decreasing across
// row groups (the Snowflake micro-partition story). ok is false if any
// row group is missing min/max or the sequence goes backwards.
func (t *Table) Clustering(col string) (ok bool, detail string) {
	var prevMax parquetscan.Bound
	var havePrev bool
	compared := 0
	for i, rg := range t.RowGroups {
		st, found := rg.ColStats(col)
		if !found || !st.HasMinMax {
			return false, fmt.Sprintf("row group %d (%s) has no min/max for %s", i, filepath.Base(rg.File), col)
		}
		if havePrev {
			cmp, err := compareBounds(prevMax, st.MinBound)
			if err != nil {
				return false, err.Error()
			}
			compared++
			if cmp > 0 {
				return false, fmt.Sprintf("not clustered: %s max=%s then %s min=%s",
					filepath.Base(t.RowGroups[i-1].File), prevMax.String(), filepath.Base(rg.File), st.MinBound.String())
			}
		}
		prevMax = st.MaxBound
		havePrev = true
	}
	if compared == 0 {
		return true, "single row group"
	}
	return true, fmt.Sprintf("non-decreasing min/max across %d row-group boundaries", compared)
}

func compareBounds(a, b parquetscan.Bound) (int, error) {
	return a.Cmp(b)
}

// TimestampRangeMS returns min(min) and max(max) for an INT64 timestamp
// column across all row groups (unix milliseconds).
func (t *Table) TimestampRangeMS(col string) (minMS, maxMS int64, ok bool) {
	var have bool
	for _, rg := range t.RowGroups {
		st, found := rg.ColStats(col)
		if !found || !st.HasMinMax || st.MinBound.Kind != parquetscan.BoundInt64 {
			continue
		}
		if !have {
			minMS, maxMS = st.MinBound.I64, st.MaxBound.I64
			have = true
			continue
		}
		if st.MinBound.I64 < minMS {
			minMS = st.MinBound.I64
		}
		if st.MaxBound.I64 > maxMS {
			maxMS = st.MaxBound.I64
		}
	}
	return minMS, maxMS, have
}
