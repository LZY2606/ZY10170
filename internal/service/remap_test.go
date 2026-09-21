package service_test

import (
	"testing"

	"voicebench/internal/fixture"
	"voicebench/internal/model"
	"voicebench/internal/service"
)

// TestRerunUniqueAndConflict replays the same revision set onto the
// recomputed candidate set: unique mappings restore automatically, the
// designed ambiguous item keeps BOTH options and is not chosen by distance.
func TestRerunUniqueAndConflict(t *testing.T) {
	svc := testService(t)

	// Revision 1: path on F2 over frames 9..11 (includes the ambiguous f10),
	// plus an unrelated F1 move at frame 5.
	if err := submitPath(t, svc, 9, 11, 1); err != nil {
		t.Fatal(err)
	}
	st0, err := svc.State("s1", "ai-a")
	if err != nil {
		t.Fatal(err)
	}
	var f5f0 model.Candidate
	for _, c := range st0.Candidates {
		if c.Frame == 5 && c.Band == 0 {
			f5f0 = c
		}
	}
	if _, err := svc.AddOp("s1", "ai-a", model.Operation{
		Kind: model.KindMove, Author: "test",
		Items: []model.OpItem{{Frame: 5, Band: 0,
			CandidateID: f5f0.ID, Freq: f5f0.Freq}}}); err != nil {
		t.Fatal(err)
	}
	// Locks at the gap boundary restore uniquely.
	if err := submitLock(svc, 12); err != nil {
		t.Fatal(err)
	}

	res, err := svc.Rerun("s1", "ai-a")
	if err != nil {
		t.Fatal(err)
	}
	if res.ActiveSet != fixture.SetV2 {
		t.Fatalf("active set = %q", res.ActiveSet)
	}

	// The f10 F2 remap must be one conflict with exactly two options.
	var f10conf *model.Ref
	for i := range res.Mapping.Refs {
		r := res.Mapping.Refs[i]
		if r.Frame == 10 && r.Band == 1 {
			f10conf = &res.Mapping.Refs[i]
		}
	}
	if f10conf == nil || f10conf.Status != model.StatusConflict {
		t.Fatalf("f10/F2 must be conflict, got %+v", f10conf)
	}
	if len(f10conf.Options) != 2 {
		t.Fatalf("ambiguous mapping must preserve TWO candidates, got %d: %v",
			len(f10conf.Options), f10conf.Options)
	}
	// Both options must genuinely lie within tolerance of the old peak.
	st1, err := svc.State("s1", "ai-a")
	if err != nil {
		t.Fatal(err)
	}
	oldF := 0.0
	for _, c := range st0.Candidates {
		if c.Frame == 10 && c.Band == 1 {
			// v1 path choice was the highest candidate = 900
			if c.ID != 0 {
				_ = c
			}
		}
	}
	_ = oldF
	for _, id := range f10conf.Options {
		c := findCand(st1, id)
		if c.Freq < 900-fixture.ToleranceHz || c.Freq > 900+fixture.ToleranceHz {
			t.Fatalf("option %v outside tolerance of old 900Hz", c.Freq)
		}
	}

	// Unique items restored automatically: f5 move, f9/f11 path, locks.
	if res.RestoredOps < 3 {
		t.Fatalf("expected auto restoration of unique ops, got %d",
			res.RestoredOps)
	}
	// The conflict path operation must NOT have been auto-created as a
	// complete v2 path over 9..11 band1.
	for _, op := range st1.Operations {
		if op.Version == fixture.SetV2 && op.Kind == model.KindPath {
			for _, it := range op.Items {
				if it.Frame == 10 && it.Band == 1 {
					t.Fatalf("ambiguous f10 must not be silently chosen")
				}
			}
		}
	}
	// Conflict is listed by state.
	if len(st1.Conflicts) != 1 || st1.Conflicts[0].Frame != 10 {
		t.Fatalf("conflict list must hold f10 only, got %+v", st1.Conflicts)
	}

	// Researcher explicitly picks one option; afterwards it is a normal move.
	if err := svc.ResolveConflict("s1", "ai-a", f10conf.FromOpID, 10, 1,
		f10conf.Options[0]); err != nil {
		t.Fatal(err)
	}
	st2, err := svc.State("s1", "ai-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(st2.Conflicts) != 0 {
		t.Fatalf("conflict must be resolved, got %+v", st2.Conflicts)
	}
	cell10f2 := cellOf(st2, 10, 1)
	if cell10f2.CandidateID != f10conf.Options[0] {
		t.Fatalf("resolved choice not applied: %+v", cell10f2)
	}
}

// TestRerunUnresolved: an operation on a candidate that disappears entirely in
// the recomputed set becomes unresolved and is never force-picked.
func TestRerunUnresolved(t *testing.T) {
	svc := testService(t)
	st, err := svc.State("s1", "ai-a")
	if err != nil {
		t.Fatal(err)
	}
	// v1 frame 9 F1 slot contains a weak competitor (the F2 branch leak),
	// which v2 drops. Move F1 onto that disappearing peak.
	var dis model.Candidate
	for _, c := range st.Candidates {
		if c.Frame == 9 && c.Band == 0 && c.Freq > 900 {
			dis = c // leaked F2 peak
		}
	}
	if dis.ID == 0 {
		t.Fatal("fixture must include a disappearing competitor at f9/F1")
	}
	if _, err := svc.AddOp("s1", "ai-a", model.Operation{
		Kind: model.KindMove, Author: "test",
		Items: []model.OpItem{{Frame: 9, Band: 0,
			CandidateID: dis.ID, Freq: dis.Freq}}}); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Rerun("s1", "ai-a")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range res.Mapping.Refs {
		if r.Frame == 9 && r.Band == 0 {
			found = true
			if r.Status != model.StatusUnresolved {
				t.Fatalf("dropped peak must be unresolved, got %s", r.Status)
			}
		}
	}
	if !found {
		t.Fatal("unresolved ref missing")
	}
}

func findCand(st *service.State, id int64) model.Candidate {
	for _, c := range st.Candidates {
		if c.ID == id {
			return c
		}
	}
	return model.Candidate{}
}

func cellOf(st *service.State, f, b int) model.Cell {
	for _, c := range st.Cells {
		if c.Frame == f && c.Band == b {
			return c
		}
	}
	return model.Cell{}
}
