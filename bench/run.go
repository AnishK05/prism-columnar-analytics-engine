// Command bench runs PrismBench (same flags as `prism bench`).
//
//	go run ./bench --scale testdata --repeat 3
package main

import (
	"fmt"
	"os"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/bench"
)

func main() {
	if err := bench.Main(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "bench: %v\n", err)
		os.Exit(1)
	}
}
