package nav

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"time"
)

var ErrConflict = errors.New("observation conflict")

type Store struct{ db *sql.DB }

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY AUTOINCREMENT,kind TEXT NOT NULL,created_at TEXT NOT NULL,payload TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS frames(event_id TEXT PRIMARY KEY,camera_id TEXT NOT NULL,session_id TEXT NOT NULL,frame_index INTEGER NOT NULL,video_ts_ms INTEGER NOT NULL,payload TEXT NOT NULL,UNIQUE(camera_id,session_id,frame_index));
 CREATE TABLE IF NOT EXISTS sessions(camera_id TEXT NOT NULL,session_id TEXT NOT NULL,last_frame INTEGER NOT NULL,last_video_ms INTEGER NOT NULL,active INTEGER NOT NULL,PRIMARY KEY(camera_id,session_id));`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Check(o Observation) error {
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM frames WHERE event_id=?", o.EventID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: duplicate event", ErrConflict)
	}
	var frame, ts int64
	var active int
	err := s.db.QueryRow("SELECT last_frame,last_video_ms,active FROM sessions WHERE camera_id=? AND session_id=?", o.CameraID, o.SessionID).Scan(&frame, &ts, &active)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if active == 0 || o.FrameIndex <= frame || o.VideoTSMS <= ts {
		return fmt.Errorf("%w: retired session or out-of-order frame/timestamp", ErrConflict)
	}
	return nil
}

type Pending struct {
	Kind    string
	Payload any
}

func (s *Store) Commit(o *Observation, pending []Pending) ([]Event, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if o != nil {
		b, _ := json.Marshal(o)
		_, err = tx.Exec("INSERT INTO frames VALUES(?,?,?,?,?,?)", o.EventID, o.CameraID, o.SessionID, o.FrameIndex, o.VideoTSMS, string(b))
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec("UPDATE sessions SET active=0 WHERE camera_id=? AND session_id<>?", o.CameraID, o.SessionID); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(`INSERT INTO sessions VALUES(?,?,?,?,1) ON CONFLICT(camera_id,session_id) DO UPDATE SET last_frame=excluded.last_frame,last_video_ms=excluded.last_video_ms,active=1`, o.CameraID, o.SessionID, o.FrameIndex, o.VideoTSMS); err != nil {
			return nil, err
		}
	}
	events := []Event{}
	for _, p := range pending {
		b, err := json.Marshal(p.Payload)
		if err != nil {
			return nil, err
		}
		now := time.Now().UTC()
		res, err := tx.Exec("INSERT INTO events(kind,created_at,payload) VALUES(?,?,?)", p.Kind, now.Format(time.RFC3339Nano), string(b))
		if err != nil {
			return nil, err
		}
		id, _ := res.LastInsertId()
		events = append(events, Event{ID: id, Kind: p.Kind, CreatedAt: now, Payload: b})
	}
	return events, tx.Commit()
}
func (s *Store) Events(limit int) ([]Event, error) {
	rows, err := s.db.Query("SELECT id,kind,created_at,payload FROM events ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var ts, b string
		if err = rows.Scan(&e.ID, &e.Kind, &ts, &b); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, ts)
		e.Payload = json.RawMessage(b)
		out = append(out, e)
	}
	return out, rows.Err()
}
