package nav

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"
)

type Node struct {
	ID    string  `json:"id"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Label string  `json:"label"`
}
type Edge struct {
	ID            string  `json:"id"`
	From          string  `json:"from"`
	To            string  `json:"to"`
	Length        float64 `json:"length"`
	Width         float64 `json:"width"`
	SpeedLimit    float64 `json:"speed_limit"`
	Bidirectional bool    `json:"bidirectional"`
	TurnPenalty   float64 `json:"turn_penalty"`
}
type ROI struct {
	ID      string       `json:"id"`
	EdgeIDs []string     `json:"edge_ids"`
	Polygon [][2]float64 `json:"polygon"`
}
type VehicleProfile struct {
	ID             string  `json:"id"`
	Width          float64 `json:"width"`
	Clearance      float64 `json:"clearance"`
	SpeedCap       float64 `json:"speed_cap"`
	MaxTurnPenalty float64 `json:"max_turn_penalty"`
}
type Policy struct {
	Confidence      float64  `json:"confidence_threshold"`
	SuspectFrames   int      `json:"suspect_frames"`
	BlockAfterMS    int64    `json:"block_after_ms"`
	ClearingAfterMS int64    `json:"clearing_after_ms"`
	ClearAfterMS    int64    `json:"clear_after_ms"`
	StaleAfterMS    int64    `json:"stale_after_ms"`
	RiskPenalty     float64  `json:"risk_penalty"`
	UnknownPenalty  float64  `json:"unknown_penalty"`
	StrictUnknown   bool     `json:"strict_unknown"`
	ObstacleClasses []string `json:"obstacle_classes"`
}
type Map struct {
	ID                  string           `json:"id"`
	CalibrationVersion  string           `json:"calibration_version"`
	CameraID            string           `json:"camera_id"`
	ImageSize           [2]int           `json:"image_size"`
	CalibrationVerified bool             `json:"calibration_verified"`
	Nodes               []Node           `json:"nodes"`
	Edges               []Edge           `json:"edges"`
	ROIs                []ROI            `json:"rois"`
	Profiles            []VehicleProfile `json:"vehicle_profiles"`
	Policy              Policy           `json:"policy"`
}

func LoadMap(path string) (Map, error) {
	var m Map
	b, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	if err = json.Unmarshal(b, &m); err != nil {
		return m, err
	}
	return m, m.Validate()
}
func (m Map) Validate() error {
	if m.CameraID == "" || m.CalibrationVersion == "" || m.ImageSize[0] <= 0 || m.ImageSize[1] <= 0 {
		return fmt.Errorf("camera and calibration metadata required")
	}
	nodes := map[string]bool{}
	edges := map[string]bool{}
	rois := map[string]bool{}
	profiles := map[string]bool{}
	for _, n := range m.Nodes {
		if n.ID == "" || nodes[n.ID] || math.IsNaN(n.X) || math.IsNaN(n.Y) {
			return fmt.Errorf("invalid/duplicate node")
		}
		nodes[n.ID] = true
	}
	for _, e := range m.Edges {
		if e.ID == "" || edges[e.ID] || !nodes[e.From] || !nodes[e.To] || e.Length <= 0 || e.Width <= 0 || e.SpeedLimit <= 0 || e.TurnPenalty < 0 {
			return fmt.Errorf("invalid edge %s", e.ID)
		}
		edges[e.ID] = true
	}
	for _, r := range m.ROIs {
		if r.ID == "" || rois[r.ID] || len(r.Polygon) < 3 || len(r.EdgeIDs) == 0 {
			return fmt.Errorf("invalid ROI %s", r.ID)
		}
		rois[r.ID] = true
		for _, e := range r.EdgeIDs {
			if !edges[e] {
				return fmt.Errorf("unknown ROI edge %s", e)
			}
		}
		for _, p := range r.Polygon {
			if p[0] < 0 || p[1] < 0 || p[0] > float64(m.ImageSize[0]) || p[1] > float64(m.ImageSize[1]) {
				return fmt.Errorf("ROI outside image")
			}
		}
	}
	for _, v := range m.Profiles {
		if v.ID == "" || profiles[v.ID] || v.Width <= 0 || v.Clearance < 0 || v.SpeedCap <= 0 || v.MaxTurnPenalty < 0 {
			return fmt.Errorf("invalid vehicle")
		}
		profiles[v.ID] = true
	}
	p := m.Policy
	if len(nodes) == 0 || len(edges) == 0 || len(profiles) == 0 || p.Confidence < 0 || p.Confidence > 1 || p.SuspectFrames < 1 || p.BlockAfterMS <= 0 || p.ClearingAfterMS <= 0 || p.ClearAfterMS < p.ClearingAfterMS || p.StaleAfterMS <= 0 || p.RiskPenalty < 0 || p.UnknownPenalty < 0 || len(p.ObstacleClasses) == 0 {
		return fmt.Errorf("invalid graph/policy")
	}
	return nil
}

// Frame observations include an empty objects array for a successfully inferred
// clear frame. Silence is never interpreted as evidence of a clear aisle.
type Object struct {
	TrackID    string     `json:"track_id,omitempty"`
	ClassLabel string     `json:"class_label"`
	Confidence float64    `json:"confidence"`
	BBox       [4]float64 `json:"bbox_xywh_px"`
}
type Observation struct {
	SchemaVersion      string   `json:"schema_version"`
	EventID            string   `json:"event_id"`
	Source             string   `json:"source"`
	CameraID           string   `json:"camera_id"`
	SessionID          string   `json:"session_id"`
	FrameIndex         int64    `json:"frame_index"`
	VideoTSMS          int64    `json:"video_ts_ms"`
	ImageSize          [2]int   `json:"image_size"`
	CalibrationVersion string   `json:"calibration_version"`
	Objects            []Object `json:"objects"`
}

func (o Observation) Validate(m Map) error {
	if o.SchemaVersion != "1.0" || o.EventID == "" || len(o.EventID) > 200 || o.SessionID == "" || len(o.SessionID) > 100 || o.Source != "pipegen-deepstream" || o.CameraID != m.CameraID || o.ImageSize != m.ImageSize || o.CalibrationVersion != m.CalibrationVersion || o.FrameIndex < 0 || o.VideoTSMS < 0 || o.Objects == nil || len(o.Objects) > 256 {
		return fmt.Errorf("invalid frame contract or calibration metadata")
	}
	for _, obj := range o.Objects {
		b := obj.BBox
		if obj.ClassLabel == "" || len(obj.ClassLabel) > 100 || len(obj.TrackID) > 100 || obj.Confidence < 0 || obj.Confidence > 1 || b[0] < 0 || b[1] < 0 || b[2] <= 0 || b[3] <= 0 || b[0]+b[2] > float64(o.ImageSize[0]) || b[1]+b[3] > float64(o.ImageSize[1]) {
			return fmt.Errorf("invalid object bounding box/class/confidence")
		}
	}
	return nil
}

type Occupancy struct {
	ROIID          string    `json:"roi_id"`
	State          string    `json:"state"`
	UpdatedAt      time.Time `json:"updated_at"`
	Cause          string    `json:"causal_event_id"`
	EvidenceFrames int       `json:"evidence_frames"`
	SinceMS        int64     `json:"since_video_ms"`
}
type Mission struct {
	ID        string `json:"id"`
	Start     string `json:"start_node"`
	Goal      string `json:"goal_node"`
	ProfileID string `json:"vehicle_profile_id"`
}
type Route struct {
	Version      int64    `json:"route_version"`
	GraphVersion int64    `json:"graph_version"`
	MissionID    string   `json:"mission_id"`
	Nodes        []string `json:"nodes"`
	Edges        []string `json:"edges"`
	Distance     float64  `json:"distance"`
	Cost         float64  `json:"cost"`
	ETA          float64  `json:"eta_seconds"`
	Status       string   `json:"status"`
	Reason       string   `json:"reason"`
	Cause        string   `json:"causal_event_id"`
	Simulated    bool     `json:"simulated"`
}
type Event struct {
	ID        int64           `json:"id"`
	Kind      string          `json:"kind"`
	CreatedAt time.Time       `json:"created_at"`
	Payload   json.RawMessage `json:"payload"`
}
type Simulator struct {
	Simulated bool    `json:"simulated"`
	Status    string  `json:"status"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Node      string  `json:"node"`
	EdgeID    string  `json:"edge_id,omitempty"`
	Progress  float64 `json:"progress"`
}
