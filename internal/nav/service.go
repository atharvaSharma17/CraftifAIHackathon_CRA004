package nav

import (
	"fmt"
	"sync"
	"time"
)

type Snapshot struct {
	PerceptionInput     InputStatus       `json:"perception_input"`
	LatestObservation   *Observation      `json:"latest_observation,omitempty"`
	Occupancy           []Occupancy       `json:"occupancy"`
	EdgeStates          map[string]string `json:"edge_states"`
	Route               *Route            `json:"route"`
	Mission             *Mission          `json:"mission"`
	Vehicle             Simulator         `json:"vehicle"`
	PerceptionConnected bool              `json:"perception_connected"`
	CalibrationVerified bool              `json:"calibration_verified"`
	LastFrameAt         time.Time         `json:"last_frame_at"`
	GraphVersion        int64             `json:"graph_version"`
	ReceivedFrames      int64             `json:"received_frames"`
	AcceptedSource      string            `json:"accepted_source"`
}
type Service struct {
	input        InputStatus
	latest       *Observation
	mu           sync.Mutex
	engine       *Engine
	store        *Store
	mission      *Mission
	route        *Route
	vehicle      Simulator
	graphVersion int64
	routeVersion int64
	frames       int64
	subscribers  map[chan Snapshot]bool
}

func NewService(m Map, s *Store) (*Service, error) {
	var routeVersion, graphVersion int64
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(json_extract(payload,'$.route_version')),0) FROM events WHERE kind='route'`).Scan(&routeVersion); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM events WHERE kind='occupancy'`).Scan(&graphVersion); err != nil {
		return nil, err
	}
	engine := NewEngine(m)
	engine.Reset(time.Now().UTC(), "service-start")
	// A restart invalidates previous clearance evidence and receives a fresh
	// graph version, even when no camera has reconnected yet.
	if _, err := s.Commit(nil, []Pending{{"occupancy", engine.Snapshot()}}); err != nil {
		return nil, err
	}
	graphVersion++
	return &Service{engine: engine, store: s, routeVersion: routeVersion, graphVersion: graphVersion, subscribers: map[chan Snapshot]bool{}, vehicle: Simulator{Simulated: true, Status: "idle"}}, nil
}
func (s *Service) Map() Map { s.mu.Lock(); defer s.mu.Unlock(); return s.engine.Map }
func (s *Service) snapshot() Snapshot {
	return Snapshot{PerceptionInput: s.input, LatestObservation: s.latest, Occupancy: s.engine.Snapshot(), EdgeStates: s.engine.EdgeStates(), Route: s.route, Mission: s.mission, Vehicle: s.vehicle, PerceptionConnected: s.engine.Connected(), CalibrationVerified: s.engine.Map.CalibrationVerified, LastFrameAt: s.engine.LastReceived, GraphVersion: s.graphVersion, ReceivedFrames: s.frames, AcceptedSource: "pipegen-deepstream"}
}

type InputStatus struct {
	Mode   string `json:"mode"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func (s *Service) SetInputStatus(mode, status, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.input = InputStatus{Mode: mode, Status: status, Error: message}
	s.publish()
}
func (s *Service) Snapshot() Snapshot { s.mu.Lock(); defer s.mu.Unlock(); return s.snapshot() }
func (s *Service) publish() {
	snap := s.snapshot()
	for ch := range s.subscribers {
		select {
		case ch <- snap:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- snap:
			default:
			}
		}
	}
}
func (s *Service) Subscribe() (chan Snapshot, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan Snapshot, 1)
	s.subscribers[ch] = true
	ch <- s.snapshot()
	return ch, func() { s.mu.Lock(); delete(s.subscribers, ch); s.mu.Unlock() }
}
func (s *Service) Ingest(o Observation, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := o.Validate(s.engine.Map); err != nil {
		return err
	}
	if err := s.store.Check(o); err != nil {
		return err
	}
	// Mutate a copy so a failed SQLite transaction cannot advance live state.
	clone := *s.engine
	clone.Regions = map[string]*region{}
	for id, r := range s.engine.Regions {
		copy := *r
		clone.Regions[id] = &copy
	}
	resetChanged := false
	if clone.Session != o.SessionID || (clone.connected && (now.Sub(clone.LastReceived) > time.Duration(clone.Map.Policy.StaleAfterMS)*time.Millisecond || o.VideoTSMS-clone.LastVideoMS > clone.Map.Policy.StaleAfterMS)) {
		resetChanged = clone.Reset(now, o.EventID)
	}
	changed := clone.Apply(o, now) || resetChanged
	pending := []Pending{{"observation", o}}
	version := s.graphVersion
	var route *Route
	if changed {
		version++
		pending = append(pending, Pending{"occupancy", clone.Snapshot()})
		if s.mission != nil {
			r := s.plan(clone, *s.mission, version, "Camera occupancy changed", o.EventID)
			route = &r
			pending = append(pending, Pending{"route", r})
		}
	}
	if _, err := s.store.Commit(&o, pending); err != nil {
		return err
	}
	s.engine = &clone
	s.graphVersion = version
	s.frames++
	s.latest = &o
	if route != nil {
		s.setRoute(route)
	}
	s.publish()
	return nil
}
func (s *Service) plan(e Engine, m Mission, version int64, reason, cause string) Route {
	if s.vehicle.Node != "" && s.mission != nil && s.mission.ID == m.ID {
		m.Start = s.vehicle.Node
	}
	r := Plan(e.Map, e.EdgeStates(), m)
	r.Version = s.routeVersion + 1
	r.GraphVersion = version
	r.Reason = reason
	r.Cause = cause
	oldStates, newStates := s.engine.EdgeStates(), e.EdgeStates()
	for _, edge := range e.Map.Edges {
		if oldStates[edge.ID] != newStates[edge.ID] {
			r.Changes = append(r.Changes, EdgeChange{EdgeID: edge.ID, Before: oldStates[edge.ID], After: newStates[edge.ID]})
			r.Reason += fmt.Sprintf("; aisle %s: %s → %s", edge.ID, oldStates[edge.ID], newStates[edge.ID])
		}
	}
	if s.route != nil {
		r.PreviousEdges = append([]string{}, s.route.Edges...)
	}
	if s.vehicle.Progress > 0 {
		r.Reason += "; simulated vehicle reset to last reached waypoint"
	}
	return r
}
func (s *Service) setRoute(r *Route) {
	s.route = r
	s.routeVersion = r.Version
	s.vehicle.Progress = 0
	s.vehicle.EdgeID = ""
	s.vehicle.Status = "held"
	if len(r.Nodes) > 0 {
		s.place(r.Nodes[0])
		if len(r.Edges) > 0 {
			s.vehicle.Status = "moving"
		} else {
			s.vehicle.Status = "arrived"
		}
	}
}
func (s *Service) place(id string) {
	s.vehicle.Node = id
	for _, n := range s.engine.Map.Nodes {
		if n.ID == id {
			s.vehicle.X = n.X
			s.vehicle.Y = n.Y
		}
	}
}
func (s *Service) StartMission(m Mission) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	nodeOK := func(id string) bool {
		for _, n := range s.engine.Map.Nodes {
			if n.ID == id {
				return true
			}
		}
		return false
	}
	profileOK := false
	for _, v := range s.engine.Map.Profiles {
		if v.ID == m.ProfileID {
			profileOK = true
		}
	}
	if !nodeOK(m.Start) || !nodeOK(m.Goal) || !profileOK {
		return fmt.Errorf("unknown start, goal or vehicle profile")
	}
	m.ID = fmt.Sprintf("mission-%d", time.Now().UnixNano())
	r := Plan(s.engine.Map, s.engine.EdgeStates(), m)
	r.Version = s.routeVersion + 1
	r.GraphVersion = s.graphVersion
	r.Reason = "Mission requested; unknown lanes use the configured conservative policy"
	if _, err := s.store.Commit(nil, []Pending{{"mission", m}, {"route", r}}); err != nil {
		return err
	}
	s.mission = &m
	s.place(m.Start)
	s.setRoute(&r)
	s.publish()
	return nil
}
func (s *Service) Tick(now time.Time, dt float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := *s.engine
	clone.Regions = map[string]*region{}
	for id, r := range s.engine.Regions {
		copy := *r
		clone.Regions[id] = &copy
	}
	if clone.Stale(now) {
		version := s.graphVersion + 1
		pending := []Pending{{"occupancy", clone.Snapshot()}}
		var route *Route
		if s.mission != nil {
			r := s.plan(clone, *s.mission, version, "Perception stale; affected regions are unknown", "perception-stale")
			route = &r
			pending = append(pending, Pending{"route", r})
		}
		if _, err := s.store.Commit(nil, pending); err != nil {
			return err
		}
		s.engine = &clone
		s.graphVersion = version
		if route != nil {
			s.setRoute(route)
		}
	} else {
		s.engine = &clone
	}
	if s.route != nil && s.vehicle.Status == "moving" {
		index := -1
		for i, n := range s.route.Nodes {
			if n == s.vehicle.Node {
				index = i
				break
			}
		}
		if index >= 0 && index < len(s.route.Edges) {
			var edge Edge
			for _, e := range s.engine.Map.Edges {
				if e.ID == s.route.Edges[index] {
					edge = e
				}
			}
			speed := edge.SpeedLimit
			for _, v := range s.engine.Map.Profiles {
				if v.ID == s.mission.ProfileID && v.SpeedCap < speed {
					speed = v.SpeedCap
				}
			}
			s.vehicle.EdgeID = edge.ID
			s.vehicle.Progress += dt * speed / edge.Length
			var target Node
			for _, n := range s.engine.Map.Nodes {
				if n.ID == s.route.Nodes[index+1] {
					target = n
				}
			}
			if s.vehicle.Progress >= 1 {
				s.place(target.ID)
				s.vehicle.Progress = 0
				s.vehicle.EdgeID = ""
				if index+1 == len(s.route.Nodes)-1 {
					s.vehicle.Status = "arrived"
				}
			} else {
				for _, n := range s.engine.Map.Nodes {
					if n.ID == s.vehicle.Node {
						s.vehicle.X = n.X + (target.X-n.X)*s.vehicle.Progress
						s.vehicle.Y = n.Y + (target.Y-n.Y)*s.vehicle.Progress
					}
				}
			}
		}
	}
	s.publish()
	return nil
}
