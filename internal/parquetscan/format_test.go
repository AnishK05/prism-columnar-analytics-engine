package parquetscan

import "testing"

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		0:          "0 B",
		512:        "512 B",
		1024:       "1.0 KiB",
		2950:       "2.9 KiB",
		12128:      "11.8 KiB",
		1024 * 1024: "1.0 MiB",
	}
	for n, want := range cases {
		if got := FormatBytes(n); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
