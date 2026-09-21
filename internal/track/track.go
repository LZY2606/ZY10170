// Package track applies sparse correction operations to spectrum candidates,
// validates the corrected trajectories, detects identity swaps across runs of
// invisible frames, and replays old operations onto recomputed candidate sets.
package track

import (
	"fmt"
	"sort"

	"voicebench/internal/model"
)

// Data is the candidate/frame universe of one utterance on one candidate set.
type Data struct {
	Frames []model.FrameMeta
	// Cand[frame][band] sorted by frequency ascending
	Cand    map[int]map[int][]model.Candidate
	ByID    map[int64]model.Candidate
	Auto    map[int]map[int]model.AutoPoint // frame -> band
	Version string
}

// NewData assembles the lookup universe.
func NewData(frames []model.FrameMeta, cands []model.Candidate,
	auto []model.AutoPoint, version string) *Data {
	d := &Data{
		Frames:  frames,
		Cand:    map[int]map[int][]model.Candidate{},
		ByID:    map[int64]model.Candidate{},
		Auto:    map[int]map[int]model.AutoPoint{},
		Version: version,
	}
	for _, c := range cands {
		if c.Version != version {
			continue
		}
		if d.Cand[c.Frame] == nil {
			d.Cand[c.Frame] = map[int][]model.Candidate{}
		}
		d.Cand[c.Frame][c.Band] = append(d.Cand[c.Frame][c.Band], c)
		d.ByID[c.ID] = c
	}
	for fb := range d.Cand {
		for b := range d.Cand[fb] {
			sort.Slice(d.Cand[fb][b], func(i, j int) bool {
				return d.Cand[fb][b][i].Freq < d.Cand[fb][b][j].Freq
			})
		}
	}
	for _, a := range auto {
		if a.Version != version {
			continue
		}
		if d.Auto[a.Frame] == nil {
			d.Auto[a.Frame] = map[int]model.AutoPoint{}
		}
		d.Auto[a.Frame][a.Band] = a
	}
	return d
}

func (d *Data) frame(f int) (model.FrameMeta, bool) {
	if f < 0 || f >= len(d.Frames) {
		return model.FrameMeta{}, false
	}
	return d.Frames[f], true
}

// Validate checks one operation against the universe. It enforces strict
// per-frame increase of the item itself only against items in the same op;
// full ordering is checked after application.
func (d *Data) Validate(op model.Operation) error {
	if d.Version != op.Version {
		return fmt.Errorf("operation version %q does not match active set %q",
			op.Version, d.Version)
	}
	if len(op.Items) == 0 {
		return fmt.Errorf("operation %s has no items", op.Kind)
	}
	switch op.Kind {
	case model.KindMove:
		if len(op.Items) != 1 {
			return fmt.Errorf("move requires exactly one item, got %d", len(op.Items))
		}
	case model.KindPath:
		if err := checkPathItems(op.Items); err != nil {
			return err
		}
	case model.KindLock:
		// locks may be sparse boundary frames
	default:
		return fmt.Errorf("unknown operation kind %q", op.Kind)
	}
	for _, it := range op.Items {
		c, ok := d.ByID[it.CandidateID]
		if !ok {
			return fmt.Errorf("candidate %d not found in set %s", it.CandidateID, d.Version)
		}
		if c.Frame != it.Frame || c.Band != it.Band {
			return fmt.Errorf("candidate %d does not belong to frame %d band %d",
				it.CandidateID, it.Frame, it.Band)
		}
		fm, ok := d.frame(it.Frame)
		if !ok || !fm.InWindow || !fm.Present {
			return fmt.Errorf("frame %d is outside the window or missing", it.Frame)
		}
	}
	return nil
}

func checkPathItems(items []model.OpItem) error {
	band := items[0].Band
	prev := items[0].Frame
	for i, it := range items {
		if it.Band != band {
			return fmt.Errorf("path mixes bands F%d and F%d", band+1, it.Band+1)
		}
		if i > 0 {
			if it.Frame != prev+1 {
				return fmt.Errorf("path not contiguous: frame %d after %d", it.Frame, prev)
			}
			prev = it.Frame
		}
	}
	return nil
}

// Choice is the final effective candidate for one cell.
type Choice struct {
	Candidate model.Candidate
	Source    string
	OpID      int64
}

// Apply folds operations over the auto assignments. Later operations win per
// cell; the original auto rows are never modified.
func Apply(d *Data, ops []model.Operation) (map[int]map[int]Choice, error) {
	grid := map[int]map[int]Choice{}
	for f, bs := range d.Auto {
		for b, a := range bs {
			c := d.ByID[a.CandidateID]
			if grid[f] == nil {
				grid[f] = map[int]Choice{}
			}
			grid[f][b] = Choice{Candidate: c, Source: "auto", OpID: 0}
		}
	}
	for _, op := range ops {
		if op.Version != d.Version {
			return nil, fmt.Errorf("operation %d version %q != %q",
				op.ID, op.Version, d.Version)
		}
		if err := d.Validate(op); err != nil {
			return nil, err
		}
		source := op.Kind
		for _, it := range op.Items {
			c := d.ByID[it.CandidateID]
			if grid[it.Frame] == nil {
				grid[it.Frame] = map[int]Choice{}
			}
			grid[it.Frame][it.Band] = Choice{Candidate: c, Source: source, OpID: op.ID}
		}
	}
	return grid, nil
}

// Locked returns the set of "frame:band" cells pinned by lock operations.
func Locked(ops []model.Operation) map[[2]int]int64 {
	out := map[[2]int]int64{}
	for _, op := range ops {
		if op.Kind != model.KindLock {
			continue
		}
		for _, it := range op.Items {
			out[[2]int{it.Frame, it.Band}] = op.ID
		}
	}
	return out
}

// Final builds the exportable per-cell view, including confidence and
// provenance. Cells for missing/out-of-window frames stay empty (no
// zero-frequency interpolation).
func Final(d *Data, grid map[int]map[int]Choice) []model.Cell {
	var out []model.Cell
	for _, fm := range d.Frames {
		if !fm.InWindow {
			continue
		}
		for b := 0; b < model.Bands; b++ {
			cell := model.Cell{Frame: fm.Frame, Band: b,
				Present: fm.Present, InWindow: fm.InWindow}
			if !fm.Present {
				out = append(out, cell)
				continue
			}
			ch, ok := grid[fm.Frame][b]
			if !ok {
				out = append(out, cell)
				continue
			}
			cell.Freq = ch.Candidate.Freq
			cell.CandidateID = ch.Candidate.ID
			cell.Source = ch.Source
			cell.OpID = ch.OpID
			if a, ok := d.Auto[fm.Frame][b]; ok {
				cell.AutoCandID = a.CandidateID
				cell.Confidence = a.Confidence
			}
			out = append(out, cell)
		}
	}
	return out
}
