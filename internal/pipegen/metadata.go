// Package pipegen adapts the frame_json.h file exporter supplied with PipeGen.
// Coordinates are nvstreammux pixels, not detector-input or source-video pixels.
package pipegen

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"navforge/internal/nav"
	"os"
	"os/exec"
	"time"
)

type Object struct {
	ID         json.RawMessage `json:"id"`
	Label      *string         `json:"label"`
	Confidence *float64        `json:"confidence"`
	BBox       *struct {
		Left   *float64 `json:"left"`
		Top    *float64 `json:"top"`
		Width  *float64 `json:"width"`
		Height *float64 `json:"height"`
	} `json:"bbox"`
}

type Frame struct {
	Index    *int64    `json:"frame"`
	SourceID *int      `json:"source_id"`
	PTS      *int64    `json:"pts_ns"`
	Width    *int      `json:"width"`
	Height   *int      `json:"height"`
	Objects  *[]Object `json:"objects"`
}

func NewSession() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "pipegen-" + hex.EncodeToString(b[:])
}

func Normalize(f Frame, m nav.Map, session, mode string) (nav.Observation, error) {
	o := nav.Observation{}
	if f.Index == nil || f.SourceID == nil || f.PTS == nil || f.Width == nil || f.Height == nil || f.Objects == nil {
		return o, fmt.Errorf("PipeGen frame is missing required metadata")
	}
	if *f.SourceID != 0 || *f.PTS < 0 || *f.Width != m.ImageSize[0] || *f.Height != m.ImageSize[1] {
		return o, fmt.Errorf("source_id must be 0 and metadata resolution must match calibration (%dx%d); received source=%d %dx%d", m.ImageSize[0], m.ImageSize[1], *f.SourceID, *f.Width, *f.Height)
	}
	o = nav.Observation{SchemaVersion: "1.0", Source: "pipegen-deepstream", InputMode: mode,
		EventID: fmt.Sprintf("%s-%d", session, *f.Index), CameraID: m.CameraID, SessionID: session,
		FrameIndex: *f.Index, VideoTSMS: *f.PTS / 1_000_000, ImageSize: m.ImageSize,
		CalibrationVersion: m.CalibrationVersion, Objects: []nav.Object{}}
	for _, raw := range *f.Objects {
		b := raw.BBox
		if raw.Label == nil || raw.Confidence == nil || b == nil || b.Left == nil || b.Top == nil || b.Width == nil || b.Height == nil {
			return o, fmt.Errorf("object is missing label, confidence or bbox fields")
		}
		values := []float64{*raw.Confidence, *b.Left, *b.Top, *b.Width, *b.Height}
		for _, value := range values {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return o, fmt.Errorf("nonfinite object metadata")
			}
		}
		x, y, w, h := *b.Left, *b.Top, *b.Width, *b.Height
		// The writer rounds each coordinate to 0.1 px. Clip only that rounding
		// tolerance; genuinely invalid tracker boxes must never become clear frames.
		if x < -0.11 || y < -0.11 || w <= 0 || h <= 0 || x+w > float64(*f.Width)+0.11 || y+h > float64(*f.Height)+0.11 {
			return o, fmt.Errorf("out-of-bounds PipeGen bbox on frame %d", *f.Index)
		}
		x2, y2 := math.Min(x+w, float64(*f.Width)), math.Min(y+h, float64(*f.Height))
		x, y = math.Max(x, 0), math.Max(y, 0)
		track := ""
		if len(raw.ID) > 0 && string(raw.ID) != "null" {
			var id string
			if raw.ID[0] == '"' {
				if err := json.Unmarshal(raw.ID, &id); err != nil {
					return o, err
				}
			} else {
				var n json.Number
				if err := json.Unmarshal(raw.ID, &n); err != nil {
					return o, err
				}
				id = n.String()
			}
			if id != "18446744073709551615" && id != "-1" {
				track = id
			}
		}
		o.Objects = append(o.Objects, nav.Object{TrackID: track, ClassLabel: *raw.Label, Confidence: *raw.Confidence, BBox: nav.Box{x, y, x2 - x, y2 - y}})
	}
	return o, o.Validate(m)
}

// Stream consumes the actual exporter envelope without loading a whole recording.
// Optional vendor keys are skipped; required fields are never fabricated.
func Stream(r io.Reader, maxFilter float64, consume func(Frame) error) error {
	d := json.NewDecoder(r)
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return fmt.Errorf("expected PipeGen document object")
	}
	pipelineOK, filterOK, seenFrames := false, false, false
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return err
		}
		switch key {
		case "pipeline":
			var p string
			if err = d.Decode(&p); err != nil {
				return err
			}
			pipelineOK = p == "video_json_export"
		case "min_confidence":
			var c *float64
			if err = d.Decode(&c); err != nil {
				return err
			}
			filterOK = c != nil && *c >= 0 && *c <= maxFilter
		case "frames":
			if seenFrames || !pipelineOK || !filterOK {
				return fmt.Errorf("expected video_json_export header and min_confidence <= occupancy confidence before frames")
			}
			seenFrames = true
			token, err = d.Token()
			if err != nil {
				return err
			}
			if token != json.Delim('[') {
				return fmt.Errorf("frames must be an array")
			}
			for d.More() {
				var f Frame
				if err = d.Decode(&f); err != nil {
					return err
				}
				if err = consume(f); err != nil {
					return err
				}
			}
			if _, err = d.Token(); err != nil {
				return err
			}
		default:
			var ignored json.RawMessage
			if err = d.Decode(&ignored); err != nil {
				return err
			}
		}
	}
	if _, err = d.Token(); err != nil {
		return err
	}
	if !seenFrames {
		return fmt.Errorf("missing frames array")
	}
	return nil
}

type Options struct {
	Path, Session, Container string
	Follow                   bool
	Speed                    float64
}

type tailReader struct {
	ctx    context.Context
	file   *os.File
	follow bool
	offset int64
}

func (r *tailReader) Read(b []byte) (int, error) {
	for {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		n, err := r.file.Read(b)
		r.offset += int64(n)
		if n > 0 || err != io.EOF || !r.follow {
			return n, err
		}
		info, e := r.file.Stat()
		if e != nil {
			return 0, e
		}
		if info.Size() < r.offset {
			return 0, fmt.Errorf("metadata file truncated; restart input with a new session")
		}
		if err := pause(r.ctx, 100*time.Millisecond); err != nil {
			return 0, err
		}
	}
}
func pause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Run replays at video time by default. Follow tails a freshly created, flushed
// exporter file and ends at the document's closing brace. Each launch has a new
// session; a caller-specified session is useful for transport retry deduplication.
func Run(ctx context.Context, m nav.Map, opt Options, consume func(nav.Observation) error) error {
	if math.IsNaN(opt.Speed) || math.IsInf(opt.Speed, 0) || opt.Speed < 0 {
		return fmt.Errorf("speed must be finite and nonnegative")
	}
	if opt.Session == "" {
		opt.Session = NewSession()
	}
	if len(opt.Session) > 100 {
		return fmt.Errorf("session ID exceeds 100 characters")
	}
	var reader io.Reader
	if opt.Container != "" {
		if !opt.Follow {
			return fmt.Errorf("container metadata requires follow mode")
		}
		// Read the exporter file through Docker stdout. No container port, host
		// listener exposure, or bind mount is needed. Use a fresh path each run.
		command := exec.CommandContext(ctx, "docker", "exec", opt.Container, "tail", "-c", "+1", "-F", opt.Path)
		command.Stderr = os.Stderr
		stdout, err := command.StdoutPipe()
		if err != nil {
			return err
		}
		if err = command.Start(); err != nil {
			return err
		}
		defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
		reader = stdout
	} else {
		var file *os.File
		for {
			var err error
			file, err = os.Open(opt.Path)
			if err == nil {
				break
			}
			if !opt.Follow || !os.IsNotExist(err) {
				return err
			}
			if err = pause(ctx, 100*time.Millisecond); err != nil {
				return err
			}
		}
		defer file.Close()
		reader = &tailReader{ctx: ctx, file: file, follow: opt.Follow}
	}
	mode := "replay"
	if opt.Follow {
		mode = "live"
	}
	lastIndex, lastPTS := int64(-1), int64(-1)
	var started time.Time
	var firstPTS int64
	return Stream(reader, m.Policy.Confidence, func(f Frame) error {
		o, err := Normalize(f, m, opt.Session, mode)
		if err != nil {
			return err
		}
		if o.FrameIndex <= lastIndex || o.VideoTSMS <= lastPTS {
			return fmt.Errorf("non-monotonic PipeGen frame or timestamp at %d", o.FrameIndex)
		}
		if started.IsZero() {
			started = time.Now()
			firstPTS = o.VideoTSMS
		}
		if opt.Speed > 0 {
			target := started.Add(time.Duration(float64(o.VideoTSMS-firstPTS) / opt.Speed * float64(time.Millisecond)))
			if err = pause(ctx, time.Until(target)); err != nil {
				return err
			}
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = consume(o); err != nil {
			return err
		}
		lastIndex, lastPTS = o.FrameIndex, o.VideoTSMS
		return nil
	})
}
