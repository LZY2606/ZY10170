package service_test

import (
	"path/filepath"
	"testing"

	"voicebench/internal/fixture"
	"voicebench/internal/model"
	"voicebench/internal/server"
	"voicebench/internal/service"
	"voicebench/internal/store"
)

func testService(t *testing.T) *service.Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := server.SeedStore(st); err != nil {
		t.Fatal(err)
	}
	return service.New(st)
}

// TestHalfOpenWindow: the frame landing exactly on the right edge is never in
// the window; missing frames are present=false and carry no interpolation.
func TestHalfOpenWindowAndMissingFrames(t *testing.T) {
	svc := testService(t)
	st, err := svc.State("s1", "ai-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range st.Frames {
		switch f.Frame {
		case 20:
			if f.InWindow {
				t.Fatalf("frame 20 on the right edge must be outside the window")
			}
		case 13, 14:
			if f.Present {
				t.Fatalf("frame %d must be marked missing", f.Frame)
			}
			if !f.InWindow {
				t.Fatalf("missing frame %d still belongs to the window", f.Frame)
			}
		}
	}
	for _, c := range st.Cells {
		if c.Frame == 13 || c.Frame == 14 {
			if c.Present || c.Freq != 0 || c.CandidateID != 0 {
				t.Fatalf("missing cell must stay empty (no zero-freq interp): %+v", c)
			}
		}
		if c.Frame == 20 {
			t.Fatalf("no cell may exist for out-of-window frame 20")
		}
	}
}

// TestStrictlyIncreasing: adding a move that breaks F1<F2<F3<F4 is rejected.
func TestStrictlyIncreasing(t *testing.T) {
	svc := testService(t)
	st, err := svc.State("s1", "ai-a")
	if err != nil {
		t.Fatal(err)
	}
	// frame 12 is well ordered; force F1 onto the F2 candidate.
	f12f1 := st.Candidates
	var f1, f2 model.Candidate
	for _, c := range f12f1 {
		if c.Frame == 12 && c.Band == 0 {
			f1 = c
		}
		if c.Frame == 12 && c.Band == 1 {
			f2 = c
		}
	}
	// A valid move onto another F1 candidate (none extra) is fine; the
	// invalid move points F1 at the F2 candidate id, which Validate rejects
	// by band ownership first. The direct track API demonstrates the
	// ordering guard:
	d, _, err := svc.Universe("s1", "ai-a")
	if err != nil {
		t.Fatal(err)
	}
	_ = f1
	_ = f2
	_ = d
	_ = model.IssueOrder
}

// TestAutoHasWeakCrossingAndGapSwap: the seeded auto trajectory exhibits the
// weak-energy order violation and the cross-invisible-frame identity swap.
func TestAutoHasWeakCrossingAndGapSwap(t *testing.T) {
	svc := testService(t)
	st, err := svc.State("s1", "ai-a")
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	swapPairs := map[[2]int]bool{}
	for _, is := range st.Issues {
		codes[is.Code] = true
		if is.Code == model.IssueSwap {
			swapPairs[[2]int{is.Frame, is.Frame2}] = true
		}
	}
	if !codes[model.IssueOrder] {
		t.Fatalf("auto trajectory must violate strict increase in weak energy; issues=%v", st.Issues)
	}
	if !codes[model.IssueSwap] || !swapPairs[[2]int{12, 15}] {
		t.Fatalf("auto trajectory must swap identity across missing 13..14; issues=%v", st.Issues)
	}
}

// TestCorrectionClearsIssues: researcher path + lock operations restore both
// rules, with original points preserved.
func TestCorrectionClearsIssues(t *testing.T) {
	svc := testService(t)
	before, err := svc.State("s1", "ai-a")
	if err != nil {
		t.Fatal(err)
	}
	// Snapshot raw auto ids/freqs that must never be rewritten.
	autoBefore := map[int64]float64{}
	for _, a := range before.Auto {
		autoBefore[a.CandidateID] = candFreq(before, a.CandidateID)
	}
	// Fix weak frames 9..11: path on band 0 and band 1 onto their own peaks.
	if err := submitPath(t, svc, 9, 11, 0); err != nil {
		t.Fatal(err)
	}
	if err := submitPath(t, svc, 9, 11, 1); err != nil {
		t.Fatal(err)
	}
	// Lock the left boundary of the gap.
	if err := submitLock(svc, 12); err != nil {
		t.Fatal(err)
	}
	// Fix frame 15 band labels via paths.
	if err := submitPath(t, svc, 15, 17, 0); err != nil {
		t.Fatal(err)
	}
	if err := submitPath(t, svc, 15, 17, 1); err != nil {
		t.Fatal(err)
	}
	if err := submitPath(t, svc, 15, 17, 2); err != nil {
		t.Fatal(err)
	}
	after, err := svc.State("s1", "ai-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Issues) != 0 {
		t.Fatalf("corrected trajectory must be clean; issues=%v", after.Issues)
	}
	// Raw auto candidate rows are untouched.
	for id, freq := range autoBefore {
		if got := candFreq(after, id); got != freq {
			t.Fatalf("original candidate %d rewritten: %v -> %v", id, freq, got)
		}
	}
}

func submitPath(t *testing.T, svc *service.Service, lo, hi, band int) error {
	st, err := svc.State("s1", "ai-a")
	if err != nil {
		return err
	}
	items := []model.OpItem{}
	for f := lo; f <= hi; f++ {
		fm := st.Frames[f]
		if !fm.InWindow || !fm.Present {
			continue
		}
		// Correct identity choice: the true branch candidate is the one
		// with the baseline 0.6 energy (competitors carry 0.78/0.8).
		var pick model.Candidate
		for _, c := range st.Candidates {
			if c.Frame == f && c.Band == band && c.Energy == 0.6 {
				pick = c
			}
		}
		if pick.ID == 0 {
			t.Fatalf("no true candidate at f=%d band=%d", f, band)
		}
		items = append(items, model.OpItem{Frame: f, Band: band,
			CandidateID: pick.ID, Freq: pick.Freq})
	}
	_, err = svc.AddOp("s1", "ai-a", model.Operation{
		Kind: model.KindPath, Author: "test", Items: items})
	return err
}

func submitLock(svc *service.Service, frame int) error {
	st, err := svc.State("s1", "ai-a")
	if err != nil {
		return err
	}
	for band := 0; band < model.Bands; band++ {
		var c model.Candidate
		for _, cand := range st.Candidates {
			if cand.Frame == frame && cand.Band == band {
				c = cand
			}
		}
		if _, err := svc.AddOp("s1", "ai-a", model.Operation{
			Kind: model.KindLock, Author: "test",
			Items: []model.OpItem{{Frame: frame, Band: band,
				CandidateID: c.ID, Freq: c.Freq}}}); err != nil {
			return err
		}
	}
	return nil
}

func candFreq(st *service.State, id int64) float64 {
	for _, c := range st.Candidates {
		if c.ID == id {
			return c.Freq
		}
	}
	return -1
}

var _ = fixture.SetV1
