package sql

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/catalog"
)

func TestLexerBasics(t *testing.T) {
	toks, err := Tokenize(`SELECT country, COUNT(*) FROM events WHERE amount_cents > 0 AND country = 'US'`)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(toks, " ")
	if !strings.Contains(joined, "SELECT") || !strings.Contains(joined, "COUNT") || !strings.Contains(joined, "*") {
		t.Fatal(joined)
	}
	toks, err = Tokenize(`select "amount_cents", 1.5, 'it''s'`)
	if err != nil {
		t.Fatal(err)
	}
	if toks[1] != "amount_cents" || toks[3] != "1.5" || toks[5] != "it's" {
		t.Fatalf("%v", toks)
	}
}

func TestLexerUnsupported(t *testing.T) {
	toks, err := Tokenize("SELECT * FROM events JOIN users")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, t0 := range toks {
		if strings.HasPrefix(t0, "UNSUPPORTED:JOIN") {
			found = true
		}
	}
	if !found {
		t.Fatalf("%v", toks)
	}
}

func TestParseExpectedFrom(t *testing.T) {
	_, err := Parse("SELECT * WHERE true")
	if err == nil || !strings.Contains(err.Error(), "expected FROM") {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "column") {
		t.Fatalf("want byte-offset column in %v", err)
	}
}

func TestParseDialect(t *testing.T) {
	cases := []string{
		`SELECT * FROM events`,
		`SELECT ALL country FROM events`,
		`SELECT country, event_type FROM events`,
		`SELECT COUNT(*) FROM events`,
		`SELECT SUM(amount_cents), AVG(amount_cents), MIN(qty), MAX(qty) FROM events`,
		`SELECT COUNT(country) FROM events`,
		`SELECT country FROM events WHERE amount_cents > 0`,
		`SELECT country FROM events WHERE ts >= TIMESTAMP '2024-01-01' AND country IN ('US', 'CA', 'GB')`,
		`SELECT country FROM events WHERE amount_cents BETWEEN 1 AND 10`,
		`SELECT country FROM events WHERE event_type IS NOT NULL`,
		`SELECT country FROM events WHERE NOT (country = 'XX')`,
		`SELECT country, COUNT(*) FROM events GROUP BY country`,
		`SELECT country FROM events ORDER BY country ASC, event_id DESC`,
		`SELECT country FROM events LIMIT 20`,
		`SELECT country AS c FROM events`,
		`SELECT 1 + 2 * 3 FROM events`,
		`SELECT * FROM events;`,
	}
	for _, src := range cases {
		q, err := Parse(src)
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		rt := q.String()
		if _, err := Parse(rt); err != nil {
			t.Errorf("round-trip parse of %q -> %q: %v", src, rt, err)
		}
	}
}

func TestParseRejects(t *testing.T) {
	cases := []struct {
		src, want string
	}{
		{`SELECT * FROM events JOIN users ON true`, "JOIN not supported"},
		{`SELECT * FROM events HAVING COUNT(*) > 1`, "HAVING not supported"},
		{`SELECT DISTINCT country FROM events`, "DISTINCT not supported"},
		{`WITH x AS (SELECT * FROM events) SELECT * FROM x`, "WITH not supported"},
		{`SELECT * FROM events OFFSET 10`, "OFFSET not supported"},
	}
	for _, tc := range cases {
		_, err := Parse(tc.src)
		if err == nil || !strings.Contains(err.Error(), "not supported") {
			t.Errorf("%s: got %v, want %s", tc.src, err, tc.want)
		}
	}
}

func testdataDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata")
}

func TestBindTestdataSQLFiles(t *testing.T) {
	cat, err := catalog.Load(filepath.Join(testdataDir(t), "tables"))
	if err != nil {
		t.Fatal(err)
	}
	okDir := filepath.Join(testdataDir(t), "sql", "ok")
	rejDir := filepath.Join(testdataDir(t), "sql", "reject")
	bindDir(t, cat, okDir, true)
	bindDir(t, cat, rejDir, false)
}

func bindDir(t *testing.T, cat *catalog.Catalog, dir string, wantOK bool) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		n++
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		q, err := Parse(string(body))
		if wantOK && err != nil {
			t.Errorf("%s parse: %v", e.Name(), err)
			continue
		}
		if !wantOK && err != nil {
			continue // parse-time reject is OK
		}
		_, err = Bind(q, cat)
		if wantOK && err != nil {
			t.Errorf("%s bind: %v", e.Name(), err)
		}
		if !wantOK && err == nil && q != nil {
			// some rejects fail at parse; if it parsed, bind must fail
			t.Errorf("%s: expected bind/parse error", e.Name())
		}
	}
	if n == 0 {
		t.Fatalf("no .sql files in %s", dir)
	}
}

func TestBindUnknowns(t *testing.T) {
	cat, err := catalog.Load(filepath.Join(testdataDir(t), "tables"))
	if err != nil {
		t.Fatal(err)
	}
	q, err := Parse("SELECT nope FROM events")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Bind(q, cat); err == nil || !strings.Contains(err.Error(), "unknown column") {
		t.Fatalf("got %v", err)
	}
	q, _ = Parse("SELECT * FROM missing")
	if _, err := Bind(q, cat); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("got %v", err)
	}
	q, _ = Parse("SELECT country, COUNT(*) FROM events")
	if _, err := Bind(q, cat); err == nil || !strings.Contains(err.Error(), "GROUP BY") {
		t.Fatalf("got %v", err)
	}
}
