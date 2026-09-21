package export_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"voiceprintbench/internal/export"
	"voiceprintbench/internal/fixture"
	"voiceprintbench/internal/httpapi"
	"voiceprintbench/internal/model"
	"voiceprintbench/internal/ops"
	"voiceprintbench/internal/store"
)

// TestBundleAllowsIndependentReplay 从 HTTP 导出包读取原始候选与操作序列,
// 在纯内存重建的数据集上独立重放, 复现“唯一映射恢复 + 歧义保留两项”。
func TestBundleAllowsIndependentReplay(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "e.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.ResetAndImport(ctx, fixture.Build()); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(httpapi.New(st).Handler())
	defer ts.Close()

	// 通过 API 施加与验收一致的5条修订。
	candID := func(gen, f int, freq float64) string {
		d := fixture.Build()
		for _, gc := range d.Gens["u01"] {
			if gc.Gen == gen && gc.Frame == f {
				for _, c := range gc.Candidates {
					if c.Frequency == freq {
						return c.ID
					}
				}
			}
		}
		return ""
	}
	mustPost := func(op model.Operation) {
		buf, _ := json.Marshal(op)
		res, err := http.Post(ts.URL+"/api/operations", "application/json",
			bytes.NewReader(buf))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
	}
	mustPost(model.Operation{UtteranceID: "u01", Gen: 1, Type: model.OpDrag, Frame: 22, Rank: 2,
		Drag: &model.DragPayload{FromCandidateID: candID(1, 22, 1382), ToCandidateID: candID(1, 22, 1288)}})
	mustPost(model.Operation{UtteranceID: "u01", Gen: 1, Type: model.OpDrag, Frame: 22, Rank: 3,
		Drag: &model.DragPayload{FromCandidateID: candID(1, 22, 1288), ToCandidateID: candID(1, 22, 1382)}})
	mustPost(model.Operation{UtteranceID: "u01", Gen: 1, Type: model.OpReselect, Rank: 2,
		Reselect: &model.ReselectPayload{StartFrame: 47, EndFrame: 48, Picked: map[int]string{47: candID(1, 47, 1388)}}})
	mustPost(model.Operation{UtteranceID: "u01", Gen: 1, Type: model.OpReselect, Rank: 3,
		Reselect: &model.ReselectPayload{StartFrame: 47, EndFrame: 48, Picked: map[int]string{47: candID(1, 47, 1486)}}})
	mustPost(model.Operation{UtteranceID: "u01", Gen: 1, Type: model.OpLock, Frame: 24,
		Lock: &model.LockPayload{Frame: 24, VowelID: "v2", Edge: "start", CandidateID: candID(1, 24, 700)}})

	// 取导出包。
	res, err := http.Get(ts.URL + "/api/export")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var b export.Bundle
	if err := json.NewDecoder(res.Body).Decode(&b); err != nil {
		t.Fatal(err)
	}
	if b.Schema != export.SchemaVersion {
		t.Fatalf("导出 schema 不符: %s", b.Schema)
	}

	// 仅用导出包内的数据重建 Dataset(模拟独立重放)。
	d := &model.Dataset{
		Speakers: b.Speakers, Utterances: b.Utterances,
		Gens: b.Candidates, Autos: b.AutoTracks, Vowels: b.Vowels,
	}
	rep := ops.Replay(d, "u01", b.Operations["u01"], 1, 2)

	uniq, amb := 0, 0
	for _, mt := range rep.Touches {
		switch mt.Status {
		case "unique":
			uniq++
		case "ambiguous":
			amb++
		}
	}
	if uniq != 3 || amb != 1 || len(rep.Conflicts) != 1 {
		t.Fatalf("独立重放映射结果错误: uniq=%d amb=%d conflicts=%d",
			uniq, amb, len(rep.Conflicts))
	}
	if got := rep.Conflicts[0].CandidateIDs; len(got) != 2 {
		t.Fatalf("歧义必须保留两项, 实际 %v", got)
	}
	if len(rep.Issues) != 0 {
		t.Fatalf("重放最终轨迹应合规: %+v", rep.Issues)
	}

	// 导出包的原始 auto 点必须仍是算法原值。
	for _, a := range b.AutoTracks["u01"] {
		if a.Gen == 1 && a.Frame == 22 && a.Rank == 2 && a.Frequency != 1382 {
			t.Fatalf("导出原始点被改写: %v", a.Frequency)
		}
	}
}
