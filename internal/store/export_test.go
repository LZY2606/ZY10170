package store_test

import (
	"path/filepath"
	"testing"

	"voicebench/internal/fixture"
	"voicebench/internal/model"
	"voicebench/internal/server"
	"voicebench/internal/store"
)

func openSeed(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := server.SeedStore(st); err != nil {
		t.Fatal(err)
	}
	return st
}

// TestExportImportRoundTrip: original points are never rewritten and the
// operations replay independently after wiping the database.
func TestExportImportRoundTrip(t *testing.T) {
	st1 := openSeed(t)
	cands, err := st1.Candidates("s1", "ai-a", fixture.SetV1)
	if err != nil {
		t.Fatal(err)
	}
	var lockC model.Candidate
	for _, c := range cands {
		if c.Frame == 5 && c.Band == 0 {
			lockC = c
		}
	}
	id1, err := st1.AddOperation(model.Operation{
		SpeakerID: "s1", UtterID: "ai-a", Version: fixture.SetV1,
		Kind: model.KindLock, Author: "researcher",
		Items: []model.OpItem{{Frame: 5, Band: 0,
			CandidateID: lockC.ID, Freq: lockC.Freq}},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st1.Export()
	if err != nil {
		t.Fatal(err)
	}
	if b.Schema != store.SchemaVersion {
		t.Fatalf("schema tag missing: %s", b.Schema)
	}
	rawC1 := map[int64]model.Candidate{}
	for _, c := range b.Candidates {
		rawC1[c.ID] = c
	}
	autoC1 := map[int64]model.AutoPoint{}
	for _, a := range b.Auto {
		autoC1[a.ID] = a
	}

	// Wipe and re-import the bundle into a fresh database.
	st2, err := store.Open(filepath.Join(t.TempDir(), "re.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	if err := st2.ReplaceBundle(b, "roundtrip"); err != nil {
		t.Fatal(err)
	}
	b2, err := st2.Export()
	if err != nil {
		t.Fatal(err)
	}
	for id, c := range rawC1 {
		var got *model.Candidate
		for i := range b2.Candidates {
			if b2.Candidates[i].ID == id {
				got = &b2.Candidates[i]
			}
		}
		if got == nil {
			t.Fatalf("candidate %d lost on reimport", id)
		}
		if got.Freq != c.Freq || got.Frame != c.Frame || got.Band != c.Band ||
			got.Version != c.Version {
			t.Fatalf("original candidate %d rewritten: %+v vs %+v", id, c, got)
		}
	}
	for id, a := range autoC1 {
		found := false
		for _, a2 := range b2.Auto {
			if a2.ID == id {
				found = true
				if a2.CandidateID != a.CandidateID || a2.Frame != a.Frame ||
					a2.Band != a.Band || a2.Version != a.Version {
					t.Fatalf("auto point %d rewritten", id)
				}
			}
		}
		if !found {
			t.Fatalf("auto point %d lost", id)
		}
	}
	var opFound bool
	for _, op := range b2.Operations {
		if op.ID == id1 && op.Kind == model.KindLock {
			opFound = true
		}
	}
	if !opFound {
		t.Fatal("operation sequence must survive reimport with its id")
	}
}

// TestClearAndReseed verifies a wiped database can re-import the fixed fixture.
func TestClearAndReseed(t *testing.T) {
	st := openSeed(t)
	b, err := st.Export()
	if err != nil {
		t.Fatal(err)
	}
	n0 := len(b.Candidates)
	if err := st.ReplaceBundle(&model.Bundle{Schema: store.SchemaVersion,
		Speakers: []model.Speaker{{ID: "x", Name: "x"}}}, "wipe"); err != nil {
		t.Fatal(err)
	}
	n, err := st.CountSpeakers()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("wipe failed: %d speakers", n)
	}
	if err := server.SeedStore(st); err != nil {
		t.Fatal(err)
	}
	b2, _ := st.Export()
	if len(b2.Candidates) != n0 {
		t.Fatalf("reseed candidate count %d != %d", len(b2.Candidates), n0)
	}
}
