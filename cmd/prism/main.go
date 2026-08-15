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
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/exec"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/expr"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/kernel"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/parquetscan"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/plan"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/sql"
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
	case "tables":
		return cmdTables(args[1:])
	case "describe":
		return cmdDescribe(args[1:])
	case "agg":
		return cmdAgg(args[1:])
	case "sql":
		return cmdSQL(args[1:])
	case "explain":
		return cmdExplain(args[1:])
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], helpText)
	}
}

const helpText = `prism is a miniature vectorized OLAP engine.

Usage:
  prism <command> [flags]

Commands:
  version    Print version
  tables     List tables in the data dir
  describe   Schema, row counts, and cached row-group stats
  inspect    Dump Parquet schema, row groups, and min/max stats
  scan       Read selected columns (optional --where) and print rows
  agg        Hash aggregate (flag-based GROUP BY)
  sql        Parse/bind/run a Prism SQL SELECT
  explain    Print the physical plan (optional --analyze / --json)
  help       Show this message

Examples:
  go run ./cmd/prism version
  go run ./cmd/prism tables --data-dir testdata/tables
  go run ./cmd/prism describe events --data-dir testdata/tables
  go run ./cmd/prism inspect --table events
  go run ./cmd/prism scan --table events --columns country,amount_cents --limit 5
  go run ./cmd/prism scan --table events --where "amount_cents > 0 AND country = 'US'" --columns country,amount_cents
  go run ./cmd/prism agg --table events --group country --agg count,sum(amount_cents) --order count --desc --limit 10
  go run ./cmd/prism sql --data-dir testdata/tables "SELECT country, COUNT(*) FROM events GROUP BY country ORDER BY COUNT(*) DESC LIMIT 5"
  go run ./cmd/prism explain --data-dir testdata/tables --file testdata/sql/ok/q2.sql
`

func printHelp(w io.Writer) {
	fmt.Fprint(w, helpText)
}

// flagsFirst lets `prism describe events --data-dir testdata/tables` work
// (Go's flag package otherwise stops at the first positional).
func flagsFirst(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		rest = append(rest, a)
	}
	return append(flags, rest...)
}

func cmdTables(args []string) error {
	fs := flag.NewFlagSet("tables", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dataDir := fs.String("data-dir", "", "tables root (default PRISM_DATA_DIR or ./data/tables)")
	if err := fs.Parse(flagsFirst(args)); err != nil {
		return err
	}
	cat, err := catalog.Load(catalog.ResolveDataDir(*dataDir))
	if err != nil {
		return err
	}
	names := cat.Names()
	if len(names) == 0 {
		fmt.Printf("no tables in %s\n", cat.DataDir)
		return nil
	}
	fmt.Printf("data dir: %s\n", cat.DataDir)
	for _, name := range names {
		tbl, _ := cat.Table(name)
		fmt.Printf("%-16s files=%d rows=%d row_groups=%d compressed=%s\n",
			name, len(tbl.Files), tbl.NumRows, tbl.NumRowGroups, parquetscan.FormatBytes(tbl.CompressedBytes))
	}
	return nil
}

func cmdDescribe(args []string) error {
	fs := flag.NewFlagSet("describe", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dataDir := fs.String("data-dir", "", "tables root (default PRISM_DATA_DIR or ./data/tables)")
	tableFlag := fs.String("table", "", "table name")
	if err := fs.Parse(flagsFirst(args)); err != nil {
		return err
	}
	name := *tableFlag
	if name == "" && fs.NArg() > 0 {
		name = fs.Arg(0)
	}
	if name == "" {
		return fmt.Errorf("describe: table name required (prism describe events)")
	}
	cat, err := catalog.Load(catalog.ResolveDataDir(*dataDir))
	if err != nil {
		return err
	}
	tbl, err := cat.Table(name)
	if err != nil {
		return err
	}
	fmt.Printf("table: %s\n", tbl.Name)
	fmt.Printf("dir: %s\n", tbl.Dir)
	fmt.Printf("files: %d  rows: %d  row_groups: %d  compressed: %s\n",
		len(tbl.Files), tbl.NumRows, tbl.NumRowGroups, parquetscan.FormatBytes(tbl.CompressedBytes))
	fmt.Printf("schema:\n")
	for _, f := range tbl.Fields {
		fmt.Printf("  %-16s %s\n", f.Name, f.Type)
	}
	if _, ok := tbl.FieldType("ts"); ok {
		okClust, detail := tbl.Clustering("ts")
		if okClust {
			fmt.Printf("ts clustering: %s\n", detail)
		} else {
			fmt.Printf("ts clustering: not clustered (%s)\n", detail)
		}
	}
	fmt.Printf("row groups:\n")
	for i, rg := range tbl.RowGroups {
		ts := ""
		if st, ok := rg.ColStats("ts"); ok && st.HasMinMax {
			ts = fmt.Sprintf("  ts=[%s .. %s]", st.Min, st.Max)
		}
		fmt.Printf("  [%d] %s rg=%d rows=%d%s\n", i, filepathBase(rg.File), rg.Index, rg.NumRows, ts)
	}
	return nil
}

func filepathBase(p string) string {
	i := strings.LastIndexAny(p, `/\`)
	if i < 0 {
		return p
	}
	return p[i+1:]
}

func cmdInspect(args []string) error {
	fs := flag.NewFlagSet("inspect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	table := fs.String("table", "", "table name under the data dir")
	dataDir := fs.String("data-dir", "", "tables root (default PRISM_DATA_DIR or ./data/tables)")
	filePath := fs.String("file", "", "inspect a single parquet file instead of a table")
	if err := fs.Parse(flagsFirst(args)); err != nil {
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
	where := fs.String("where", "", "predicate, e.g. amount_cents > 0 AND country = 'US'")
	limit := fs.Int64("limit", 10, "max rows to print")
	batch := fs.Int64("batch-size", parquetscan.DefaultBatchSize, "Arrow batch size")
	if err := fs.Parse(flagsFirst(args)); err != nil {
		return err
	}

	var files []string
	var rowGroups map[string][]int
	if *filePath != "" {
		files = []string{*filePath}
	} else if *table == "" {
		return fmt.Errorf("scan: --table or --file is required")
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

	var pred expr.Expr
	scanCols := append([]string(nil), columns...)
	if *where != "" {
		var err error
		pred, err = expr.ParseWhere(*where)
		if err != nil {
			return err
		}
		scanCols = unionStrings(scanCols, pred.Columns())
	}

	if *filePath == "" {
		cat, err := catalog.Load(catalog.ResolveDataDir(*dataDir))
		if err != nil {
			return err
		}
		tbl, err := cat.Table(*table)
		if err != nil {
			return err
		}
		node := plan.Build(plan.Input{
			Table:    tbl,
			Where:    pred,
			ScanCols: scanCols,
			Project:  columns,
			Limit:    *limit,
		})
		req := node.Request(*batch)
		files = tbl.Files
		rowGroups = req.RowGroups
		if req.Empty {
			rowGroups = map[string][]int{}
			for _, f := range tbl.Files {
				rowGroups[f] = []int{}
			}
		}
	}

	ctx := context.Background()
	rdr, err := parquetscan.Open(ctx, files, parquetscan.Options{
		Columns:   scanCols,
		BatchSize: *batch,
		RowGroups: rowGroups,
	})
	if err != nil {
		return err
	}
	defer rdr.Close()

	var printed int64
	var rowsKept int64
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	headerPrinted := false
	outNames := columns
	for printed < *limit {
		rec, err := rdr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		cur := rec
		if pred != nil {
			mask, err := kernel.Eval(pred, rec)
			if err != nil {
				rec.Release()
				return err
			}
			compacted, err := kernel.Compact(rec, mask)
			mask.Release()
			rec.Release()
			if err != nil {
				return err
			}
			cur = compacted
		}
		if len(outNames) > 0 {
			proj, err := kernel.Project(cur, outNames)
			cur.Release()
			if err != nil {
				return err
			}
			cur = proj
		}
		rowsKept += cur.NumRows()
		if cur.NumRows() == 0 {
			cur.Release()
			continue
		}
		if !headerPrinted {
			names := make([]string, cur.NumCols())
			for i := 0; i < int(cur.NumCols()); i++ {
				names[i] = cur.ColumnName(i)
			}
			fmt.Fprintln(tw, strings.Join(names, "\t"))
			headerPrinted = true
		}
		n := cur.NumRows()
		if printed+n > *limit {
			n = *limit - printed
		}
		for i := int64(0); i < n; i++ {
			cells := make([]string, cur.NumCols())
			for c := 0; c < int(cur.NumCols()); c++ {
				cells[c] = formatCell(cur.Column(c), int(i))
			}
			fmt.Fprintln(tw, strings.Join(cells, "\t"))
		}
		printed += n
		cur.Release()
	}
	tw.Flush()

	st := rdr.Stats()
	fmt.Fprintf(os.Stderr, "\nscan: printed=%d rows_kept=%d rows_read=%d batches=%d columns=%d (%s) row_groups_read=%d skipped=%d compressed_selected=%s files=%d\n",
		printed, rowsKept, st.RowsRead, st.BatchesEmitted, st.ColumnsRead, strings.Join(st.ColumnNames, ","),
		st.RowGroupsRead, st.RowGroupsSkipped, parquetscan.FormatBytes(st.CompressedBytes), st.FilesOpened)
	return nil
}

func cmdAgg(args []string) error {
	fs := flag.NewFlagSet("agg", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	table := fs.String("table", "", "table name")
	dataDir := fs.String("data-dir", "", "tables root")
	group := fs.String("group", "", "comma-separated GROUP BY columns")
	aggList := fs.String("agg", "count", "aggregates: count,sum(col),avg(col),min(col),max(col)")
	where := fs.String("where", "", "optional predicate")
	order := fs.String("order", "", "comma-separated output columns, suffix :desc")
	descAll := fs.Bool("desc", false, "sort every --order key descending")
	limit := fs.Int64("limit", 20, "max rows to print (0 = all)")
	batch := fs.Int64("batch-size", parquetscan.DefaultBatchSize, "Arrow batch size")
	if err := fs.Parse(flagsFirst(args)); err != nil {
		return err
	}
	if *table == "" {
		return fmt.Errorf("agg: --table is required")
	}
	cat, err := catalog.Load(catalog.ResolveDataDir(*dataDir))
	if err != nil {
		return err
	}
	tbl, err := cat.Table(*table)
	if err != nil {
		return err
	}
	aggs, err := kernel.ParseAggList(*aggList)
	if err != nil {
		return err
	}
	var keys []string
	if *group != "" {
		for _, g := range strings.Split(*group, ",") {
			g = strings.TrimSpace(g)
			if g != "" {
				keys = append(keys, g)
			}
		}
	}
	var pred expr.Expr
	if *where != "" {
		pred, err = expr.ParseWhere(*where)
		if err != nil {
			return err
		}
	}
	var orderKeys []kernel.OrderKey
	if *order != "" {
		for _, part := range strings.Split(*order, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			desc := false
			name := part
			if i := strings.LastIndexByte(part, ':'); i >= 0 {
				name = part[:i]
				if strings.EqualFold(part[i+1:], "desc") {
					desc = true
				}
			}
			orderKeys = append(orderKeys, kernel.OrderKey{Name: name, Desc: desc || *descAll})
		}
	}
	node := plan.Build(plan.Input{
		Table:   tbl,
		Where:   pred,
		GroupBy: keys,
		Aggs:    aggs,
		Order:   orderKeys,
		Limit:   *limit,
		IsAgg:   true,
	})
	req := node.Request(*batch)
	res, err := exec.Run(context.Background(), req)
	if err != nil {
		return err
	}
	defer res.Record.Release()
	printRecord(os.Stdout, res.Record, res.Record.NumRows())
	st := res.Stats
	fmt.Fprintf(os.Stderr, "\nagg: rows=%d groups=%d rows_read=%d batches=%d columns=%d row_groups_read=%d skipped=%d files=%d\n",
		res.Record.NumRows(), res.Groups, st.RowsRead, st.BatchesEmitted, st.ColumnsRead, st.RowGroupsRead, st.RowGroupsSkipped, st.FilesOpened)
	return nil
}

func cmdSQL(args []string) error {
	fs := flag.NewFlagSet("sql", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dataDir := fs.String("data-dir", "", "tables root")
	filePath := fs.String("file", "", "read SQL from a file")
	astOnly := fs.Bool("ast", false, "print the parsed AST and exit")
	bindOnly := fs.Bool("bind-only", false, "parse+bind and print a summary (do not execute)")
	doExplain := fs.Bool("explain", false, "print the physical plan instead of rows")
	analyze := fs.Bool("analyze", false, "run the query and include scan stats in EXPLAIN")
	asJSON := fs.Bool("json", false, "EXPLAIN as JSON")
	batch := fs.Int64("batch-size", parquetscan.DefaultBatchSize, "Arrow batch size")
	if err := fs.Parse(flagsFirst(args)); err != nil {
		return err
	}
	src := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if *filePath != "" {
		b, err := os.ReadFile(*filePath)
		if err != nil {
			return err
		}
		src = string(b)
	}
	if src == "" {
		return fmt.Errorf("sql: query required (prism sql \"SELECT ...\" or --file)")
	}
	q, err := sql.Parse(src)
	if err != nil {
		return err
	}
	if *astOnly {
		fmt.Println(q.String())
		return nil
	}
	cat, err := catalog.Load(catalog.ResolveDataDir(*dataDir))
	if err != nil {
		return err
	}
	bound, err := sql.Bind(q, cat)
	if err != nil {
		return err
	}
	if *bindOnly {
		fmt.Printf("table: %s\n", bound.Table.Name)
		fmt.Printf("scan_cols: %s\n", strings.Join(bound.ScanCols, ", "))
		if bound.Where != nil {
			fmt.Printf("where: %s\n", bound.Where.String())
		}
		if bound.IsAgg {
			fmt.Printf("group_by: %s\n", strings.Join(bound.GroupBy, ", "))
			for _, a := range bound.Aggs {
				fmt.Printf("agg: %s %s -> %s\n", a.Fn, a.Input, a.Name)
			}
		}
		fmt.Printf("project: %s\n", strings.Join(bound.Project, ", "))
		return nil
	}
	node := plan.Build(boundInput(bound))
	if *doExplain && !*analyze {
		return printExplain(node, *asJSON)
	}
	req := node.Request(*batch)
	res, err := exec.Run(context.Background(), req)
	if err != nil {
		return err
	}
	defer res.Record.Release()
	if *doExplain || *analyze {
		node.AttachStats(res.Stats)
		return printExplain(node, *asJSON)
	}
	printRecord(os.Stdout, res.Record, res.Record.NumRows())
	st := res.Stats
	fmt.Fprintf(os.Stderr, "\nsql: rows=%d groups=%d rows_read=%d batches=%d columns=%d row_groups_read=%d skipped=%d files=%d\n",
		res.Record.NumRows(), res.Groups, st.RowsRead, st.BatchesEmitted, st.ColumnsRead, st.RowGroupsRead, st.RowGroupsSkipped, st.FilesOpened)
	return nil
}

func boundInput(b *sql.BoundQuery) plan.Input {
	return plan.Input{
		Table:    b.Table,
		Where:    b.Where,
		ScanCols: b.ScanCols,
		GroupBy:  b.GroupBy,
		Aggs:     b.Aggs,
		Project:  b.Project,
		Order:    b.Order,
		Limit:    b.Limit,
		IsAgg:    b.IsAgg,
	}
}

func cmdExplain(args []string) error {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dataDir := fs.String("data-dir", "", "tables root")
	filePath := fs.String("file", "", "read SQL from a file")
	analyze := fs.Bool("analyze", false, "execute and include bytes/rows")
	asJSON := fs.Bool("json", false, "JSON plan")
	batch := fs.Int64("batch-size", parquetscan.DefaultBatchSize, "Arrow batch size")
	if err := fs.Parse(flagsFirst(args)); err != nil {
		return err
	}
	src := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if *filePath != "" {
		b, err := os.ReadFile(*filePath)
		if err != nil {
			return err
		}
		src = string(b)
	}
	if src == "" {
		return fmt.Errorf("explain: query required")
	}
	q, err := sql.Parse(src)
	if err != nil {
		return err
	}
	cat, err := catalog.Load(catalog.ResolveDataDir(*dataDir))
	if err != nil {
		return err
	}
	bound, err := sql.Bind(q, cat)
	if err != nil {
		return err
	}
	node := plan.Build(boundInput(bound))
	if *analyze {
		res, err := exec.Run(context.Background(), node.Request(*batch))
		if err != nil {
			return err
		}
		node.AttachStats(res.Stats)
		res.Record.Release()
	}
	return printExplain(node, *asJSON)
}

func printExplain(node *plan.Node, asJSON bool) error {
	if asJSON {
		s, err := node.JSONString()
		if err != nil {
			return err
		}
		fmt.Println(s)
		return nil
	}
	fmt.Println(node.Text())
	return nil
}

func printRecord(w io.Writer, rec arrow.Record, limit int64) {
	if rec == nil || rec.NumRows() == 0 || rec.NumCols() == 0 {
		return
	}
	if limit <= 0 || limit > rec.NumRows() {
		limit = rec.NumRows()
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	names := make([]string, rec.NumCols())
	for i := 0; i < int(rec.NumCols()); i++ {
		names[i] = rec.ColumnName(i)
	}
	fmt.Fprintln(tw, strings.Join(names, "\t"))
	for i := int64(0); i < limit; i++ {
		cells := make([]string, rec.NumCols())
		for c := 0; c < int(rec.NumCols()); c++ {
			cells[c] = formatCell(rec.Column(c), int(i))
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	tw.Flush()
}

func unionStrings(a, b []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, xs := range [][]string{a, b} {
		for _, s := range xs {
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
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
