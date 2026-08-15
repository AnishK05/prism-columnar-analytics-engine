package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/catalog"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/parquetscan"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/version"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "prism: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printHelp(os.Stdout)
		return nil
	}
	switch args[0] {
	case "help", "-h", "--help":
		printHelp(os.Stdout)
		return nil
	case "version", "-v", "--version":
		fmt.Printf("prism %s\n", version.Version)
		return nil
	case "scan":
		return cmdScan(args[1:])
	case "inspect":
		return cmdInspect(args[1:])
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], helpText)
	}
}

const helpText = `prism is a miniature vectorized OLAP engine.

Usage:
  prism <command> [flags]

Commands:
  version    Print version
  inspect    Dump Parquet schema, row groups, and min/max stats
  scan       Read selected columns and print rows
  help       Show this message

Examples:
  go run ./cmd/prism version
  go run ./cmd/prism inspect --table events
  go run ./cmd/prism scan --table events --columns country,amount_cents --limit 5
`

func printHelp(w io.Writer) {
	fmt.Fprint(w, helpText)
}

func cmdInspect(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	table := fs.String("table", "", "table name under the data dir")
	dataDir := fs.String("data-dir", "", "tables root (default PRISM_DATA_DIR or ./data/tables)")
	filePath := fs.String("file", "", "inspect a single parquet file instead of a table")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var files []string
	var err error
	if *filePath != "" {
		files = []string{*filePath}
	} else {
		if *table == "" {
			return fmt.Errorf("inspect: --table or --file is required")
		}
		files, err = catalog.TableFiles(catalog.ResolveDataDir(*dataDir), *table)
		if err != nil {
			return err
		}
		fmt.Printf("table: %s\n", *table)
		fmt.Printf("data dir: %s\n", catalog.ResolveDataDir(*dataDir))
	}

	var totalRows int64
	var totalRG int
	var totalBytes int64
	for _, p := range files {
		info, err := parquetscan.InspectFile(p)
		if err != nil {
			return err
		}
		totalRows += info.NumRows
		totalRG += info.NumRowGroups
		totalBytes += info.CompressedBytes
		fmt.Printf("\nfile %s\n", p)
		fmt.Printf("  rows: %d  row groups: %d  compressed: %s\n", info.NumRows, info.NumRowGroups, parquetscan.FormatBytes(info.CompressedBytes))
		fmt.Printf("  schema:\n")
		for i, name := range info.SchemaFields {
			fmt.Printf("    %-16s %s\n", name, info.SchemaTypes[i])
		}
		for _, rg := range info.RowGroups {
			fmt.Printf("  row group %d  rows=%d  compressed=%s\n", rg.Index, rg.NumRows, parquetscan.FormatBytes(rg.CompressedBytes))
			for _, c := range rg.Columns {
				nulls := "-"
				if c.NullCount != nil {
					nulls = strconv.FormatInt(*c.NullCount, 10)
				}
				minmax := ""
				if c.HasMinMax {
					minmax = fmt.Sprintf("  min=%s  max=%s", c.Min, c.Max)
				}
				fmt.Printf("    %-16s %s  values=%d  nulls=%s  %s%s\n",
					c.Name, parquetscan.FormatBytes(c.CompressedBytes), c.NumValues, nulls, c.PhysicalType, minmax)
			}
		}
	}
	fmt.Printf("\ntotals: files=%d rows=%d row_groups=%d compressed=%s\n",
		len(files), totalRows, totalRG, parquetscan.FormatBytes(totalBytes))
	return nil
}

func cmdScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	table := fs.String("table", "", "table name under the data dir")
	dataDir := fs.String("data-dir", "", "tables root (default PRISM_DATA_DIR or ./data/tables)")
	filePath := fs.String("file", "", "scan a single parquet file instead of a table")
	cols := fs.String("columns", "", "comma-separated columns to read (default: all)")
	limit := fs.Int64("limit", 10, "max rows to print")
	batch := fs.Int64("batch-size", parquetscan.DefaultBatchSize, "Arrow batch size")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var files []string
	var err error
	if *filePath != "" {
		files = []string{*filePath}
	} else {
		if *table == "" {
			return fmt.Errorf("scan: --table or --file is required")
		}
		files, err = catalog.TableFiles(catalog.ResolveDataDir(*dataDir), *table)
		if err != nil {
			return err
		}
	}

	var columns []string
	if *cols != "" {
		for _, c := range strings.Split(*cols, ",") {
			c = strings.TrimSpace(c)
			if c != "" {
				columns = append(columns, c)
			}
		}
	}

	ctx := context.Background()
	rdr, err := parquetscan.Open(ctx, files, parquetscan.Options{
		Columns:   columns,
		BatchSize: *batch,
	})
	if err != nil {
		return err
	}
	defer rdr.Close()

	var printed int64
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	headerPrinted := false
	for printed < *limit {
		rec, err := rdr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if !headerPrinted {
			names := make([]string, rec.NumCols())
			for i := 0; i < int(rec.NumCols()); i++ {
				names[i] = rec.ColumnName(i)
			}
			fmt.Fprintln(tw, strings.Join(names, "\t"))
			headerPrinted = true
		}
		n := rec.NumRows()
		if printed+n > *limit {
			n = *limit - printed
		}
		for i := int64(0); i < n; i++ {
			cells := make([]string, rec.NumCols())
			for c := 0; c < int(rec.NumCols()); c++ {
				cells[c] = formatCell(rec.Column(c), int(i))
			}
			fmt.Fprintln(tw, strings.Join(cells, "\t"))
		}
		printed += n
		rec.Release()
	}
	tw.Flush()

	st := rdr.Stats()
	fmt.Fprintf(os.Stderr, "\nscan: printed=%d rows_read=%d batches=%d columns=%d (%s) row_groups=%d compressed_selected=%s files=%d\n",
		printed, st.RowsRead, st.BatchesEmitted, st.ColumnsRead, strings.Join(st.ColumnNames, ","),
		st.RowGroupsRead, parquetscan.FormatBytes(st.CompressedBytes), st.FilesOpened)
	return nil
}

func formatCell(col arrow.Array, row int) string {
	if col.IsNull(row) {
		return "NULL"
	}
	switch a := col.(type) {
	case *array.Int64:
		return strconv.FormatInt(a.Value(row), 10)
	case *array.Int32:
		return strconv.FormatInt(int64(a.Value(row)), 10)
	case *array.Float64:
		return strconv.FormatFloat(a.Value(row), 'g', -1, 64)
	case *array.Float32:
		return strconv.FormatFloat(float64(a.Value(row)), 'g', -1, 32)
	case *array.Boolean:
		return strconv.FormatBool(a.Value(row))
	case *array.String:
		return a.Value(row)
	case *array.Binary:
		return string(a.Value(row))
	case *array.Timestamp:
		return a.Value(row).ToTime(a.DataType().(*arrow.TimestampType).Unit).UTC().Format("2006-01-02T15:04:05.000Z")
	case *array.Date32:
		return a.Value(row).ToTime().UTC().Format("2006-01-02")
	case *array.Dictionary:
		return formatCell(a.Dictionary(), a.GetValueIndex(row))
	default:
		return col.ValueStr(row)
	}
}
