// Package service orchestrates storage and trajectory correction rules.
package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"voicebench/internal/fixture"
	"voicebench/internal/model"
	"voicebench/internal/store"
	"voicebench/internal/track"
)

// Service is the application facade.
type Service struct{ St *store.Store }

// New constructs a service.
func New(st *store.Store) *Service { return &Service{St: st} }

// Universe loads one utterance's active correction universe.
func (s *Service) Universe(speaker, utter string) (*track.Data, []model.Operation, error) {
	ver, err := s.St.ActiveSet(speaker, utter)
	if err != nil {
		return nil, nil, err
	}
	frames, err := s.St.Frames(speaker, utter)
	if err != nil {
		return nil, nil, err
	}
	cands, err := s.St.Candidates(speaker, utter, ver)
	if err != nil {
		return nil, nil, err
	}
	autos, err := s.St.AutoPoints(speaker, utter, ver)
	if err != nil {
		return nil, nil, err
	}
	ops, err := s.St.Operations(speaker, utter)
	if err != nil {
		return nil, nil, err
	}
	// Only operations recorded against the active set take effect; stale
	// operations from an older set stay in history but do not apply.
	var active []model.Operation
	for _, op := range ops {
		if op.Version == ver {
			active = append(active, op)
		}
	}
	return track.NewData(frames, cands, autos, ver), ops, nil
}

// AddOp validates and stores one sparse revision.
func (s *Service) AddOp(speaker, utter string, op model.Operation) (int64, error) {
	u, err := s.universeModel(speaker, utter)
	if err != nil {
		return 0, err
	}
	op.SpeakerID, op.UtterID = speaker, utter
	op.Version = u.Version
	d, _, err := s.Universe(speaker, utter)
	if err != nil {
		return 0, err
	}
	if err := d.Validate(op); err != nil {
		return 0, &ValidationError{Err: err}
	}
	// Apply tentatively with existing ops so the per-frame strict increase
	// rule holds after every single revision.
	existing, err := s.St.Operations(speaker, utter)
	if err != nil {
		return 0, err
	}
	var active []model.Operation
	for _, o := range existing {
		if o.Version == op.Version {
			active = append(active, o)
		}
	}
	active = append(active, op)
	grid, err := track.Apply(d, active)
	if err != nil {
		return 0, err
	}
	// A single revision may temporarily leave the frame unresolved (the
	// researcher is fixing a pre-existing swap step by step); the order
	// violation remains visible until the companion edit lands. We only
	// reject operations that *create* a new violation on a previously clean
	// frame, so corrections to already-broken fixture frames are allowed.
	issues := track.Issues(d, grid, track.Locked(active))
	prevGrid, _ := track.Apply(d, active[:len(active)-1])
	prevIssues := map[int]bool{}
	for _, is := range track.Issues(d, prevGrid, track.Locked(active[:len(active)-1])) {
		if is.Code == model.IssueOrder {
			prevIssues[is.Frame] = true
		}
	}
	for _, is := range issues {
		if is.Code == model.IssueOrder && !prevIssues[is.Frame] {
			return 0, &ValidationError{Err: fmt.Errorf(
				"帧 %d 内频率必须严格递增 F1<F2<F3<F4", is.Frame)}
		}
	}
	id, err := s.St.AddOperation(op)
	if err != nil {
		return 0, err
	}
	detail, _ := json.Marshal(op.Items)
	_ = s.St.Log(op.Author, "op:"+op.Kind,
		fmt.Sprintf("%s/%s #%d %s", speaker, utter, id, string(detail)))
	return id, nil
}

// Undo drops the newest operation of an utterance.
func (s *Service) Undo(speaker, utter string) (bool, error) {
	ok, err := s.St.UndoOperation(speaker, utter)
	if err != nil {
		return false, err
	}
	if ok {
		_ = s.St.Log("researcher", "undo", speaker+"/"+utter)
	}
	return ok, nil
}

// State is the full page payload for one utterance.
type State struct {
	Speaker    model.Speaker         `json:"speaker"`
	Utterance  model.Utterance       `json:"utterance"`
	Version    string                `json:"active_set"`
	Frames     []model.FrameMeta     `json:"frames"`
	Candidates []model.Candidate     `json:"candidates"`
	Auto       []model.AutoPoint     `json:"auto_assignments"`
	Vowels     []model.VowelInterval `json:"vowels"`
	Spectrum   []model.SpectrumCell  `json:"spectrum"`
	Operations []model.Operation     `json:"operations"`
	Cells      []model.Cell          `json:"final_cells"`
	Issues     []model.Issue         `json:"issues"`
	Conflicts  []model.Ref           `json:"conflicts"`
	Unresolved []model.Ref           `json:"unresolved"`
	AllRefs    []model.Ref           `json:"remap_refs"`
	RunLog     []model.RunLog        `json:"run_log"`
}

// State builds the synchronised view shown by the page.
func (s *Service) State(speaker, utter string) (*State, error) {
	_, opsAll, err := s.Universe(speaker, utter)
	if err != nil {
		return nil, err
	}
	d, _, err := s.Universe(speaker, utter)
	if err != nil {
		return nil, err
	}
	var active []model.Operation
	for _, o := range opsAll {
		if o.Version == d.Version {
			active = append(active, o)
		}
	}
	grid, err := track.Apply(d, active)
	if err != nil {
		return nil, err
	}
	issues := track.Issues(d, grid, track.Locked(active))
	st := &State{
		Version:    d.Version,
		Frames:     d.Frames,
		Candidates: flattenCandidates(d),
		Auto:       flattenAuto(d),
		Operations: opsAll,
		Cells:      track.Final(d, grid),
		Issues:     issues,
	}
	sp, err := s.speaker(speaker)
	if err != nil {
		return nil, err
	}
	st.Speaker = sp
	us, err := s.St.ListUtterances()
	if err != nil {
		return nil, err
	}
	for _, u := range us {
		if u.SpeakerID == speaker && u.ID == utter {
			st.Utterance = u
		}
	}
	st.Vowels, _ = s.St.Vowels(speaker, utter)
	st.Spectrum, _ = s.St.Spectrum(speaker, utter)
	st.RunLog, _ = s.St.RunLogTail(30)
	if refs, err := s.St.RemapRefs(speaker, utter); err == nil {
		st.AllRefs = refs
		for _, r := range refs {
			switch r.Status {
			case model.StatusConflict:
				st.Conflicts = append(st.Conflicts, r)
			case model.StatusUnresolved:
				st.Unresolved = append(st.Unresolved, r)
			}
		}
	}
	return st, nil
}

func flattenCandidates(d *track.Data) []model.Candidate {
	var out []model.Candidate
	for _, c := range d.ByID {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Frame != out[j].Frame {
			return out[i].Frame < out[j].Frame
		}
		if out[i].Band != out[j].Band {
			return out[i].Band < out[j].Band
		}
		return out[i].Freq < out[j].Freq
	})
	return out
}

func flattenAuto(d *track.Data) []model.AutoPoint {
	var out []model.AutoPoint
	for _, row := range d.Auto {
		for _, a := range row {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Frame != out[j].Frame {
			return out[i].Frame < out[j].Frame
		}
		return out[i].Band < out[j].Band
	})
	return out
}

func (s *Service) speaker(id string) (model.Speaker, error) {
	sps, err := s.St.ListSpeakers()
	if err != nil {
		return model.Speaker{}, err
	}
	for _, sp := range sps {
		if sp.ID == id {
			return sp, nil
		}
	}
	return model.Speaker{}, fmt.Errorf("speaker %s not found", id)
}

type universeModel struct{ Version string }

func (s *Service) universeModel(speaker, utter string) (*universeModel, error) {
	ver, err := s.St.ActiveSet(speaker, utter)
	if err != nil {
		return nil, err
	}
	return &universeModel{Version: ver}, nil
}

// RerunResult is what remapping the old ops onto the recomputed set yields.
type RerunResult struct {
	ActiveSet       string         `json:"active_set"`
	FromSet         string         `json:"from_set"`
	ToSet           string         `json:"to_set"`
	ToleranceHz     float64        `json:"tolerance_hz"`
	Mapping         *model.Mapping `json:"mapping"`
	RestoredOps     int            `json:"restored_ops"`
	ConflictCount   int            `json:"conflict_count"`
	UnresolvedCount int            `json:"unresolved_count"`
	At              string         `json:"at"`
}

// Rerun switches to the recomputed candidate set and replays prior
// operations. Unique mappings are restored automatically; conflicts and
// unresolved items are listed, never guessed by distance.
func (s *Service) Rerun(speaker, utter string) (*RerunResult, error) {
	fromVer, err := s.St.ActiveSet(speaker, utter)
	if err != nil {
		return nil, err
	}
	if fromVer == fixture.SetV2 {
		return nil, &ValidationError{Err: fmt.Errorf(
			"候选集合已经是 %s，无需重跑", fixture.SetV2)}
	}
	frames, err := s.St.Frames(speaker, utter)
	if err != nil {
		return nil, err
	}
	oldCands, err := s.St.Candidates(speaker, utter, fixture.SetV1)
	if err != nil {
		return nil, err
	}
	newCands, err := s.St.Candidates(speaker, utter, fixture.SetV2)
	if err != nil {
		return nil, err
	}
	oldAuto, err := s.St.AutoPoints(speaker, utter, fixture.SetV1)
	if err != nil {
		return nil, err
	}
	newAuto, err := s.St.AutoPoints(speaker, utter, fixture.SetV2)
	if err != nil {
		return nil, err
	}
	ops, err := s.St.Operations(speaker, utter)
	if err != nil {
		return nil, err
	}
	var oldOps []model.Operation
	for _, op := range ops {
		if op.Version == fixture.SetV1 {
			oldOps = append(oldOps, op)
		}
	}
	from := track.NewData(frames, oldCands, oldAuto, fixture.SetV1)
	to := track.NewData(frames, newCands, newAuto, fixture.SetV2)
	mapping, err := track.Remap(from, to, oldOps, fixture.ToleranceHz)
	if err != nil {
		return nil, err
	}
	mapping.SpeakerID = speaker
	mapping.UtterID = utter
	if err := s.St.SetActiveSet(speaker, utter, fixture.SetV2); err != nil {
		return nil, err
	}
	if err := s.St.EnsureConflictSchema(); err != nil {
		return nil, err
	}
	if err := s.St.SaveRemapRefs(speaker, utter, fixture.SetV1,
		fixture.SetV2, mapping.Refs); err != nil {
		return nil, err
	}
	// Store uniquely mapped operations as v2 operations. They remain sparse
	// edits; the conflict list is delivered to the UI and export, not forced.
	for _, mop := range mapping.AutoOps {
		if _, err := s.St.AddOperation(mop); err != nil {
			return nil, err
		}
	}
	res := &RerunResult{
		ActiveSet: fixture.SetV2, FromSet: fixture.SetV1, ToSet: fixture.SetV2,
		ToleranceHz: fixture.ToleranceHz, Mapping: mapping,
		RestoredOps:     len(mapping.AutoOps),
		ConflictCount:   mapping.ConflictCount,
		UnresolvedCount: mapping.UnresolvedCount,
		At:              time.Now().UTC().Format(time.RFC3339),
	}
	raw, _ := json.Marshal(map[string]any{
		"restored": res.RestoredOps, "conflicts": res.ConflictCount,
		"unresolved": res.UnresolvedCount})
	_ = s.St.Log("system", "rerun", speaker+"/"+utter+" "+string(raw))
	return res, nil
}

// ValidationError marks a user-input level failure (HTTP 422).
type ValidationError struct{ Err error }

func (e *ValidationError) Error() string { return e.Err.Error() }

// ResolveConflict records the researcher's explicit pick for an ambiguous
// remap item and applies it as a sparse move on the new candidate set.
func (s *Service) ResolveConflict(speaker, utter string,
	fromOpID int64, frame, band int, candidateID int64) error {
	if err := s.St.ResolveConflict(speaker, utter, fromOpID, frame, band,
		candidateID); err != nil {
		return &ValidationError{Err: err}
	}
	d, _, err := s.Universe(speaker, utter)
	if err != nil {
		return err
	}
	c, ok := d.ByID[candidateID]
	if !ok || c.Frame != frame || c.Band != band {
		return &ValidationError{Err: fmt.Errorf("候选 %d 不属于帧 %d F%d",
			candidateID, frame, band+1)}
	}
	op := model.Operation{
		SpeakerID: speaker, UtterID: utter, Version: d.Version,
		Kind:   model.KindMove,
		Note:   fmt.Sprintf("手动消解冲突（源操作 #%d）", fromOpID),
		Author: "researcher",
		Items: []model.OpItem{{Frame: frame, Band: band,
			CandidateID: candidateID, Freq: c.Freq}},
	}
	_, err = s.AddOp(speaker, utter, op)
	return err
}
