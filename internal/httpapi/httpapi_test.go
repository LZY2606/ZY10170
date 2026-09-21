package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"voiceprintbench/internal/fixture"
	"voiceprintbench/internal/model"
	"voiceprintbench/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "http.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ResetAndImport(context.Background(), fixture.Build()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(st).Handler())
	t.Cleanup(srv.Close)
	t.Cleanup(func() { st.Close() })
	return srv, st
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET %s -> %d", url, res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func postJSON(t *testing.T, url string, body any, v any) {
	t.Helper()
	buf, _ := json.Marshal(body)
	res, err := http.Post(url, "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		t.Fatalf("POST %s -> %d", url, res.StatusCode)
	}
	if v != nil {
		if err := json.NewDecoder(res.Body).Decode(v); err != nil {
			t.Fatal(err)
		}
	}
}

type stateResp struct {
	View map[string]struct {
		Operations []model.Operation `json:"operations"`
		Conflicts  []model.Conflict  `json:"conflicts"`
		Autos      []model.AutoTrack `json:"autos"`
		PerGen     map[string]struct {
			Issues []struct {
				Kind  string `json:"kind"`
				Frame int    `json:"frame"`
				Rank  int    `json:"rank"`
			} `json:"issues"`
		} `json:"perGen"`
	} `json:"view"`
}

func findCandID(t *testing.T, base string, gen, frame int, freq float64) string {
	var st struct {
		View map[string]struct {
			Gens []model.GenCandidates `json:"gens"`
		} `json:"view"`
	}
	getJSON(t, base+"/api/state", &st)
	for _, gc := range st.View["u01"].Gens {
		if gc.Gen == gen && gc.Frame == frame {
			for _, c := range gc.Candidates {
				if c.Frequency == freq {
					return c.ID
				}
			}
		}
	}
	t.Fatalf("候选 gen%d f%d %.0f 未找到", gen, frame, freq)
	return ""
}

func submitCorrections(t *testing.T, base string) {
	// f22 两处拖点。
	id1382 := findCandID(t, base, 1, 22, 1382)
	id1288 := findCandID(t, base, 1, 22, 1288)
	postJSON(t, base+"/api/operations", model.Operation{
		UtteranceID: "u01", Gen: 1, Type: model.OpDrag, Frame: 22, Rank: 2,
		Drag: &model.DragPayload{FromCandidateID: id1382, ToCandidateID: id1288},
	}, nil)
	postJSON(t, base+"/api/operations", model.Operation{
		UtteranceID: "u01", Gen: 1, Type: model.OpDrag, Frame: 22, Rank: 3,
		Drag: &model.DragPayload{FromCandidateID: id1288, ToCandidateID: id1382},
	}, nil)
	// f47 两段重选(各只含到达帧)。
	id1388 := findCandID(t, base, 1, 47, 1388)
	id1486 := findCandID(t, base, 1, 47, 1486)
	postJSON(t, base+"/api/operations", model.Operation{
		UtteranceID: "u01", Gen: 1, Type: model.OpReselect, Rank: 2,
		Reselect: &model.ReselectPayload{StartFrame: 47, EndFrame: 48,
			Picked: map[int]string{47: id1388}},
	}, nil)
	postJSON(t, base+"/api/operations", model.Operation{
		UtteranceID: "u01", Gen: 1, Type: model.OpReselect, Rank: 3,
		Reselect: &model.ReselectPayload{StartFrame: 47, EndFrame: 48,
			Picked: map[int]string{47: id1486}},
	}, nil)
	// 锁定 v2 起始边界 f24 的 F1(700)。
	id700 := findCandID(t, base, 1, 24, 700)
	postJSON(t, base+"/api/operations", model.Operation{
		UtteranceID: "u01", Gen: 1, Type: model.OpLock, Frame: 24,
		Lock: &model.LockPayload{Frame: 24, VowelID: "v2", Edge: "start", CandidateID: id700},
	}, nil)
}

func TestEndToEndCorrectionReplayExport(t *testing.T) {
	srv, _ := newTestServer(t)
	base := srv.URL

	submitCorrections(t, base)

	// 修订后 gen1 无任何口径问题。
	var after stateResp
	getJSON(t, base+"/api/state", &after)
	if len(after.View["u01"].PerGen["gen1"].Issues) != 0 {
		t.Fatalf("修订后 gen1 仍有问题: %+v", after.View["u01"].PerGen["gen1"].Issues)
	}

	// 重放到 gen2。
	var rep struct {
		Conflicts []model.Conflict `json:"conflicts"`
	}
	postJSON(t, base+"/api/replay", map[string]any{
		"utteranceId": "u01", "fromGen": 1, "toGen": 2,
	}, &rep)
	if len(rep.Conflicts) != 1 {
		t.Fatalf("应有恰好1条歧义冲突, 实际 %d", len(rep.Conflicts))
	}
	c := rep.Conflicts[0]
	if c.Type != "ambiguous" || c.Frame != 22 || c.Rank != 2 || len(c.CandidateIDs) != 2 {
		t.Fatalf("歧义内容不符: %+v", c)
	}

	// gen2 最终轨迹无问题(歧义未强选, 回退自动轨迹)。
	var st2 stateResp
	getJSON(t, base+"/api/state", &st2)
	if len(st2.View["u01"].PerGen["gen2"].Issues) != 0 {
		t.Fatalf("gen2 不应有口径问题: %+v", st2.View["u01"].PerGen["gen2"].Issues)
	}

	// 人工从两项中指定 1318。
	var id1318 string
	var full struct {
		View map[string]struct {
			Gens []model.GenCandidates `json:"gens"`
		} `json:"view"`
	}
	getJSON(t, base+"/api/state", &full)
	for _, gc := range full.View["u01"].Gens {
		if gc.Gen == 2 && gc.Frame == 22 {
			for _, x := range gc.Candidates {
				if x.Frequency == 1318 {
					id1318 = x.ID
				}
			}
		}
	}
	postJSON(t, base+"/api/conflicts/resolve", map[string]any{
		"utteranceId": "u01", "toGen": 2,
		"picks": map[string]string{c.ID: id1318},
	}, nil)

	var st3 stateResp
	getJSON(t, base+"/api/state", &st3)
	if len(st3.View["u01"].PerGen["gen2"].Issues) != 0 {
		t.Fatalf("解决歧义后 gen2 不应有问题: %+v", st3.View["u01"].PerGen["gen2"].Issues)
	}
	allResolved := true
	for _, cf := range st3.View["u01"].Conflicts {
		if !cf.Resolved {
			allResolved = false
		}
	}
	if !allResolved {
		t.Fatal("冲突应已标记解决")
	}

	// 导出: 原始 auto 点 f22 F2 仍是 1382, 从未被改写。
	var bundle struct {
		AutoTracks map[string][]model.AutoTrack `json:"autoTracks"`
		Operations map[string][]model.Operation `json:"operations"`
		Finals     map[string]map[string][][]struct {
			Frame     int     `json:"frame"`
			Rank      int     `json:"rank"`
			Frequency float64 `json:"frequency"`
			Missing   bool    `json:"missing"`
		} `json:"finals"`
	}
	getJSON(t, base+"/api/export", &bundle)
	rawF22F2 := -1.0
	for _, a := range bundle.AutoTracks["u01"] {
		if a.Gen == 1 && a.Frame == 22 && a.Rank == 2 {
			rawF22F2 = a.Frequency
		}
	}
	if rawF22F2 != 1382 {
		t.Fatalf("导出的原始点被改写: gen1 f22 F2=%v, 期望 1382", rawF22F2)
	}
	if len(bundle.Operations["u01"]) < 5 {
		t.Fatalf("导出应包含完整操作序列(>=5), 实际 %d", len(bundle.Operations["u01"]))
	}
	// 导出缺帧: f42..f46 在 gen1 最终值里为 missing, 且没有 0Hz 插值。
	for f := 42; f <= 46; f++ {
		row := bundle.Finals["u01"]["gen1"][f]
		if len(row) != 1 || !row[0].Missing {
			t.Fatalf("导出 f%d 应为单个 missing 标记, 实际 %v", f, row)
		}
	}
}

func TestReimportClearsOperations(t *testing.T) {
	srv, st := newTestServer(t)
	base := srv.URL
	submitCorrections(t, base)
	var before stateResp
	getJSON(t, base+"/api/state", &before)
	if len(before.View["u01"].Operations) != 5 {
		t.Fatalf("应已有5条操作, 实际 %d", len(before.View["u01"].Operations))
	}
	res, err := http.Post(base+"/api/admin/reimport", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	var after stateResp
	getJSON(t, base+"/api/state", &after)
	if len(after.View["u01"].Operations) != 0 {
		t.Fatalf("清空重导入后操作应为0, 实际 %d", len(after.View["u01"].Operations))
	}
	// 固定数据仍在, 且 gen1 又恢复原算法的 f22 违规(证明重导入的是原始数据)。
	found := false
	for _, iss := range after.View["u01"].PerGen["gen1"].Issues {
		if iss.Kind == "order" && iss.Frame == 22 {
			found = true
		}
	}
	if !found {
		t.Fatal("重导入后应恢复原算法 f22 递增违规")
	}
	_ = st
}

func TestPageServesTitle(t *testing.T) {
	srv, _ := newTestServer(t)
	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf := make([]byte, 4096)
	n, _ := res.Body.Read(buf)
	body := string(buf[:n])
	if !bytes.Contains(buf[:n], []byte("声纹校形台")) {
		t.Fatalf("首页缺少标题: %q", body)
	}
}

func TestInvalidOpOnMissingFrame(t *testing.T) {
	srv, _ := newTestServer(t)
	buf, _ := json.Marshal(map[string]any{
		"utteranceId": "u01", "gen": 1, "type": "drag",
		"frame": 44, "rank": 1,
		"drag": map[string]string{"toCandidateId": "x"},
	})
	res, err := http.Post(srv.URL+"/api/operations", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("缺帧拖点应返回400, 实际 %d", res.StatusCode)
	}
}
