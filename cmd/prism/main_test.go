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
