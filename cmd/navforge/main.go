package main

import (
	"context"
	"flag"
	"log"
	"navforge/internal/nav"
	"navforge/internal/pipegen"
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
	metadata := flag.String("pipegen-metadata", "", "PipeGen metadata.json to replay or follow")
	follow := flag.Bool("pipegen-follow", false, "follow a fresh, growing PipeGen JSON export")
	speed := flag.Float64("pipegen-speed", 1, "metadata playback speed; 0 imports without pacing")
	session := flag.String("pipegen-session", "", "input session ID; defaults to a fresh random ID")
	container := flag.String("pipegen-container", "", "optional Docker container holding the growing metadata file")
	video := flag.String("video", "", "optional local MP4 served to the dashboard")
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
	service, err := nav.NewService(m, store)
	if err != nil {
		log.Fatal(err)
	}
	if _, err = store.Commit(nil, []nav.Pending{{Kind: "startup", Payload: map[string]any{"map_id": m.ID, "calibration_version": m.CalibrationVersion, "calibration_verified": m.CalibrationVerified, "policy": m.Policy, "note": "Perception and missions reset on restart; historical evidence retained"}}}); err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	handler := service.PublicHandler()
	if *video != "" {
		path, e := filepath.Abs(*video)
		if e != nil {
			log.Fatal(e)
		}
		info, e := os.Stat(path)
		if e != nil || info.IsDir() {
			log.Fatalf("video must be an existing file: %s", path)
		}
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/video", func(w http.ResponseWriter, r *http.Request) {
			// Recordings can change between runs; never combine cached byte ranges.
			w.Header().Set("Cache-Control", "no-store")
			http.ServeFile(w, r, path)
		})
		mux.Handle("/", handler)
		handler = mux
	}
	api := &http.Server{Addr: "127.0.0.1:8080", Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
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
	inputDone := make(chan struct{})
	if *metadata != "" {
		mode := "replay"
		if *follow {
			mode = "live"
		}
		service.SetInputStatus(mode, "reading", "")
		go func() {
			defer close(inputDone)
			err := pipegen.Run(ctx, m, pipegen.Options{Path: *metadata, Container: *container, Follow: *follow, Speed: *speed, Session: *session}, func(o nav.Observation) error { return service.Ingest(o, time.Now().UTC()) })
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				log.Printf("PipeGen input failed: %v", err)
				service.SetInputStatus(mode, "failed", err.Error())
			} else {
				log.Print("PipeGen input complete")
				service.SetInputStatus(mode, "complete", "")
			}
		}()
	} else {
		close(inputDone)
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
			<-inputDone
			return
		}
	}
}
