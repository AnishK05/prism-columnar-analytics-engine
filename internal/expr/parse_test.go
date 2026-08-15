package expr

import (
	"strings"
	"testing"
)

func TestParseWhere(t *testing.T) {
	e, err := ParseWhere(`amount_cents > 0 AND country = 'US'`)
	if err != nil {
		t.Fatal(err)
	}
	cols := e.Columns()
	if len(cols) != 2 {
		t.Fatalf("columns = %v", cols)
	}
	if !strings.Contains(e.String(), "amount_cents") {
		t.Fatal(e.String())
	}
}

func TestParseInBetweenNull(t *testing.T) {
	cases := []string{
		`country IN ('US', 'CA')`,
		`country NOT IN ('XX')`,
		`amount_cents BETWEEN 1 AND 10`,
		`amount_cents IS NULL`,
		`amount_cents IS NOT NULL`,
		`NOT (amount_cents > 0)`,
		`ts >= 1704067200000`,
		`qty <> 1 OR qty = 1`,
	}
	for _, src := range cases {
		if _, err := ParseWhere(src); err != nil {
			t.Errorf("%s: %v", src, err)
		}
	}
}

func TestParseErrors(t *testing.T) {
	_, err := ParseWhere(`amount_cents >`)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "column") {
		t.Fatalf("want column in error, got %v", err)
	}
	_, err = ParseWhere(`'unterminated`)
	if err == nil {
		t.Fatal("expected unterminated string")
	}
}

func TestParseStringEscape(t *testing.T) {
	e, err := ParseWhere(`country = 'O''Brien'`)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := e.(*Binary)
	if !ok {
		t.Fatalf("%T", e)
	}
	lit := b.Right.(*Lit)
	if lit.Str != "O'Brien" {
		t.Fatalf("got %q", lit.Str)
	}
}
