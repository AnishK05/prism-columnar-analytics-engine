// Package catalog locates Parquet tables on disk.
//
// A table is a directory of *.parquet files under the data dir:
//
//	<data-dir>/<table>/*.parquet
//
// The default data dir is ./data/tables, or PRISM_DATA_DIR if set.
package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const envDataDir = "PRISM_DATA_DIR"

// DefaultDataDir returns PRISM_DATA_DIR, or ./data/tables relative to the
// current working directory.
func DefaultDataDir() string {
	if d := os.Getenv(envDataDir); d != "" {
		return d
	}
	return filepath.Join("data", "tables")
}

// ResolveDataDir returns flagDir if non-empty, otherwise DefaultDataDir.
func ResolveDataDir(flagDir string) string {
	if flagDir != "" {
		return flagDir
	}
	return DefaultDataDir()
}

// TableDir is <dataDir>/<table>.
func TableDir(dataDir, table string) string {
	return filepath.Join(dataDir, table)
}

// TableFiles lists parquet files for a table, sorted by name.
func TableFiles(dataDir, table string) ([]string, error) {
	if table == "" {
		return nil, fmt.Errorf("table name is required")
	}
	dir := TableDir(dataDir, table)
	matches, err := filepath.Glob(filepath.Join(dir, "*.parquet"))
	if err != nil {
		return nil, fmt.Errorf("list parquet files in %s: %w", dir, err)
	}
	if len(matches) == 0 {
		if st, statErr := os.Stat(dir); statErr != nil {
			if os.IsNotExist(statErr) {
				return nil, fmt.Errorf("table %q not found (looked in %s)", table, dir)
			}
			return nil, statErr
		} else if !st.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", dir)
		}
		return nil, fmt.Errorf("table %q has no .parquet files in %s", table, dir)
	}
	sort.Strings(matches)
	return matches, nil
}
