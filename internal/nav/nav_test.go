package nav

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Fixtures are synthetic and live only in tests/temp databases. They are never
// exposed as a production inference producer or submitted as PipeGen evidence.
func testMap(t *testing.T) Map {
	t.Helper()
	m, err := LoadMap("../../configs/warehouse.json")
	if err != nil {
		t.Fatal(err)
	}
	m.CalibrationVerified = true
	return m
}
func frame(m Map, index, ts int64, hit bool) Observation {
	o := Observation{SchemaVersion: "1.0", EventID: fmt.Sprintf("synthetic-test-%d", index), Source: "pipegen-deepstream", CameraID: m.CameraID, SessionID: "synthetic-test-session", FrameIndex: index, VideoTSMS: ts, ImageSize: m.ImageSize, CalibrationVersion: m.CalibrationVersion, Objects: []Object{}}
	if hit {
		o.Objects = append(o.Objects, Object{ClassLabel: "person", Confidence: 0.95, BBox: [4]float64{350, 250, 80, 200}})
	}
	return o
}
func newTestService(t *testing.T, m Map) (*Service, *Store) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	s, err := NewService(m, store)
	if err != nil {
		t.Fatal(err)
	}
	return s, store
}

func TestPlannerConstraints(t *testing.T) {
	m := testMap(t)
	mission := Mission{ID: "test", Start: "A", Goal: "G", ProfileID: "forklift"}
	states := NewEngine(m).EdgeStates()
	r := Plan(m, states, mission)
	if !reflect.DeepEqual(r.Edges, []string{"A-B", "B-C", "C-G"}) {
		t.Fatalf("baseline: %+v", r)
	}
	states["B-C"] = "BLOCKED"
	r = Plan(m, states, mission)
	if !reflect.DeepEqual(r.Edges, []string{"A-B", "B-D", "D-E", "E-C", "C-G"}) {
		t.Fatalf("detour: %+v", r)
	}
	m.Policy.StrictUnknown = true
	if Plan(m, states, mission).Status != "no_route" {
		t.Fatal("strict policy allowed unknown edge")
	}
	m.Policy.StrictUnknown = false
	m.Profiles[0].Width = 4
	if Plan(m, states, mission).Status != "no_route" {
		t.Fatal("vehicle clearance ignored")
	}
	m.Profiles[0].Width = 1.3
	m.Profiles[0].MaxTurnPenalty = 0
	if Plan(m, states, mission).Status != "no_route" {
		t.Fatal("turn restrictions ignored")
	}
	m.Profiles[0].MaxTurnPenalty = 5
	m.Edges[0].Bidirectional = false
	mission.Start = "G"
	mission.Goal = "A"
	if Plan(m, states, mission).Status != "no_route" {
		t.Fatal("one-way restriction ignored")
	}
}
func TestOccupancyHysteresis(t *testing.T) {
	m := testMap(t)
	e := NewEngine(m)
	now := time.Now()
	apply := func(i, ts int64, hit bool) {
		e.Apply(frame(m, i, ts, hit), now.Add(time.Duration(ts)*time.Millisecond))
	}
	state := func() string { return e.Regions["aisle_BC"].State }
	apply(0, 0, false)
	apply(1, 1000, false)
	if state() != "UNKNOWN" {
		t.Fatal("clear too early")
	}
	apply(2, 2000, false)
	if state() != "CLEAR" {
		t.Fatal(state())
	}
	apply(3, 2100, true)
	apply(4, 2200, true)
	if state() != "CLEAR" {
		t.Fatal("transient closed aisle")
	}
	apply(5, 2300, true)
	if state() != "SUSPECTED_BLOCKED" {
		t.Fatal(state())
	}
	apply(6, 2600, true)
	if state() != "BLOCKED" {
		t.Fatal(state())
	}
	apply(7, 2700, false)
	apply(8, 3700, false)
	if state() != "CLEARING" {
		t.Fatal(state())
	}
	apply(9, 3800, true)
	if state() != "BLOCKED" {
		t.Fatal("occupancy during clearing must reblock")
	}
	apply(10, 3900, false)
	apply(11, 5900, false)
	if state() != "CLEAR" {
		t.Fatal(state())
	}
	if !e.Stale(now.Add(8*time.Second)) || state() != "UNKNOWN" || e.Connected() {
		t.Fatal("stale frame retained clearance")
	}
}
func TestUnverifiedAndGeometry(t *testing.T) {
	m := testMap(t)
	m.CalibrationVerified = false
	e := NewEngine(m)
	e.Apply(frame(m, 0, 0, false), time.Now())
	e.Apply(frame(m, 1, 2500, false), time.Now())
	if e.Regions["aisle_BC"].State != "UNKNOWN" {
		t.Fatal("unverified map cleared")
	}
	m.CalibrationVerified = true
	e = NewEngine(m)
	for i := int64(0); i < 5; i++ {
		o := frame(m, i, i*250, true)
		o.Objects[0].BBox = [4]float64{1000, 0, 20, 40}
		e.Apply(o, time.Now())
	}
	if e.Regions["aisle_BC"].State == "BLOCKED" {
		t.Fatal("object outside ROI blocked it")
	}
}
func TestIngestionCausalChainAndReplayDefense(t *testing.T) {
	m := testMap(t)
	s, store := newTestService(t, m)
	if err := s.StartMission(Mission{Start: "A", Goal: "G", ProfileID: "forklift"}); err != nil {
		t.Fatal(err)
	}
	initial := s.Snapshot().Route.Version
	now := time.Now()
	for i := int64(0); i < 4; i++ {
		if err := s.Ingest(frame(m, i, i*250, true), now.Add(time.Duration(i)*250*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	snap := s.Snapshot()
	if snap.EdgeStates["B-C"] != "BLOCKED" || snap.Route.Version <= initial || snap.Route.Cause != "synthetic-test-2" || len(snap.Route.Edges) != 5 {
		t.Fatalf("causal detour missing: %+v", snap)
	}
	if err := s.Ingest(frame(m, 3, 750, true), now); err == nil {
		t.Fatal("duplicate accepted")
	}
	older := frame(m, 2, 500, true)
	older.EventID = "different-event-id"
	if err := s.Ingest(older, now); err == nil {
		t.Fatal("out of order accepted")
	}
	if err := s.Tick(now.Add(4*time.Second), 0); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().EdgeStates["B-C"] != "UNKNOWN" {
		t.Fatal("TTL failed")
	}
	replacement := frame(m, 0, 0, false)
	replacement.SessionID = "new-session"
	replacement.EventID = "new-session-frame"
	if err := s.Ingest(replacement, now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	retired := frame(m, 10, 2500, false)
	if err := s.Ingest(retired, now.Add(6*time.Second)); err == nil {
		t.Fatal("retired session reactivated")
	}
	restarted, err := NewService(m, store)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Snapshot().PerceptionConnected || restarted.Snapshot().EdgeStates["B-C"] != "UNKNOWN" {
		t.Fatal("restart trusted stale state")
	}
	if err = restarted.Ingest(replacement, now); err == nil {
		t.Fatal("restart lost duplicate history")
	}
	if err = restarted.StartMission(Mission{Start: "A", Goal: "G", ProfileID: "forklift"}); err != nil {
		t.Fatal(err)
	}
	if restarted.Snapshot().Route.Version <= s.Snapshot().Route.Version {
		t.Fatal("route version reused after restart")
	}
	events, err := store.Events(100)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, e := range events {
		kinds[e.Kind] = true
	}
	for _, kind := range []string{"observation", "occupancy", "route", "mission"} {
		if !kinds[kind] {
			t.Fatal("missing durable event", kind)
		}
	}
}
func TestHTTPBoundary(t *testing.T) {
	m := testMap(t)
	s, _ := newTestService(t, m)
	token := strings.Repeat("s", 32)
	ingest := s.IngestHandler(token)
	o := frame(m, 0, 0, true)
	body, _ := json.Marshal(o)
	request := func(h http.Handler, path, auth string, b []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request(ingest, "/internal/perception/events", "", body); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(s.PublicHandler(), "/internal/perception/events", token, body); w.Code != 404 {
		t.Fatal("public API exposed ingestion", w.Code)
	}
	malformed := append(append([]byte{}, body...), []byte(" {}")...)
	if w := request(ingest, "/internal/perception/events", token, malformed); w.Code != 400 {
		t.Fatal("trailing JSON accepted", w.Code)
	}
	if w := request(ingest, "/internal/perception/events", token, body); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(ingest, "/internal/perception/events", token, body); w.Code != 409 {
		t.Fatal("duplicate HTTP event accepted")
	}
	o.EventID = "bad-geometry"
	o.FrameIndex = 1
	o.VideoTSMS = 1
	o.Objects[0].BBox[2] = 9999
	bad, _ := json.Marshal(o)
	if w := request(ingest, "/internal/perception/events", token, bad); w.Code != 400 {
		t.Fatal("bad geometry accepted")
	}
	r := httptest.NewRequest("POST", "/api/missions", strings.NewReader(`{"start_node":"A","goal_node":"G","vehicle_profile_id":"forklift"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://untrusted.example")
	w := httptest.NewRecorder()
	s.PublicHandler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin mutation accepted")
	}
}
func TestFailedCommitDoesNotMutateLiveState(t *testing.T) {
	m := testMap(t)
	s, store := newTestService(t, m)
	store.Close()
	before := s.Snapshot()
	if err := s.StartMission(Mission{Start: "A", Goal: "G", ProfileID: "forklift"}); err == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	after := s.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed persistence mutated live state")
	}
}
