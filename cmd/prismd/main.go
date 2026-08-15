package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/catalog"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/server"
	"github.com/AnishK05/prism-columnar-analytics-engine/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "prismd: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("prismd", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	listen := fs.String("listen", envOr("PRISM_LISTEN", "127.0.0.1:8080"), "listen address")
	dataDir := fs.String("data-dir", "", "tables root (default PRISM_DATA_DIR or ./data/tables)")
	timeout := fs.Duration("timeout", 60*time.Second, "per-query timeout")
	jobs := fs.Int("jobs", 0, "parallel workers (0 = PRISM_PARALLELISM or GOMAXPROCS)")
	cors := fs.String("cors", "*", "CORS Allow-Origin for the Next.js workbench")
	benchFile := fs.String("bench-file", "", "JSON file for GET /bench (default bench/results/sample.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	dir := catalog.ResolveDataDir(*dataDir)
	srv := server.New(server.Config{
		DataDir:    dir,
		Timeout:    *timeout,
		Jobs:       *jobs,
		CORSOrigin: *cors,
		BenchFile:  *benchFile,
	})

	hs := &http.Server{
		Addr:              *listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      *timeout + 15*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	fmt.Printf("prismd %s listen=%s data_dir=%s timeout=%s\n", version.Version, *listen, dir, *timeout)

	errCh := make(chan error, 1)
	go func() {
		errCh <- hs.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		fmt.Printf("prismd shutting down (%s)\n", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return hs.Shutdown(ctx)
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
