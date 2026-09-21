// Package store persists the bench in a single SQLite database.
package store

import (
	"database/sql"
	"encoding/json"
	"time"

	_ "modernc.org/sqlite"

	"voicebench/internal/model"
)

// Store is the application database handle.
type Store struct{ DB *sql.DB }

// Open opens (creating if needed) the database.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite",
		path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

// Close releases the handle.
func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate() error {
	if _, err := s.DB.Exec(schema); err != nil {
		return err
	}
	_, err := s.DB.Exec(conflictSchema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS speakers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS utterances (
  speaker_id TEXT NOT NULL REFERENCES speakers(id),
  id TEXT NOT NULL,
  pronunciation_version TEXT NOT NULL,
  window_from_frame INTEGER NOT NULL,
  window_to_frame INTEGER NOT NULL,
  fps INTEGER NOT NULL,
  frame_count INTEGER NOT NULL,
  active_set TEXT NOT NULL DEFAULT 'v1',
  PRIMARY KEY(speaker_id, id)
);
CREATE TABLE IF NOT EXISTS frames (
  speaker_id TEXT NOT NULL,
  utter_id TEXT NOT NULL,
  frame INTEGER NOT NULL,
  t REAL NOT NULL,
  present INTEGER NOT NULL,
  in_window INTEGER NOT NULL,
  PRIMARY KEY(speaker_id, utter_id, frame)
);
CREATE TABLE IF NOT EXISTS candidates (
  id INTEGER PRIMARY KEY,
  speaker_id TEXT NOT NULL,
  utter_id TEXT NOT NULL,
  cand_version TEXT NOT NULL,
  frame INTEGER NOT NULL,
  band INTEGER NOT NULL,
  freq REAL NOT NULL,
  energy REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cand_lookup
  ON candidates(speaker_id, utter_id, cand_version, frame, band, freq);
CREATE TABLE IF NOT EXISTS auto_assignments (
  id INTEGER PRIMARY KEY,
  speaker_id TEXT NOT NULL,
  utter_id TEXT NOT NULL,
  cand_version TEXT NOT NULL,
  frame INTEGER NOT NULL,
  band INTEGER NOT NULL,
  candidate_id INTEGER NOT NULL,
  confidence REAL NOT NULL,
  UNIQUE(speaker_id, utter_id, cand_version, frame, band)
);
CREATE TABLE IF NOT EXISTS vowel_intervals (
  id INTEGER PRIMARY KEY,
  speaker_id TEXT NOT NULL,
  utter_id TEXT NOT NULL,
  start_frame INTEGER NOT NULL,
  end_frame INTEGER NOT NULL,
  label TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS spectrum (
  speaker_id TEXT NOT NULL,
  utter_id TEXT NOT NULL,
  frame INTEGER NOT NULL,
  bin INTEGER NOT NULL,
  freq_lo REAL NOT NULL,
  freq_hi REAL NOT NULL,
  energy REAL NOT NULL,
  PRIMARY KEY(speaker_id, utter_id, frame, bin)
);
CREATE TABLE IF NOT EXISTS operations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  speaker_id TEXT NOT NULL,
  utter_id TEXT NOT NULL,
  cand_version TEXT NOT NULL,
  kind TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  author TEXT NOT NULL DEFAULT 'researcher',
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  items TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS run_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  at TEXT NOT NULL DEFAULT (datetime('now')),
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT ''
);
`

// CountSpeakers reports whether data is loaded.
func (s *Store) CountSpeakers() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM speakers`).Scan(&n)
	return n, err
}

// ReplaceBundle wipes application data and imports a bundle. Candidate and
// auto-assignment ids are preserved verbatim, so an exported original point is
// never rewritten by a round trip.
func (s *Store) ReplaceBundle(b *model.Bundle, logEntry string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM run_log`, `DELETE FROM operations`, `DELETE FROM spectrum`,
		`DELETE FROM vowel_intervals`, `DELETE FROM auto_assignments`,
		`DELETE FROM candidates`, `DELETE FROM frames`, `DELETE FROM utterances`,
		`DELETE FROM speakers`,
	} {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	if err := loadBundle(tx, b); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO meta(key,value) VALUES('schema',?)`,
		b.Schema); err != nil {
		return err
	}
	if logEntry != "" {
		if err := logTx(tx, "system", "import", logEntry); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func loadBundle(tx *sql.Tx, b *model.Bundle) error {
	for _, sp := range b.Speakers {
		if _, err := tx.Exec(
			`INSERT INTO speakers(id,name) VALUES(?,?)`, sp.ID, sp.Name); err != nil {
			return err
		}
	}
	for _, u := range b.Utterances {
		active := ""
		if b.ActiveSet != nil {
			active = b.ActiveSet[u.SpeakerID+"/"+u.ID]
		}
		if active == "" {
			active = "v1"
		}
		if _, err := tx.Exec(`INSERT INTO utterances
(speaker_id,id,pronunciation_version,window_from_frame,window_to_frame,
 fps,frame_count,active_set) VALUES(?,?,?,?,?,?,?,?)`,
			u.SpeakerID, u.ID, u.PronVer, u.WindowFrom, u.WindowTo,
			u.FPS, u.Frames, active); err != nil {
			return err
		}
	}
	for _, f := range b.Frames {
		if _, err := tx.Exec(`INSERT INTO frames
(speaker_id,utter_id,frame,t,present,in_window) VALUES(?,?,?,?,?,?)`,
			f.SpeakerID, f.UtterID, f.Frame, f.T, btoi(f.Present),
			btoi(f.InWindow)); err != nil {
			return err
		}
	}
	for _, c := range b.Candidates {
		if _, err := tx.Exec(`INSERT INTO candidates
(id,speaker_id,utter_id,cand_version,frame,band,freq,energy)
 VALUES(?,?,?,?,?,?,?,?)`,
			c.ID, c.SpeakerID, c.UtterID, c.Version, c.Frame, c.Band,
			c.Freq, c.Energy); err != nil {
			return err
		}
	}
	for _, a := range b.Auto {
		if _, err := tx.Exec(`INSERT INTO auto_assignments
(id,speaker_id,utter_id,cand_version,frame,band,candidate_id,confidence)
 VALUES(?,?,?,?,?,?,?,?)`,
			a.ID, a.SpeakerID, a.UtterID, a.Version, a.Frame, a.Band,
			a.CandidateID, a.Confidence); err != nil {
			return err
		}
	}
	for _, v := range b.Vowels {
		if _, err := tx.Exec(`INSERT INTO vowel_intervals
(id,speaker_id,utter_id,start_frame,end_frame,label)
 VALUES(?,?,?,?,?,?)`,
			v.ID, v.SpeakerID, v.UtterID, v.StartFrame, v.EndFrame, v.Label); err != nil {
			return err
		}
	}
	for _, sc := range b.Spectrum {
		if _, err := tx.Exec(`INSERT INTO spectrum
(speaker_id,utter_id,frame,bin,freq_lo,freq_hi,energy)
 VALUES(?,?,?,?,?,?,?)`,
			sc.SpeakerID, sc.UtterID, sc.Frame, sc.Bin,
			sc.FreqLo, sc.FreqHi, sc.Energy); err != nil {
			return err
		}
	}
	for _, op := range b.Operations {
		if err := insertOpWithID(tx, op); err != nil {
			return err
		}
	}
	for _, r := range b.RunLog {
		if _, err := tx.Exec(`INSERT INTO run_log(id,at,actor,action,detail)
 VALUES(?,?,?,?,?)`, r.ID,
			r.At.UTC().Format("2006-01-02 15:04:05"),
			r.Actor, r.Action, r.Detail); err != nil {
			return err
		}
	}
	return nil
}

func insertOpWithID(tx *sql.Tx, op model.Operation) error {
	raw, err := json.Marshal(op.Items)
	if err != nil {
		return err
	}
	created := op.CreatedAt
	if created == "" {
		created = time.Now().UTC().Format("2006-01-02 15:04:05")
	}
	_, err = tx.Exec(`INSERT INTO operations
(id,speaker_id,utter_id,cand_version,kind,note,author,created_at,items)
 VALUES(?,?,?,?,?,?,?,?,?)`,
		op.ID, op.SpeakerID, op.UtterID, op.Version, op.Kind, op.Note,
		op.Author, created, string(raw))
	return err
}

func logTx(tx *sql.Tx, actor, action, detail string) error {
	_, err := tx.Exec(
		`INSERT INTO run_log(at,actor,action,detail) VALUES(?,?,?,?)`,
		time.Now().UTC().Format("2006-01-02 15:04:05"), actor, action, detail)
	return err
}

// Log records a user-visible action.
func (s *Store) Log(actor, action, detail string) error {
	return s.withTx(func(tx *sql.Tx) error { return logTx(tx, actor, action, detail) })
}

func (s *Store) withTx(fn func(*sql.Tx) error) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
