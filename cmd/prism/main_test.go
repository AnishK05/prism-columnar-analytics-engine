package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	if err := run([]string{"version"}); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownCommand(t *testing.T) {
	err := run([]string{"nope"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHelp(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf)
	if !strings.Contains(buf.String(), "scan") {
		t.Fatal(buf.String())
	}
	if !strings.Contains(buf.String(), "describe") {
		t.Fatal("help should mention describe")
	}
	if !strings.Contains(buf.String(), "agg") || !strings.Contains(buf.String(), "sql") {
		t.Fatal("help should mention agg and sql")
	}
	if !strings.Contains(buf.String(), "explain") {
		t.Fatal("help should mention explain")
	}
}

func TestFlagsFirst(t *testing.T) {
	got := flagsFirst([]string{"events", "--data-dir", "testdata/tables"})
	if got[0] != "--data-dir" || got[2] != "events" {
		t.Fatalf("%v", got)
	}
}

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

func TestDescribeTestdata(t *testing.T) {
	dir := testdataTables(t)
	if err := run([]string{"describe", "events", "--data-dir", dir}); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"tables", "--data-dir", dir}); err != nil {
		t.Fatal(err)
	}
}

func TestScanWhereTestdata(t *testing.T) {
	dir := testdataTables(t)
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	runErr := run([]string{
		"scan", "--data-dir", dir, "--table", "events",
		"--where", "amount_cents > 0 AND country = 'US'",
		"--columns", "country,amount_cents", "--limit", "10",
	})
	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	out, _ := io.ReadAll(rOut)
	errb, _ := io.ReadAll(rErr)
	if runErr != nil {
		t.Fatalf("%v\nstderr: %s", runErr, errb)
	}
	if !strings.Contains(string(out), "US") {
		t.Fatalf("expected US rows, got:\n%s", out)
	}
	if !strings.Contains(string(errb), "rows_kept=") {
		t.Fatalf("expected scan stats, stderr:\n%s", errb)
	}
}

func TestAggAndSQLTestdata(t *testing.T) {
	dir := testdataTables(t)
	oldOut, oldErr := os.Stdout, os.Stderr
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	runErr := run([]string{"agg", "--data-dir", dir, "--table", "events", "--group", "country", "--agg", "count", "--limit", "5"})
	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	out, _ := io.ReadAll(rOut)
	errb, _ := io.ReadAll(rErr)
	if runErr != nil {
		t.Fatalf("agg: %v\n%s", runErr, errb)
	}
	if !strings.Contains(string(out), "country") {
		t.Fatalf("agg out:\n%s", out)
	}

	rOut, wOut, _ = os.Pipe()
	rErr, wErr, _ = os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr
	runErr = run([]string{"sql", "--data-dir", dir, "SELECT COUNT(*) FROM events"})
	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	out, _ = io.ReadAll(rOut)
	errb, _ = io.ReadAll(rErr)
	if runErr != nil {
		t.Fatalf("sql: %v\n%s", runErr, errb)
	}
	if !strings.Contains(string(out), "8192") {
		t.Fatalf("sql COUNT(*) out:\n%s\nstderr:%s", out, errb)
	}
}

func TestExplainQ2(t *testing.T) {
	dir := testdataTables(t)
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	q2 := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "sql", "ok", "q2.sql")
	oldOut, oldErr := os.Stdout, os.Stderr
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr }()
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, os.Stderr
	runErr := run([]string{"explain", "--data-dir", dir, "--file", q2})
	wOut.Close()
	os.Stdout = oldOut
	out, _ := io.ReadAll(rOut)
	if runErr != nil {
		t.Fatal(runErr)
	}
	text := string(out)
	if !strings.Contains(text, "pushed=") || !strings.Contains(text, "ts") {
		t.Fatalf("expected pushed ts:\n%s", text)
	}
	if !strings.Contains(text, "kept_row_groups=1") || !strings.Contains(text, "skipped=3") {
		t.Fatalf("expected skip counts:\n%s", text)
	}
	if strings.Contains(text, "event_type") || strings.Contains(text, "session_id") {
		t.Fatalf("column list not pruned:\n%s", text)
	}
}
