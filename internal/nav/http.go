package nav

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return fmt.Errorf("Content-Type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON object")
	}
	return nil
}
func (s *Service) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		snap := s.Snapshot()
		JSON(w, 200, map[string]any{"status": "ok", "perception_connected": snap.PerceptionConnected, "calibration_verified": snap.CalibrationVerified, "simulated_vehicle": true})
	})
	mux.HandleFunc("GET /api/map", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, s.Map()) })
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, s.Snapshot()) })
	mux.HandleFunc("GET /api/routes/current", func(w http.ResponseWriter, r *http.Request) { JSON(w, 200, s.Snapshot().Route) })
	mux.HandleFunc("GET /api/occupancy", func(w http.ResponseWriter, r *http.Request) {
		snap := s.Snapshot()
		JSON(w, 200, map[string]any{"regions": snap.Occupancy, "edges": snap.EdgeStates, "graph_version": snap.GraphVersion})
	})
	mux.HandleFunc("POST /api/missions", func(w http.ResponseWriter, r *http.Request) {
		var m Mission
		if err := decode(w, r, &m); err != nil {
			JSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if err := s.StartMission(m); err != nil {
			JSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		JSON(w, 201, s.Snapshot())
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		limit := 100
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 1000 {
				JSON(w, 400, map[string]string{"error": "limit must be 1..1000"})
				return
			}
			limit = n
		}
		events, err := s.store.Events(limit)
		if err != nil {
			JSON(w, 500, map[string]string{"error": "event store unavailable"})
			return
		}
		JSON(w, 200, events)
	})
	mux.HandleFunc("GET /api/stream", func(w http.ResponseWriter, r *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unavailable", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		ch, cancel := s.Subscribe()
		defer cancel()
		heartbeat := time.NewTicker(15 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case snap := <-ch:
				b, _ := json.Marshal(snap)
				controller := http.NewResponseController(w)
				controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if _, err := fmt.Fprintf(w, "event: state\ndata: %s\n\n", b); err != nil {
					return
				}
				f.Flush()
			case <-heartbeat.C:
				if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
					return
				}
				f.Flush()
			}
		}
	})
	return localBrowserGuard(mux)
}

// Browser clients may read through the Next.js same-origin proxy. Mutating
// requests reject cross-origin browsers; ingestion has its own listener/token.
func localBrowserGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" && (r.Header.Get("Sec-Fetch-Site") == "cross-site" || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://localhost:3000" && r.Header.Get("Origin") != "http://127.0.0.1:3000")) {
			JSON(w, 403, map[string]string{"error": "cross-origin mutation rejected"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Service) IngestHandler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/perception/events", func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			JSON(w, 401, map[string]string{"error": "invalid ingestion credential"})
			return
		}
		var o Observation
		if err := decode(w, r, &o); err != nil {
			JSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if err := o.Validate(s.Map()); err != nil {
			JSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if err := s.Ingest(o, time.Now()); err != nil {
			if errors.Is(err, ErrConflict) {
				JSON(w, 409, map[string]string{"error": err.Error()})
			} else {
				JSON(w, 500, map[string]string{"error": "observation could not be committed"})
			}
			return
		}
		JSON(w, 202, map[string]string{"event_id": o.EventID, "status": "accepted"})
	})
	return mux
}
