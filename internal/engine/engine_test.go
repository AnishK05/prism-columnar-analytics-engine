package engine

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
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

func readSQL(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	p := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "sql", "ok", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestQ1toQ8EnginesMatch(t *testing.T) {
	dir := testdataTables(t)
	files := []string{"q1.sql", "q2.sql", "q3.sql", "q4.sql", "q5.sql", "q6.sql", "q7.sql", "q8.sql"}
	ctx := context.Background()
	for _, name := range files {
		src := readSQL(t, name)
		ordered := strings.Contains(strings.ToUpper(src), "ORDER BY")
		t.Run(name, func(t *testing.T) {
			vec1, err := Run(ctx, src, Opts{DataDir: dir, Engine: KindVectorized, Jobs: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer vec1.Record.Release()
			got := []struct {
				kind Kind
				jobs int
			}{
				{KindVectorized, 4},
				{KindRow, 1},
				{KindRow, 4},
			}
			base := Fingerprint(vec1.Record, ordered)
			if len(base) == 0 && vec1.Record.NumRows() != 0 {
				t.Fatal("empty fingerprint")
			}
			for _, g := range got {
				res, err := Run(ctx, src, Opts{DataDir: dir, Engine: g.kind, Jobs: g.jobs})
				if err != nil {
					t.Fatalf("%s jobs=%d: %v", g.kind, g.jobs, err)
				}
				fp := Fingerprint(res.Record, ordered)
				res.Record.Release()
				if !reflect.DeepEqual(base, fp) {
					t.Fatalf("%s jobs=%d mismatch\nvec1=%v\n got=%v", g.kind, g.jobs, base, fp)
				}
			}
			if vec1.Profile.RowGroupsTotal != 4 {
				t.Fatalf("row_groups_total=%d want 4", vec1.Profile.RowGroupsTotal)
			}
		})
	}
}

func TestQ2SkipCounts(t *testing.T) {
	dir := testdataTables(t)
	res, err := Run(context.Background(), readSQL(t, "q2.sql"), Opts{DataDir: dir, Engine: KindVectorized, Jobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Record.Release()
	if res.Profile.RowGroupsRead != 1 || res.Profile.RowGroupsSkipped != 3 {
		t.Fatalf("skip: read=%d skipped=%d", res.Profile.RowGroupsRead, res.Profile.RowGroupsSkipped)
	}
	row, err := Run(context.Background(), readSQL(t, "q2.sql"), Opts{DataDir: dir, Engine: KindRow, Jobs: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer row.Record.Release()
	if row.Profile.RowGroupsRead != 1 {
		t.Fatalf("row engine skipped poorly: read=%d", row.Profile.RowGroupsRead)
	}
}

func TestResultJSON(t *testing.T) {
	dir := testdataTables(t)
	res, err := Run(context.Background(), `SELECT COUNT(*) FROM events`, Opts{DataDir: dir, Jobs: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Record.Release()
	b, err := res.JSON()
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"engine": "vectorized"`) || !strings.Contains(s, "8192") {
		t.Fatal(s)
	}
}
