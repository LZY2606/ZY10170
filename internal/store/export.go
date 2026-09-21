package store

import (
	"time"

	"voicebench/internal/model"
)

// SchemaVersion is the export format tag.
const SchemaVersion = "voicebench/v1"

// Export reads a self-contained bundle: raw candidates, untouched auto
// assignments, sparse operations, current active set and the run record.
func (s *Store) Export() (*model.Bundle, error) {
	b := &model.Bundle{
		Schema:     SchemaVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		ActiveSet:  map[string]string{},
	}
	var err error
	if b.Speakers, err = s.ListSpeakers(); err != nil {
		return nil, err
	}
	if b.Utterances, err = s.ListUtterances(); err != nil {
		return nil, err
	}
	active := map[string]string{}
	rows, err := s.DB.Query(`SELECT speaker_id,id,active_set FROM utterances`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var sp, u, v string
		if err := rows.Scan(&sp, &u, &v); err != nil {
			rows.Close()
			return nil, err
		}
		active[sp+"/"+u] = v
	}
	rows.Close()
	b.ActiveSet = active

	for _, u := range b.Utterances {
		fr, err := s.Frames(u.SpeakerID, u.ID)
		if err != nil {
			return nil, err
		}
		b.Frames = append(b.Frames, fr...)
		vs, err := s.Vowels(u.SpeakerID, u.ID)
		if err != nil {
			return nil, err
		}
		b.Vowels = append(b.Vowels, vs...)
		sp, err := s.Spectrum(u.SpeakerID, u.ID)
		if err != nil {
			return nil, err
		}
		b.Spectrum = append(b.Spectrum, sp...)
		ops, err := s.Operations(u.SpeakerID, u.ID)
		if err != nil {
			return nil, err
		}
		b.Operations = append(b.Operations, ops...)
		for _, ver := range []string{"v1", "v2"} {
			cs, err := s.Candidates(u.SpeakerID, u.ID, ver)
			if err != nil {
				return nil, err
			}
			b.Candidates = append(b.Candidates, cs...)
			as, err := s.AutoPoints(u.SpeakerID, u.ID, ver)
			if err != nil {
				return nil, err
			}
			b.Auto = append(b.Auto, as...)
		}
	}
	b.RunLog, err = s.RunLogTail(0)
	return b, err
}
