package nav

import "time"

type region struct {
	Occupancy
	lastHit    int64
	lastFrame  int64
	clearSince int64
	hasClear   bool
}
type Engine struct {
	Map          Map
	Regions      map[string]*region
	Session      string
	LastFrame    int64
	LastVideoMS  int64
	LastReceived time.Time
	connected    bool
}

func NewEngine(m Map) *Engine {
	e := &Engine{Map: m, Regions: map[string]*region{}}
	for _, r := range m.ROIs {
		e.Regions[r.ID] = &region{Occupancy: Occupancy{ROIID: r.ID, State: "UNKNOWN"}}
	}
	return e
}
func inside(x, y float64, polygon [][2]float64) bool {
	hit := false
	j := len(polygon) - 1
	for i, p := range polygon {
		q := polygon[j]
		if (p[1] > y) != (q[1] > y) && x < (q[0]-p[0])*(y-p[1])/(q[1]-p[1])+p[0] {
			hit = !hit
		}
		j = i
	}
	return hit
}
func (e *Engine) Apply(o Observation, now time.Time) bool {
	if e.Session != o.SessionID {
		e.Reset(now, o.EventID)
		e.Session = o.SessionID
	}
	e.LastFrame = o.FrameIndex
	e.LastVideoMS = o.VideoTSMS
	e.LastReceived = now
	e.connected = true
	changed := false
	p := e.Map.Policy
	for _, roi := range e.Map.ROIs {
		r := e.Regions[roi.ID]
		before := r.State
		hit := false
		for _, obj := range o.Objects {
			relevant := false
			for _, c := range p.ObstacleClasses {
				if c == obj.ClassLabel {
					relevant = true
				}
			}
			if relevant && obj.Confidence >= p.Confidence && inside(obj.BBox[0]+obj.BBox[2]/2, obj.BBox[1]+obj.BBox[3], roi.Polygon) {
				hit = true
			}
		}
		if !e.Map.CalibrationVerified {
			r.State = "UNKNOWN"
			r.EvidenceFrames = 0
			r.hasClear = false
		} else if hit {
			if r.EvidenceFrames == 0 || o.FrameIndex != r.lastFrame+1 {
				r.EvidenceFrames = 0
				r.SinceMS = o.VideoTSMS
			}
			r.EvidenceFrames++
			r.lastFrame = o.FrameIndex
			r.lastHit = o.VideoTSMS
			r.hasClear = false
			if r.State == "CLEARING" {
				r.State = "BLOCKED"
			} else if r.State != "BLOCKED" {
				if r.EvidenceFrames >= p.SuspectFrames && o.VideoTSMS-r.SinceMS >= p.BlockAfterMS {
					r.State = "BLOCKED"
				} else if r.EvidenceFrames >= p.SuspectFrames {
					r.State = "SUSPECTED_BLOCKED"
				}
			}
		} else {
			r.EvidenceFrames = 0
			if !r.hasClear {
				r.clearSince = o.VideoTSMS
				r.hasClear = true
			}
			elapsed := o.VideoTSMS - r.clearSince
			if elapsed >= p.ClearAfterMS {
				r.State = "CLEAR"
			} else if (r.State == "BLOCKED" || r.State == "SUSPECTED_BLOCKED") && elapsed >= p.ClearingAfterMS {
				r.State = "CLEARING"
			}
		}
		if r.State != before {
			r.UpdatedAt = now
			r.Cause = o.EventID
			changed = true
		}
	}
	return changed
}
func (e *Engine) Reset(now time.Time, cause string) bool {
	changed := false
	for _, r := range e.Regions {
		if r.State != "UNKNOWN" {
			changed = true
		}
		r.Occupancy = Occupancy{ROIID: r.ROIID, State: "UNKNOWN", UpdatedAt: now, Cause: cause}
		r.EvidenceFrames = 0
		r.hasClear = false
	}
	e.connected = false
	return changed
}
func (e *Engine) Stale(now time.Time) bool {
	if e.connected && now.Sub(e.LastReceived) > time.Duration(e.Map.Policy.StaleAfterMS)*time.Millisecond {
		return e.Reset(now, "perception-stale")
	}
	return false
}
func (e *Engine) Connected() bool { return e.connected }
func (e *Engine) Snapshot() []Occupancy {
	out := []Occupancy{}
	for _, roi := range e.Map.ROIs {
		out = append(out, e.Regions[roi.ID].Occupancy)
	}
	return out
}
func (e *Engine) EdgeStates() map[string]string {
	states := map[string]string{}
	rank := map[string]int{"CLEAR": 0, "UNKNOWN": 1, "SUSPECTED_BLOCKED": 2, "CLEARING": 3, "BLOCKED": 4}
	for _, edge := range e.Map.Edges {
		states[edge.ID] = "UNKNOWN"
	}
	covered := map[string]bool{}
	for _, roi := range e.Map.ROIs {
		state := e.Regions[roi.ID].State
		for _, edge := range roi.EdgeIDs {
			if !covered[edge] || rank[state] > rank[states[edge]] {
				states[edge] = state
			}
			covered[edge] = true
		}
	}
	return states
}
