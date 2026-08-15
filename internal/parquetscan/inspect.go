package parquetscan

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/metadata"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
)

// FileInfo is footer metadata without decoding column pages.
type FileInfo struct {
	Path            string
	NumRows         int64
	NumRowGroups    int
	SchemaFields    []string
	SchemaTypes     []string
	RowGroups       []RowGroupInfo
	CompressedBytes int64
}

// RowGroupInfo is per-row-group footer stats.
type RowGroupInfo struct {
	Index           int
	NumRows         int64
	CompressedBytes int64
	Columns         []ColumnChunkInfo
}

// ColumnChunkInfo is per-column-chunk footer stats.
type ColumnChunkInfo struct {
	Name            string
	PhysicalType    string
	CompressedBytes int64
	NumValues       int64
	NullCount       *int64
	Min             string
	Max             string
	HasMinMax       bool
}

// InspectFile reads Parquet footer metadata (schema, row groups, min/max).
func InspectFile(path string) (*FileInfo, error) {
	rdr, err := file.OpenParquetFile(path, false)
	if err != nil {
		return nil, fmt.Errorf("open parquet %s: %w", path, err)
	}
	defer rdr.Close()

	arrowRdr, err := pqarrow.NewFileReader(rdr, pqarrow.ArrowReadProperties{}, memory.DefaultAllocator)
	if err != nil {
		return nil, fmt.Errorf("arrow reader %s: %w", path, err)
	}
	schema, err := arrowRdr.Schema()
	if err != nil {
		return nil, fmt.Errorf("schema %s: %w", path, err)
	}

	meta := rdr.MetaData()
	info := &FileInfo{
		Path:         path,
		NumRows:      meta.GetNumRows(),
		NumRowGroups: rdr.NumRowGroups(),
	}
	for i := 0; i < schema.NumFields(); i++ {
		f := schema.Field(i)
		info.SchemaFields = append(info.SchemaFields, f.Name)
		info.SchemaTypes = append(info.SchemaTypes, f.Type.String())
	}

	for i := 0; i < rdr.NumRowGroups(); i++ {
		rgMeta := meta.RowGroup(i)
		rg := RowGroupInfo{
			Index:           i,
			NumRows:         rgMeta.NumRows(),
			CompressedBytes: rgMeta.TotalCompressedSize(),
		}
		info.CompressedBytes += rg.CompressedBytes
		for c := 0; c < rgMeta.NumColumns(); c++ {
			chunk, err := rgMeta.ColumnChunk(c)
			if err != nil {
				return nil, fmt.Errorf("%s row group %d col %d: %w", path, i, c, err)
			}
			col := ColumnChunkInfo{
				Name:            strings.Join(chunk.PathInSchema(), "."),
				PhysicalType:    chunk.Type().String(),
				CompressedBytes: chunk.TotalCompressedSize(),
				NumValues:       chunk.NumValues(),
			}
			stats, err := chunk.Statistics()
			if err != nil {
				return nil, err
			}
			if stats != nil {
				if stats.HasNullCount() {
					n := stats.NullCount()
					col.NullCount = &n
				}
				if stats.HasMinMax() {
					col.HasMinMax = true
					col.Min = formatStatValue(stats.Type(), stats.EncodeMin())
					col.Max = formatStatValue(stats.Type(), stats.EncodeMax())
				}
			}
			rg.Columns = append(rg.Columns, col)
		}
		info.RowGroups = append(info.RowGroups, rg)
	}
	return info, nil
}

func formatStatValue(typ parquet.Type, raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	v := metadata.GetStatValue(typ, raw)
	switch x := v.(type) {
	case int64:
		if x > 1_000_000_000_000 && x < 10_000_000_000_000 {
			return fmt.Sprintf("%d (%s)", x, time.UnixMilli(x).UTC().Format(time.RFC3339))
		}
		return fmt.Sprintf("%d", x)
	case int32:
		return fmt.Sprintf("%d", x)
	case float32:
		return fmt.Sprintf("%g", x)
	case float64:
		return fmt.Sprintf("%g", x)
	case bool:
		return fmt.Sprintf("%v", x)
	case []byte:
		if isPrintable(x) {
			return string(x)
		}
		return fmt.Sprintf("0x%x", x)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func isPrintable(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c < 32 || c > 126 {
			return false
		}
	}
	return true
}

func formatBytes(n int64) string {
	if n < 0 {
		n = -n
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	if f >= 100 {
		return fmt.Sprintf("%.0f %s", math.Round(f), units[i])
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

// FormatBytes is exported for the CLI.
func FormatBytes(n int64) string { return formatBytes(n) }
