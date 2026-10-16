package main

import (
	"context"
	"flag"
	"log"
	"navforge/internal/nav"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	config := flag.String("config", "configs/warehouse.json", "warehouse map and occupancy policy")
	dbPath := flag.String("db", "data/navforge.db", "SQLite event store")
	flag.Parse()
	m, err := nav.LoadMap(*config)
	if err != nil {
		log.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(*dbPath), 0700); err != nil {
		log.Fatal(err)
	}
	store, err := nav.OpenStore(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	service := nav.NewService(m, store)
	if _, err = store.Commit(nil, []nav.Pending{{Kind: "startup", Payload: map[string]any{"map_id": m.ID, "calibration_version": m.CalibrationVersion, "calibration_verified": m.CalibrationVerified, "policy": m.Policy, "note": "Perception and missions reset on restart; historical evidence retained"}}}); err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	api := &http.Server{Addr: "127.0.0.1:8080", Handler: service.PublicHandler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	servers := []*http.Server{api}
	token := os.Getenv("NAVFORGE_INGEST_TOKEN")
	if len(token) >= 24 {
		servers = append(servers, &http.Server{Addr: "127.0.0.1:8081", Handler: service.IngestHandler(token), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, MaxHeaderBytes: 16 << 10})
	} else {
		log.Print("Ingestion disabled: set NAVFORGE_INGEST_TOKEN to at least 24 characters")
	}
	for _, server := range servers {
		go func() {
			log.Printf("listening on http://%s", server.Addr)
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Print(err)
				stop()
			}
		}()
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	last := time.Now()
	for {
		select {
		case now := <-ticker.C:
			dt := now.Sub(last).Seconds()
			last = now
			if dt > 1 {
				dt = 1
			}
			if err = service.Tick(now, dt); err != nil {
				log.Printf("tick failed: %v", err)
			}
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			for _, server := range servers {
				server.Shutdown(shutdown)
			}
			cancel()
			return
		}
	}
}
