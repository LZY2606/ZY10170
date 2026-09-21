package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"voicebench/internal/model"
)

const conflictSchema = `
CREATE TABLE IF NOT EXISTS remap_refs (
  speaker_id TEXT NOT NULL,
  utter_id TEXT NOT NULL,
  from_version TEXT NOT NULL,
  to_version TEXT NOT NULL,
  from_op_id INTEGER NOT NULL,
  frame INTEGER NOT NULL,
  band INTEGER NOT NULL,
  old_candidate_id INTEGER NOT NULL,
  status TEXT NOT NULL,
  unique_candidate_id INTEGER NOT NULL DEFAULT 0,
  options TEXT NOT NULL DEFAULT '[]',
  resolved INTEGER NOT NULL DEFAULT 0,
  chosen_candidate_id INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(speaker_id, utter_id, from_op_id, frame, band)
);
`

// EnsureConflictSchema creates the remap_refs table.
func (s *Store) EnsureConflictSchema() error {
	_, err := s.DB.Exec(conflictSchema)
	return err
}

// SaveRemapRefs replaces the remap references of one utterance.
func (s *Store) SaveRemapRefs(speaker, utter, fromVer, toVer string,
	refs []model.Ref) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`DELETE FROM remap_refs WHERE speaker_id=? AND utter_id=?`,
		speaker, utter); err != nil {
		return err
	}
	for _, r := range refs {
		opt, _ := json.Marshal(r.Options)
		if _, err := tx.Exec(`INSERT INTO remap_refs
(speaker_id,utter_id,from_version,to_version,from_op_id,frame,band,
 old_candidate_id,status,unique_candidate_id,options)
 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			speaker, utter, fromVer, toVer, r.FromOpID, r.Frame, r.Band,
			r.OldCandID, r.Status, r.UniqueCand, string(opt)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RemapRefs returns stored remap references of one utterance.
func (s *Store) RemapRefs(speaker, utter string) ([]model.Ref, error) {
	rows, err := s.DB.Query(`SELECT from_op_id,frame,band,old_candidate_id,
 status,unique_candidate_id,options,resolved,chosen_candidate_id
 FROM remap_refs WHERE speaker_id=? AND utter_id=? AND resolved=0
 ORDER BY from_op_id,frame,band`, speaker, utter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Ref
	for rows.Next() {
		var r model.Ref
		var opt string
		var isResolved int
		var chosenID int64
		_ = isResolved
		_ = chosenID
		if err := rows.Scan(&r.FromOpID, &r.Frame, &r.Band, &r.OldCandID,
			&r.Status, &r.UniqueCand, &opt, &isResolved, &chosenID); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(opt), &r.Options)
		out = append(out, r)
	}
	return out, rows.Err()
}

// PendingConflicts lists unresolved conflict refs (ambiguous mappings with two
// or more options that a researcher must choose between).
func (s *Store) PendingConflicts(speaker, utter string) ([]model.Ref, error) {
	rows, err := s.DB.Query(`SELECT from_op_id,frame,band,old_candidate_id,
 status,unique_candidate_id,options FROM remap_refs
 WHERE speaker_id=? AND utter_id=? AND status=? AND resolved=0
 ORDER BY from_op_id,frame,band`, speaker, utter, model.StatusConflict)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Ref
	for rows.Next() {
		var r model.Ref
		var opt string
		if err := rows.Scan(&r.FromOpID, &r.Frame, &r.Band, &r.OldCandID,
			&r.Status, &r.UniqueCand, &opt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(opt), &r.Options)
		out = append(out, r)
	}
	return out, rows.Err()
}

// ResolveConflict records the researcher's explicit choice for one ambiguous
// item. The candidate id must be one of the stored options.
func (s *Store) ResolveConflict(speaker, utter string,
	fromOpID int64, frame, band int, candidateID int64) error {
	var opt string
	var status string
	err := s.DB.QueryRow(`SELECT options,status FROM remap_refs
 WHERE speaker_id=? AND utter_id=? AND from_op_id=? AND frame=? AND band=?`,
		speaker, utter, fromOpID, frame, band).Scan(&opt, &status)
	if err == sql.ErrNoRows {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}
	var options []int64
	if err := json.Unmarshal([]byte(opt), &options); err != nil {
		return err
	}
	ok := false
	for _, id := range options {
		if id == candidateID {
			ok = true
		}
	}
	if !ok {
		return errInvalidChoice(candidateID)
	}
	_, err = s.DB.Exec(`UPDATE remap_refs SET resolved=1,chosen_candidate_id=?
 WHERE speaker_id=? AND utter_id=? AND from_op_id=? AND frame=? AND band=?`,
		candidateID, speaker, utter, fromOpID, frame, band)
	return err
}

type badChoice struct{ id int64 }

func (e *badChoice) Error() string {
	return fmt.Sprintf("candidate %d is not one of the offered options", e.id)
}
func errInvalidChoice(id int64) error { return &badChoice{id: id} }

// ErrInvalidChoice allows callers to detect the validation failure.
var ErrInvalidChoice = &badChoice{}
