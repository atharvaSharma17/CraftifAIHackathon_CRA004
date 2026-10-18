package pipegen

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"navforge/internal/nav"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mapForTest(t *testing.T) nav.Map {
	t.Helper()
	m, err := nav.LoadMap("../../configs/warehouse.pipegen.json")
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func capturedFrames(t *testing.T) []Frame {
	t.Helper()
	f, err := os.Open("testdata/metadata.sample.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var frames []Frame
	if err = Stream(f, 0.65, func(f Frame) error { frames = append(frames, f); return nil }); err != nil {
		t.Fatal(err)
	}
	return frames
}
func TestCapturedMetadataThroughHTTP(t *testing.T) {
	m := mapForTest(t)
	store, err := nav.OpenStore(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s, err := nav.NewService(m, store)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(s.IngestHandler("test-token-with-at-least-24-chars"))
	defer server.Close()
	frames := capturedFrames(t)
	for _, f := range frames {
		o, err := Normalize(f, m, "captured-sample-replay", "replay")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(o)
		req, _ := http.NewRequest("POST", server.URL+"/internal/perception/events", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-token-with-at-least-24-chars")
		req.Header.Set("Content-Type", "application/json")
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 202 {
			t.Fatalf("%d %s", res.StatusCode, b)
		}
	}
	snap := s.Snapshot()
	if snap.ReceivedFrames != 4 || snap.LatestObservation.FrameIndex != 1253 || len(snap.LatestObservation.Objects) != 6 {
		t.Fatalf("bad snapshot %+v", snap)
	}
	if snap.LatestObservation.Objects[0].ClassLabel != "forklift" || snap.LatestObservation.InputMode != "replay" {
		t.Fatal("lost class or provenance")
	}
	for _, state := range snap.EdgeStates {
		if state != "UNKNOWN" {
			t.Fatal("unverified metadata changed occupancy")
		}
	}
}

func TestNormalizeRejectsMissingWrongResolutionAndInvalidBoxes(t *testing.T) {
	m := mapForTest(t)
	frames := capturedFrames(t)
	if _, err := Normalize(Frame{}, m, "test", "replay"); err == nil {
		t.Fatal("accepted missing frame metadata")
	}
	f := frames[1]
	badWidth := 1280
	f.Width = &badWidth
	if _, err := Normalize(f, m, "test", "replay"); err == nil {
		t.Fatal("accepted mismatched resolution")
	}
	f = frames[1]
	objects := append([]Object{}, (*f.Objects)...)
	obj := objects[0]
	box := *obj.BBox
	box.Left = nil
	obj.BBox = &box
	objects[0] = obj
	f.Objects = &objects
	if _, err := Normalize(f, m, "test", "replay"); err == nil {
		t.Fatal("accepted missing bbox coordinate")
	}
}

func TestIncompleteOrOverfilteredExportIsNotClear(t *testing.T) {
	for _, input := range []string{
		`{"pipeline":"video_json_export","min_confidence":0.9,"frames":[]}`,
		`{"pipeline":"video_json_export","min_confidence":null,"frames":[]}`,
		`{"pipeline":"unknown","min_confidence":0,"frames":[]}`,
		`{"pipeline":"video_json_export","min_confidence":0,"frames":[`,
	} {
		if err := Stream(strings.NewReader(input), 0.65, func(Frame) error { t.Fatal("consumed invalid frame"); return nil }); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestFollowWaitsForPartialFrameAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metadata.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	file.WriteString(`{"pipeline":"video_json_export","min_confidence":0,"frames":[{"frame":0`)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	consumed := make(chan nav.Observation, 1)
	done := make(chan error, 1)
	m := mapForTest(t)
	go func() {
		done <- Run(ctx, m, Options{Path: path, Follow: true, Speed: 0}, func(o nav.Observation) error { consumed <- o; return nil })
	}()
	select {
	case <-consumed:
		t.Fatal("partial frame consumed")
	case <-time.After(150 * time.Millisecond):
	}
	file.WriteString(`,"source_id":0,"pts_ns":0,"width":1920,"height":1080,"objects":[]}`)
	// No newline or closing array: follow must publish a complete frame immediately.
	select {
	case o := <-consumed:
		if o.InputMode != "live" || len(o.Objects) != 0 {
			t.Fatal("wrong observation")
		}
	case <-ctx.Done():
		t.Fatal("did not follow appended data")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled partial document succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("follower ignored cancellation")
	}
}

func TestExportAdapterCausesRouteChangeWithSyntheticVerifiedCalibration(t *testing.T) {
	// Integration mechanics only: the full-frame ROI and temporal repetition are
	// synthetic fixtures, never evidence of physical calibration or model accuracy.
	m := mapForTest(t)
	m.CalibrationVerified = true
	m.ROIs[0].Polygon = [][2]float64{{0, 0}, {1920, 0}, {1920, 1080}, {0, 1080}}
	store, err := nav.OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s, err := nav.NewService(m, store)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.StartMission(nav.Mission{Start: "A", Goal: "G", ProfileID: "forklift"}); err != nil {
		t.Fatal(err)
	}
	f := capturedFrames(t)[3]
	for i := int64(0); i < 20; i++ {
		idx, pts := i, i*41_666_666
		f.Index = &idx
		f.PTS = &pts
		o, err := Normalize(f, m, "synthetic-calibration-test", "replay")
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Ingest(o, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	snap := s.Snapshot()
	if snap.EdgeStates["B-C"] != "BLOCKED" {
		t.Fatal("persistent detection did not block lane")
	}
	if strings.Join(snap.Route.Edges, ",") != "A-B,B-D,D-E,E-C,C-G" || snap.Route.Cause == "" {
		t.Fatalf("no causal detour: %+v", snap.Route)
	}
}
