package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"voicebench/internal/model"
)

// ListSpeakers returns all speakers in id order.
func (s *Store) ListSpeakers() ([]model.Speaker, error) {
	rows, err := s.DB.Query(`SELECT id,name FROM speakers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Speaker
	for rows.Next() {
		var sp model.Speaker
		if err := rows.Scan(&sp.ID, &sp.Name); err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// ListUtterances returns all pronunciation versions.
func (s *Store) ListUtterances() ([]model.Utterance, error) {
	rows, err := s.DB.Query(`SELECT speaker_id,id,pronunciation_version,
 window_from_frame,window_to_frame,fps,frame_count FROM utterances
 ORDER BY speaker_id,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Utterance
	for rows.Next() {
		var u model.Utterance
		if err := rows.Scan(&u.SpeakerID, &u.ID, &u.PronVer, &u.WindowFrom,
			&u.WindowTo, &u.FPS, &u.Frames); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ActiveSet returns the active candidate-set tag for an utterance.
func (s *Store) ActiveSet(speaker, utter string) (string, error) {
	var v string
	err := s.DB.QueryRow(
		`SELECT active_set FROM utterances WHERE speaker_id=? AND id=?`,
		speaker, utter).Scan(&v)
	return v, err
}

// SetActiveSet switches the candidate set used for editing.
func (s *Store) SetActiveSet(speaker, utter, version string) error {
	res, err := s.DB.Exec(
		`UPDATE utterances SET active_set=? WHERE speaker_id=? AND id=?`,
		version, speaker, utter)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("utterance %s/%s not found", speaker, utter)
	}
	return nil
}

// Frames returns one utterance's frame metadata.
func (s *Store) Frames(speaker, utter string) ([]model.FrameMeta, error) {
	rows, err := s.DB.Query(`SELECT speaker_id,utter_id,frame,t,present,in_window
 FROM frames WHERE speaker_id=? AND utter_id=? ORDER BY frame`, speaker, utter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.FrameMeta
	for rows.Next() {
		var f model.FrameMeta
		var p, w int
		if err := rows.Scan(&f.SpeakerID, &f.UtterID, &f.Frame, &f.T, &p, &w); err != nil {
			return nil, err
		}
		f.Present, f.InWindow = p == 1, w == 1
		out = append(out, f)
	}
	return out, rows.Err()
}

// Candidates returns candidates of one set for one utterance.
func (s *Store) Candidates(speaker, utter, version string) ([]model.Candidate, error) {
	rows, err := s.DB.Query(`SELECT id,speaker_id,utter_id,cand_version,
 frame,band,freq,energy FROM candidates
 WHERE speaker_id=? AND utter_id=? AND cand_version=?
 ORDER BY frame,band,freq`, speaker, utter, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Candidate
	for rows.Next() {
		var c model.Candidate
		if err := rows.Scan(&c.ID, &c.SpeakerID, &c.UtterID, &c.Version,
			&c.Frame, &c.Band, &c.Freq, &c.Energy); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AutoPoints returns auto assignments of one set.
func (s *Store) AutoPoints(speaker, utter, version string) ([]model.AutoPoint, error) {
	rows, err := s.DB.Query(`SELECT id,speaker_id,utter_id,cand_version,
 frame,band,candidate_id,confidence FROM auto_assignments
 WHERE speaker_id=? AND utter_id=? AND cand_version=?
 ORDER BY frame,band`, speaker, utter, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AutoPoint
	for rows.Next() {
		var a model.AutoPoint
		if err := rows.Scan(&a.ID, &a.SpeakerID, &a.UtterID, &a.Version,
			&a.Frame, &a.Band, &a.CandidateID, &a.Confidence); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Vowels returns vowel intervals of an utterance.
func (s *Store) Vowels(speaker, utter string) ([]model.VowelInterval, error) {
	rows, err := s.DB.Query(`SELECT id,speaker_id,utter_id,start_frame,
 end_frame,label FROM vowel_intervals WHERE speaker_id=? AND utter_id=?
 ORDER BY start_frame`, speaker, utter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.VowelInterval
	for rows.Next() {
		var v model.VowelInterval
		if err := rows.Scan(&v.ID, &v.SpeakerID, &v.UtterID,
			&v.StartFrame, &v.EndFrame, &v.Label); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Spectrum returns spectrogram bins of an utterance.
func (s *Store) Spectrum(speaker, utter string) ([]model.SpectrumCell, error) {
	rows, err := s.DB.Query(`SELECT speaker_id,utter_id,frame,bin,freq_lo,
 freq_hi,energy FROM spectrum WHERE speaker_id=? AND utter_id=?
 ORDER BY frame,bin`, speaker, utter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SpectrumCell
	for rows.Next() {
		var c model.SpectrumCell
		if err := rows.Scan(&c.SpeakerID, &c.UtterID, &c.Frame, &c.Bin,
			&c.FreqLo, &c.FreqHi, &c.Energy); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Operations returns the sparse revision history of one utterance, newest last.
func (s *Store) Operations(speaker, utter string) ([]model.Operation, error) {
	rows, err := s.DB.Query(`SELECT id,speaker_id,utter_id,cand_version,kind,
 note,author,created_at,items FROM operations
 WHERE speaker_id=? AND utter_id=? ORDER BY id`, speaker, utter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Operation
	for rows.Next() {
		op, err := scanOp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, rows.Err()
}

func scanOp(rows *sql.Rows) (model.Operation, error) {
	var op model.Operation
	var raw string
	if err := rows.Scan(&op.ID, &op.SpeakerID, &op.UtterID, &op.Version,
		&op.Kind, &op.Note, &op.Author, &op.CreatedAt, &raw); err != nil {
		return op, err
	}
	if err := json.Unmarshal([]byte(raw), &op.Items); err != nil {
		return op, err
	}
	return op, nil
}

// AddOperation appends one sparse revision and returns its new id.
func (s *Store) AddOperation(op model.Operation) (int64, error) {
	raw, err := json.Marshal(op.Items)
	if err != nil {
		return 0, err
	}
	created := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := s.DB.Exec(`INSERT INTO operations
(speaker_id,utter_id,cand_version,kind,note,author,created_at,items)
 VALUES(?,?,?,?,?,?,?,?)`,
		op.SpeakerID, op.UtterID, op.Version, op.Kind, op.Note,
		op.Author, created, string(raw))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UndoOperation removes the most recent operation of an utterance.
func (s *Store) UndoOperation(speaker, utter string) (bool, error) {
	res, err := s.DB.Exec(`DELETE FROM operations WHERE id =
 (SELECT id FROM operations WHERE speaker_id=? AND utter_id=?
  ORDER BY id DESC LIMIT 1)`, speaker, utter)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RunLogTail returns recent run records.
func (s *Store) RunLogTail(limit int) ([]model.RunLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.DB.Query(`SELECT id,at,actor,action,detail FROM
 (SELECT * FROM run_log ORDER BY id DESC LIMIT ?) ORDER BY id`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.RunLog
	for rows.Next() {
		var r model.RunLog
		var at string
		if err := rows.Scan(&r.ID, &at, &r.Actor, &r.Action, &r.Detail); err != nil {
			return nil, err
		}
		t, _ := time.Parse("2006-01-02 15:04:05", at)
		r.At = t
		out = append(out, r)
	}
	return out, rows.Err()
}
