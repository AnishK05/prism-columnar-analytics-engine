package plan

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/catalog"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/exec"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/sql"
)

func testdataTables(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "tables")
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func compileSQL(t *testing.T, src string) *Node {
	t.Helper()
	cat, err := catalog.Load(testdataTables(t))
	if err != nil {
		t.Fatal(err)
	}
	q, err := sql.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	b, err := sql.Bind(q, cat)
	if err != nil {
		t.Fatal(err)
	}
	return Build(Input{
		Table:    b.Table,
		Where:    b.Where,
		ScanCols: b.ScanCols,
		GroupBy:  b.GroupBy,
		Aggs:     b.Aggs,
		Project:  b.Project,
		Order:    b.Order,
		Limit:    b.Limit,
		IsAgg:    b.IsAgg,
	})
}

func TestExplainQ2PushesTsAndPrunes(t *testing.T) {
	src := `
SELECT COUNT(*), SUM(amount_cents)
FROM events
WHERE ts >= TIMESTAMP '2024-01-01' AND ts < TIMESTAMP '2024-01-08'`
	n := compileSQL(t, src)
	text := n.Text()
	t.Log("\n" + text)
	if !strings.Contains(text, "ParquetScan table=events") {
		t.Fatal(text)
	}
	if !strings.Contains(text, "pushed=") || !strings.Contains(text, "ts") {
		t.Fatalf("expected pushed ts predicate:\n%s", text)
	}
	if !strings.Contains(text, "columns=[") {
		t.Fatal(text)
	}
	if strings.Contains(text, "event_type") || strings.Contains(text, "session_id") {
		t.Fatalf("column list not pruned:\n%s", text)
	}
	if !strings.Contains(text, "amount_cents") || !strings.Contains(text, "ts") {
		t.Fatalf("need ts + amount_cents in scan:\n%s", text)
	}
	if !strings.Contains(text, "pruned") {
		t.Fatalf("expected pruned col note:\n%s", text)
	}
	scan := n.find(OpScan)
	if scan == nil {
		t.Fatal("no scan")
	}
	if scan.RowGroupsKept != 1 || scan.RowGroupsSkipped != 3 {
		t.Fatalf("Q2 skip: kept=%d skipped=%d total=%d (want 1 kept, 3 skipped)",
			scan.RowGroupsKept, scan.RowGroupsSkipped, scan.RowGroupsTotal)
	}
}

func TestExplainJSON(t *testing.T) {
	n := compileSQL(t, `SELECT country FROM events WHERE country = 'US' LIMIT 5`)
	js, err := n.JSONString()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, `"op": "Limit"`) || !strings.Contains(js, "ParquetScan") {
		t.Fatal(js)
	}
	if !strings.Contains(n.Text(), "limit_pushdown=5") {
		t.Fatal(n.Text())
	}
}

func TestConstantFalseEmpty(t *testing.T) {
	n := compileSQL(t, `SELECT * FROM events WHERE FALSE`)
	if n.find(OpEmpty) == nil && !n.find(OpScan).Empty {
		t.Fatalf("expected empty plan:\n%s", n.Text())
	}
}

func TestQ2ExecSkipsThreeRowGroups(t *testing.T) {
	src := `
SELECT COUNT(*), SUM(amount_cents)
FROM events
WHERE ts >= TIMESTAMP '2024-01-01' AND ts < TIMESTAMP '2024-01-08'`
	n := compileSQL(t, src)
	res, err := exec.Run(context.Background(), n.Request(1024))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Record.Release()
	n.AttachStats(res.Stats)
	if res.Stats.RowGroupsRead != 1 || res.Stats.RowGroupsSkipped != 3 {
		t.Fatalf("exec skip: read=%d skipped=%d total=%d\n%s",
			res.Stats.RowGroupsRead, res.Stats.RowGroupsSkipped, res.Stats.RowGroupsTotal, n.Text())
	}
	if res.Record.NumRows() != 1 {
		t.Fatalf("rows = %d", res.Record.NumRows())
	}
}
