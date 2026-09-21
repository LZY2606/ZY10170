package track_test

import (
	"testing"

	"voicebench/internal/fixture"
	"voicebench/internal/model"
	"voicebench/internal/track"
)

func worlds(t *testing.T) (*track.Data, *track.Data) {
	t.Helper()
	d := fixture.Build()
	frames := func(sp, u string) []model.FrameMeta {
		var out []model.FrameMeta
		for _, f := range d.Frames {
			if f.SpeakerID == sp && f.UtterID == u {
				out = append(out, f)
			}
		}
		return out
	}
	fr := frames("s1", "ai-a")
	filter := func(ver string, cands bool) ([]model.Candidate, []model.AutoPoint) {
		var cs []model.Candidate
		var as []model.AutoPoint
		for _, c := range d.Candidates {
			if c.SpeakerID == "s1" && c.UtterID == "ai-a" && c.Version == ver {
				cs = append(cs, c)
			}
		}
		for _, a := range d.Auto {
			if a.SpeakerID == "s1" && a.UtterID == "ai-a" && a.Version == ver {
				as = append(as, a)
			}
		}
		return cs, as
	}
	c1, a1 := filter(fixture.SetV1, true)
	c2, a2 := filter(fixture.SetV2, true)
	return track.NewData(fr, c1, a1, fixture.SetV1),
		track.NewData(fr, c2, a2, fixture.SetV2)
}

func TestRemapDoesNotChooseByDistance(t *testing.T) {
	v1, v2 := worlds(t)
	// Path item at f10 F2 -> 900Hz has two v2 candidates (908,910).
	op := model.Operation{
		ID: 7, SpeakerID: "s1", UtterID: "ai-a", Version: fixture.SetV1,
		Kind: model.KindPath,
	}
	var old900 int64
	for _, c := range v1.ByID {
		if c.Frame == 10 && c.Band == 1 && c.Freq == 900 {
			old900 = c.ID
		}
	}
	if old900 == 0 {
		t.Fatal("fixture must contain 900Hz F2 candidate at f10")
	}
	op.Items = []model.OpItem{
		{Frame: 10, Band: 1, CandidateID: old900, Freq: 900},
	}
	m, err := track.Remap(v1, v2, []model.Operation{op}, fixture.ToleranceHz)
	if err != nil {
		t.Fatal(err)
	}
	var conf *model.Ref
	for i := range m.Refs {
		if m.Refs[i].Frame == 10 && m.Refs[i].Band == 1 {
			conf = &m.Refs[i]
		}
	}
	if conf == nil || conf.Status != model.StatusConflict {
		t.Fatalf("must be conflict: %+v", conf)
	}
	if len(conf.Options) != 2 {
		t.Fatalf("both candidates must be retained, got %v", conf.Options)
	}
	if len(m.AutoOps) != 0 {
		t.Fatal("ambiguous op must not be auto-restored")
	}
}

func TestRemapUniqueRestores(t *testing.T) {
	v1, v2 := worlds(t)
	var id int64
	for _, c := range v1.ByID {
		if c.Frame == 5 && c.Band == 0 {
			id = c.ID
		}
	}
	op := model.Operation{ID: 3, SpeakerID: "s1", UtterID: "ai-a",
		Version: fixture.SetV1, Kind: model.KindMove,
		Items: []model.OpItem{{Frame: 5, Band: 0, CandidateID: id}}}
	m, err := track.Remap(v1, v2, []model.Operation{op}, fixture.ToleranceHz)
	if err != nil {
		t.Fatal(err)
	}
	if m.UniqueCount != 1 || len(m.AutoOps) != 1 {
		t.Fatalf("unique mapping must restore: %+v", m)
	}
	if m.AutoOps[0].Version != fixture.SetV2 {
		t.Fatal("restored op must target v2")
	}
}

func TestApplyRejectsCrossBandCandidate(t *testing.T) {
	v1, _ := worlds(t)
	var f2cand model.Candidate
	for _, c := range v1.ByID {
		if c.Frame == 5 && c.Band == 1 {
			f2cand = c
		}
	}
	op := model.Operation{ID: 1, SpeakerID: "s1", UtterID: "ai-a",
		Version: fixture.SetV1, Kind: model.KindMove,
		Items: []model.OpItem{{Frame: 5, Band: 0, CandidateID: f2cand.ID}}}
	if err := v1.Validate(op); err == nil {
		t.Fatal("candidate owned by F2 must not be used as an F1 target")
	}
}

func TestNoInterpolationAcrossGap(t *testing.T) {
	v1, _ := worlds(t)
	grid, err := track.Apply(v1, nil)
	if err != nil {
		t.Fatal(err)
	}
	cells := track.Final(v1, grid)
	for _, c := range cells {
		if c.Frame == 13 || c.Frame == 14 {
			if c.Present || c.Freq != 0 || c.CandidateID != 0 {
				t.Fatalf("missing frame cell must be empty: %+v", c)
			}
		}
	}
}
