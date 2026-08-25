// Command server is the runnable entry point for the leaf-intake fixation
// gate service. It opens the SQLite store, seeds the demo directory, runs the
// startup recovery scan, and begins serving the HTTP API.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"verdant-leaf-fixation-gate/internal/device"
	"verdant-leaf-fixation-gate/internal/httpapi"
	"verdant-leaf-fixation-gate/internal/store"
)

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "verdant-leaf.db"
	}

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	app := httpapi.NewApp(st, device.NewAdapter(device.DefaultScripts()))

	// Seed the demo directory and run the startup recovery scan.
	ctx := context.Background()
	if err := app.SeedDemo(ctx); err != nil {
		log.Fatalf("seed demo catalog: %v", err)
	}
	if pending, err := app.Recovery.Recover(ctx); err != nil {
		log.Fatalf("recovery scan: %v", err)
	} else if len(pending) > 0 {
		log.Printf("recovered %d pending device attempt(s)", len(pending))
	}

	server := httpapi.NewServerWithApp(app)

	srv := &http.Server{
		Addr:              addr,
		Handler:           server,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("verdant-leaf-fixation-gate listening on %s (db=%s)", addr, dbPath)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server exited: %v", err)
	}
}
