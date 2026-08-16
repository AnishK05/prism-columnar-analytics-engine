package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	s := New(Config{
		DataDir:    testdataTables(t),
		Timeout:    30 * time.Second,
		Jobs:       1,
		CORSOrigin: "*",
		BenchFile:  filepath.Join(root, "bench", "results", "sample.json"),
	})
	return httptest.NewServer(s.Handler())
}

func TestHealth(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()
	res, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.Status)
	}
	body := decodeMap(t, res.Body)
	if body["ok"] != true {
		t.Fatalf("%v", body)
	}
	if body["version"] == "" {
		t.Fatal("missing version")
	}
}

func TestTablesAndDescribe(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()
	res, err := http.Get(ts.URL + "/tables")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	list := decodeMap(t, res.Body)
	raw, _ := json.Marshal(list["tables"])
	if !strings.Contains(string(raw), `"name":"events"`) && !strings.Contains(string(raw), `"name": "events"`) {
		t.Fatalf("tables: %s", raw)
	}

	res2, err := http.Get(ts.URL + "/tables/events")
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()
	info := decodeMap(t, res2.Body)
	if info["rows"] != float64(8192) {
		t.Fatalf("rows=%v", info["rows"])
	}
	if info["min_ts_ms"] != float64(1704067200000) {
		t.Fatalf("min_ts=%v", info["min_ts_ms"])
	}

	res3, err := http.Get(ts.URL + "/tables/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer res3.Body.Close()
	if res3.StatusCode != 404 {
		t.Fatalf("status %d", res3.StatusCode)
	}
}

func TestQueryGroupBy(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()
	sql := `SELECT country, COUNT(*) FROM events GROUP BY country ORDER BY COUNT(*) DESC LIMIT 5`
	res := postJSON(t, ts.URL+"/query", map[string]any{
		"sql":     sql,
		"engine":  "vectorized",
		"explain": true,
	})
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("%d %s", res.StatusCode, b)
	}
	body := decodeMap(t, res.Body)
	rows, _ := body["rows"].([]any)
	if len(rows) != 5 {
		t.Fatalf("rows=%d body=%v", len(rows), body)
	}
	prof, _ := body["profile"].(map[string]any)
	if prof["engine"] != "vectorized" {
		t.Fatalf("profile=%v", prof)
	}
	if prof["plan"] == nil {
		t.Fatal("expected plan when explain=true")
	}
	if _, ok := prof["elapsed_ms"].(float64); !ok {
		t.Fatalf("elapsed_ms=%v", prof["elapsed_ms"])
	}
}

func TestQueryDefaultScanLimit(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()
	res := postJSON(t, ts.URL+"/query", map[string]any{
		"sql": "SELECT event_id FROM events",
	})
	defer res.Body.Close()
	body := decodeMap(t, res.Body)
	rows, _ := body["rows"].([]any)
	if len(rows) != 100 {
		t.Fatalf("default scan cap rows=%d", len(rows))
	}
	if body["truncated"] != true {
		t.Fatal("expected truncated")
	}
}

func TestQueryParseErrorPos(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()
	res := postJSON(t, ts.URL+"/query", map[string]any{"sql": "SELCT 1"})
	defer res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("status %d", res.StatusCode)
	}
	body := decodeMap(t, res.Body)
	if body["error"] == nil || body["pos"] == nil {
		t.Fatalf("%v", body)
	}
}

func TestQueryCORS(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodOptions, ts.URL+"/query", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", res.StatusCode)
	}
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("cors origin %q", got)
	}
}

func TestExplainQ2(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()
	sql := `SELECT COUNT(*), SUM(amount_cents) FROM events WHERE ts >= TIMESTAMP '2024-01-01' AND ts < TIMESTAMP '2024-01-08'`
	res := postJSON(t, ts.URL+"/explain", map[string]any{"sql": sql, "analyze": true})
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("%d %s", res.StatusCode, b)
	}
	body := decodeMap(t, res.Body)
	text, _ := body["text"].(string)
	if !strings.Contains(text, "kept_row_groups=1") {
		t.Fatalf("explain text:\n%s", text)
	}
}

func TestBenchSample(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()
	res, err := http.Get(ts.URL + "/bench")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("%d %s", res.StatusCode, b)
	}
	body := decodeMap(t, res.Body)
	if body["schema"] != "prism-bench-v1" {
		t.Fatalf("%v", body["schema"])
	}
}

func TestQueryMissingSQL(t *testing.T) {
	ts := testServer(t)
	defer ts.Close()
	res := postJSON(t, ts.URL+"/query", map[string]any{"engine": "row"})
	defer res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func postJSON(t *testing.T, url string, body map[string]any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Post(url, "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func decodeMap(t *testing.T, r io.Reader) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
