// Package model defines the domain types of the voiceprint formant correction
// bench. All frequency values are in Hz; frames are fixed-rate indices.
package model

import "time"

// Bands is the number of tracked formants F1..F4.
const Bands = 4

// BandNames are the display names of the four tracks.
var BandNames = []string{"F1", "F2", "F3", "F4"}

// KindMove retargets one frame to another candidate.
const KindMove = "move"

// KindPath retargets one band over a contiguous frame range.
const KindPath = "path"

// KindLock pins a boundary frame so its assignment must stay identifiable when
// a candidate set is recomputed.
const KindLock = "lock"

// Candidate is one peak proposed by the spectrum analyser for one frame and
// band. A frame may carry more than one candidate per band (competing peaks).
type Candidate struct {
	ID        int64   `json:"id"`
	SpeakerID string  `json:"speaker_id"`
	UtterID   string  `json:"utter_id"`
	Version   string  `json:"cand_version"`
	Frame     int     `json:"frame"`
	Band      int     `json:"band"` // 0..3 = F1..F4
	Freq      float64 `json:"freq"`
	Energy    float64 `json:"energy"`
}

// FrameMeta describes one fixed-rate analysis frame. InWindow is false for a
// frame landing exactly on the right edge of the analysis window; such a frame
// never belongs to the window (half-open interval).
type FrameMeta struct {
	SpeakerID string  `json:"speaker_id"`
	UtterID   string  `json:"utter_id"`
	Frame     int     `json:"frame"`
	T         float64 `json:"t"`
	Present   bool    `json:"present"`
	InWindow  bool    `json:"in_window"`
}

// AutoPoint is the untouched algorithmic assignment. Final trajectories always
// derive from original candidates + operations; raw rows are never rewritten.
type AutoPoint struct {
	ID          int64   `json:"id"`
	SpeakerID   string  `json:"speaker_id"`
	UtterID     string  `json:"utter_id"`
	Version     string  `json:"cand_version"`
	Frame       int     `json:"frame"`
	Band        int     `json:"band"`
	CandidateID int64   `json:"candidate_id"`
	Confidence  float64 `json:"confidence"`
}

// VowelInterval marks a pronunciation version segment. End is exclusive.
type VowelInterval struct {
	ID         int64  `json:"id"`
	SpeakerID  string `json:"speaker_id"`
	UtterID    string `json:"utter_id"`
	StartFrame int    `json:"start_frame"`
	EndFrame   int    `json:"end_frame"` // exclusive
	Label      string `json:"label"`
}

// Speaker is an enrolled talker.
type Speaker struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Utterance is one pronunciation version of fixed content.
type Utterance struct {
	ID         string `json:"id"`
	SpeakerID  string `json:"speaker_id"`
	PronVer    string `json:"pronunciation_version"`
	WindowFrom int    `json:"window_from_frame"`
	WindowTo   int    `json:"window_to_frame"` // exclusive; frame == To is outside
	FPS        int    `json:"fps"`
	Frames     int    `json:"frame_count"` // raw frames generated, incl. right edge
}

// Key returns the utterance identity used everywhere.
func (u Utterance) Key() string { return u.SpeakerID + "/" + u.ID }

// OpItem is one (frame, band, candidate) target inside an operation.
type OpItem struct {
	Frame       int     `json:"frame"`
	Band        int     `json:"band"`
	CandidateID int64   `json:"candidate_id"`
	Freq        float64 `json:"freq"`
}

// Operation is one sparse revision step.
//
//	move: exactly one Item
//	path: Items sorted by Frame, same Band, contiguous present in-window frames
//	lock: Items are the locked boundary frames; CandidateID/Freq hold the pin
type Operation struct {
	ID        int64    `json:"id"`
	SpeakerID string   `json:"speaker_id"`
	UtterID   string   `json:"utter_id"`
	Version   string   `json:"cand_version"` // set the operation applies on
	Kind      string   `json:"kind"`
	Note      string   `json:"note"`
	Author    string   `json:"author"`
	CreatedAt string   `json:"created_at"`
	Items     []OpItem `json:"items"`
}

// Ref describes how an original operation mapped onto a recomputed set.
type Ref struct {
	FromOpID   int64   `json:"from_op_id"`
	Frame      int     `json:"frame"`
	Band       int     `json:"band"`
	OldCandID  int64   `json:"old_candidate_id"`
	Status     string  `json:"status"` // unique | conflict | unresolved
	UniqueCand int64   `json:"unique_candidate_id,omitempty"`
	Options    []int64 `json:"options,omitempty"`
}

// StatusUnique: exactly one recomputed candidate in tolerance.
const StatusUnique = "unique"

// StatusConflict: more than one recomputed candidate in tolerance; the system
// never picks one by distance.
const StatusConflict = "conflict"

// StatusUnresolved: no recomputed candidate in tolerance.
const StatusUnresolved = "unresolved"

// Mapping is the result of replaying an operation sequence onto a new set.
type Mapping struct {
	SpeakerID       string      `json:"speaker_id"`
	UtterID         string      `json:"utter_id"`
	FromVersion     string      `json:"from_version"`
	ToVersion       string      `json:"to_version"`
	ToleranceHz     float64     `json:"tolerance_hz"`
	Refs            []Ref       `json:"refs"`
	AutoOps         []Operation `json:"auto_ops"` // uniquely mapped, auto restored
	ConflictCount   int         `json:"conflict_count"`
	UnresolvedCount int         `json:"unresolved_count"`
	UniqueCount     int         `json:"unique_count"`
}

// Cell is the final frequency for one frame/band, plus provenance.
type Cell struct {
	Frame       int     `json:"frame"`
	Band        int     `json:"band"`
	Freq        float64 `json:"freq"`
	Present     bool    `json:"present"`
	InWindow    bool    `json:"in_window"`
	CandidateID int64   `json:"candidate_id"`
	AutoCandID  int64   `json:"auto_candidate_id"`
	Source      string  `json:"source"` // auto | move | path | lock
	OpID        int64   `json:"op_id"`
	Confidence  float64 `json:"confidence"`
}

// Issue flags a trajectory problem on the current correction.
type Issue struct {
	Code    string `json:"code"`
	Frame   int    `json:"frame"`
	Band    int    `json:"band,omitempty"`
	Frame2  int    `json:"frame2,omitempty"`
	Message string `json:"message"`
}

// IssueOrder: frequencies not strictly increasing within one visible frame.
const IssueOrder = "not_strictly_increasing"

// IssueSwap: band identities are exchanged across a run of invisible frames.
const IssueSwap = "identity_swap_across_invisible"

// SpectrumCell is one spectrogram bin (energy in [0,1]).
type SpectrumCell struct {
	SpeakerID string  `json:"speaker_id"`
	UtterID   string  `json:"utter_id"`
	Frame     int     `json:"frame"`
	Bin       int     `json:"bin"`
	FreqLo    float64 `json:"freq_lo"`
	FreqHi    float64 `json:"freq_hi"`
	Energy    float64 `json:"energy"`
}

// IssueInContext scopes an issue to one utterance.
type IssueInContext struct {
	SpeakerID string `json:"speaker_id"`
	UtterID   string `json:"utter_id"`
	Issue
}

// CellInContext scopes a final cell to one utterance.
type CellInContext struct {
	SpeakerID string `json:"speaker_id"`
	UtterID   string `json:"utter_id"`
	Cell
}

// RunLog records one user-visible action for the exportable run record.
type RunLog struct {
	ID     int64     `json:"id"`
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Action string    `json:"action"`
	Detail string    `json:"detail"`
}

// Bundle is the full self-contained export/import payload. Original points are
// embedded verbatim; operations are replayed independently by the importer.
type Bundle struct {
	Schema     string            `json:"schema"`
	ExportedAt string            `json:"exported_at"`
	Speakers   []Speaker         `json:"speakers"`
	Utterances []Utterance       `json:"utterances"`
	Frames     []FrameMeta       `json:"frames"`
	Candidates []Candidate       `json:"candidates"`
	Auto       []AutoPoint       `json:"auto_assignments"`
	Vowels     []VowelInterval   `json:"vowel_intervals"`
	Spectrum   []SpectrumCell    `json:"spectrum"`
	Issues     []IssueInContext  `json:"issues,omitempty"`
	FinalCells []CellInContext   `json:"final_cells,omitempty"`
	Operations []Operation       `json:"operations"`
	ActiveSet  map[string]string `json:"active_candidate_set"`
	RunLog     []RunLog          `json:"run_log"`
}
