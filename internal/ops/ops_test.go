package ops

import (
	"testing"

	"voiceprintbench/internal/fixture"
	"voiceprintbench/internal/model"
)

func testDataset() *model.Dataset { return fixture.Build() }

const uid = "u01"

func candID(d *model.Dataset, gen, f int, freq float64) string {
	for _, gc := range d.Gens[uid] {
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

func correctionOps(d *model.Dataset) []model.Operation {
	return []model.Operation{
		{ID: "op-drag22-f2", UtteranceID: uid, Gen: 1, Type: model.OpDrag, Frame: 22, Rank: 2,
			Drag: &model.DragPayload{FromCandidateID: candID(d, 1, 22, 1382), ToCandidateID: candID(d, 1, 22, 1288)}},
		{ID: "op-drag22-f3", UtteranceID: uid, Gen: 1, Type: model.OpDrag, Frame: 22, Rank: 3,
			Drag: &model.DragPayload{FromCandidateID: candID(d, 1, 22, 1288), ToCandidateID: candID(d, 1, 22, 1382)}},
		{ID: "op-path47-f2", UtteranceID: uid, Gen: 1, Type: model.OpReselect, Rank: 2,
			Reselect: &model.ReselectPayload{StartFrame: 47, EndFrame: 48,
				Picked: map[int]string{47: candID(d, 1, 47, 1388)}}},
		{ID: "op-path47-f3", UtteranceID: uid, Gen: 1, Type: model.OpReselect, Rank: 3,
			Reselect: &model.ReselectPayload{StartFrame: 47, EndFrame: 48,
				Picked: map[int]string{47: candID(d, 1, 47, 1486)}}},
		{ID: "op-lock24", UtteranceID: uid, Gen: 1, Type: model.OpLock, Frame: 24,
			Lock: &model.LockPayload{Frame: 24, VowelID: "v2", Edge: "start",
				CandidateID: candID(d, 1, 24, 700)}},
	}
}

func TestGen1CorrectionsYieldStrictFinal(t *testing.T) {
	d := testDataset()
	opsList := correctionOps(d)
	for _, op := range opsList {
		if err := ValidateOp(d, uid, op); err != nil {
			t.Fatalf("操作 %s 非法: %v", op.ID, err)
		}
	}
	touches := MergeTouches(d, uid, opsList)
	final := BuildFinal(d, uid, 1, touches)
	issues := ValidateFinal(d, uid, final)
	if len(issues) != 0 {
		t.Fatalf("修订后 gen1 不应有口径问题: %+v", issues)
	}
	// f22 / f47 修正后严格递增。
	for _, f := range []int{22, 47} {
		row := final[f]
		if !(row[0].Frequency < row[1].Frequency && row[1].Frequency < row[2].Frequency &&
			row[2].Frequency < row[3].Frequency) {
			t.Fatalf("f%d 修订后仍未严格递增: %v", f, row)
		}
	}
}

func TestReplayUniqueAndAmbiguous(t *testing.T) {
	d := testDataset()
	rep := Replay(d, uid, correctionOps(d), 1, 2)

	uniq, amb, unmatched := 0, 0, 0
	for _, mt := range rep.Touches {
		switch mt.Status {
		case "unique":
			uniq++
		case "ambiguous":
			amb++
		case "unmatched":
			unmatched++
		}
	}
	if uniq != 3 {
		t.Fatalf("唯一映射触点应为3, 实际 %d", uniq)
	}
	if amb != 1 || unmatched != 0 {
		t.Fatalf("歧义应为1, unmatched=0; 实际 amb=%d unmatched=%d", amb, unmatched)
	}

	// 唯一映射自动恢复到对应新候选。
	wantUnique := map[[2]int]float64{{22, 3}: 1382, {47, 2}: 1388, {47, 3}: 1486}
	for _, mt := range rep.Touches {
		if mt.Status != "unique" {
			continue
		}
		want, ok := wantUnique[[2]int{mt.Frame, mt.Rank}]
		if !ok || mt.Matches[0].Frequency != want {
			t.Fatalf("唯一映射 (f%d,r%d) 异常: %+v", mt.Frame, mt.Rank, mt.Matches)
		}
	}

	// 歧义必须保留两项, 不按距离强选。
	if len(rep.Conflicts) != 1 {
		t.Fatalf("冲突列表应只有1条歧义, 实际 %d: %+v", len(rep.Conflicts), rep.Conflicts)
	}
	c := rep.Conflicts[0]
	if c.Type != "ambiguous" || c.Frame != 22 || c.Rank != 2 || len(c.CandidateIDs) != 2 {
		t.Fatalf("歧义内容不符: %+v", c)
	}

	// 歧义回退到 gen2 自动轨迹后, 最终轨迹仍满足严格递增(没有被强行选错)。
	if len(rep.Issues) != 0 {
		t.Fatalf("歧义回退自动轨迹后不应违规: %+v", rep.Issues)
	}
}

func TestResolveAmbiguity(t *testing.T) {
	d := testDataset()
	rep := Replay(d, uid, correctionOps(d), 1, 2)
	// 人工选择 1318Hz 的候选解决歧义。
	var cid1318 string
	for _, x := range FrameCandidates(d, uid, 2, 22) {
		if x.Frequency == 1318 {
			cid1318 = x.ID
		}
	}
	if cid1318 == "" {
		t.Fatal("gen2 f22 缺少 1318 候选")
	}
	final, err := FinalWithResolution(d, uid, rep, map[string]string{rep.Conflicts[0].ID: cid1318})
	if err != nil {
		t.Fatal(err)
	}
	if len(ValidateFinal(d, uid, final)) != 0 {
		t.Fatalf("人工解决歧义后应合规")
	}
	row := final[22]
	if !(row[1].Frequency == 1318 && row[2].Frequency == 1382) {
		t.Fatalf("解决后 f22 取值错误: F2=%v F3=%v", row[1].Frequency, row[2].Frequency)
	}

	// 只能从候选列表中选择。
	if _, err := FinalWithResolution(d, uid, rep,
		map[string]string{rep.Conflicts[0].ID: "c-u01-g2-f22-3238.0"}); err == nil {
		t.Fatal("选择候选列表之外的项必须被拒绝")
	}
}

func TestRawAutoNeverRewritten(t *testing.T) {
	d := testDataset()
	before := map[[2]int]model.AutoTrack{}
	for _, a := range d.Autos[uid] {
		if a.Gen == 1 {
			before[[2]int{a.Frame, a.Rank}] = a
		}
	}
	_ = BuildFinal(d, uid, 1, MergeTouches(d, uid, correctionOps(d)))
	_ = Replay(d, uid, correctionOps(d), 1, 2)
	for k, want := range before {
		got, ok := lookupAuto(d, 1, k[0], k[1])
		if !ok || got.CandidateID != want.CandidateID || got.Frequency != want.Frequency {
			t.Fatalf("原始自动轨迹点被改写: %+v -> %+v", want, got)
		}
	}
}

func lookupAuto(d *model.Dataset, gen, frame, rank int) (model.AutoTrack, bool) {
	return AutoAt(d, uid, gen, frame, rank)
}

func TestMissingFramesNotInterpolated(t *testing.T) {
	d := testDataset()
	final := BuildFinal(d, uid, 1, MergeTouches(d, uid, correctionOps(d)))
	for f := 42; f <= 46; f++ {
		row := final[f]
		if len(row) != 1 || !row[0].Missing {
			t.Fatalf("缺帧 f%d 必须只有 Missing 标记, 实际 %v", f, row)
		}
	}
	// 缺帧上禁止任何操作。
	bad := model.Operation{ID: "bad", UtteranceID: uid, Gen: 1, Type: model.OpDrag,
		Frame: 44, Rank: 1,
		Drag: &model.DragPayload{ToCandidateID: "x"}}
	if err := ValidateOp(d, uid, bad); err == nil {
		t.Fatal("缺帧拖点必须被拒绝(不做零频率插值)")
	}
}

func TestCrossGapSwapDetected(t *testing.T) {
	d := testDataset()
	// 未经修订的 gen1 自动轨迹必须在 f47 被检出跨缺口身份交换。
	final := BuildFinal(d, uid, 1, nil)
	found := false
	for _, iss := range ValidateFinal(d, uid, final) {
		if iss.Kind == "cross_gap_swap" && iss.Frame == 47 {
			found = true
		}
	}
	if !found {
		t.Fatal("未检出 f47 跨缺帧缺口的身份交换")
	}
}

func TestHalfOpenReject(t *testing.T) {
	d := testDataset()
	// 重选区间右越界(48 不属于 [0,48))必须拒绝; [47,48) 合法。
	good := model.Operation{ID: "g", UtteranceID: uid, Gen: 1, Type: model.OpReselect, Rank: 2,
		Reselect: &model.ReselectPayload{StartFrame: 47, EndFrame: 48,
			Picked: map[int]string{47: candID(d, 1, 47, 1388)}}}
	if err := ValidateOp(d, uid, good); err != nil {
		t.Fatalf("[47,48) 应合法: %v", err)
	}
	bad := model.Operation{ID: "b", UtteranceID: uid, Gen: 1, Type: model.OpReselect, Rank: 2,
		Reselect: &model.ReselectPayload{StartFrame: 47, EndFrame: 49,
			Picked: map[int]string{47: candID(d, 1, 47, 1388)}}}
	if err := ValidateOp(d, uid, bad); err == nil {
		t.Fatal("[47,49) 越界必须拒绝")
	}
}

func TestSameCandidateTwoRanksRejected(t *testing.T) {
	d := testDataset()
	// 构造两个 rank 选同一候选: 最终校验应报 order。
	touches := map[int]map[int]Touch{
		22: {
			2: {Frame: 22, Rank: 2, CandidateID: candID(d, 1, 22, 1288), Frequency: 1288},
			3: {Frame: 22, Rank: 3, CandidateID: candID(d, 1, 22, 1288), Frequency: 1288},
		},
	}
	final := BuildFinal(d, uid, 1, touches)
	ok := false
	for _, iss := range ValidateFinal(d, uid, final) {
		if iss.Kind == "order" && iss.Frame == 22 {
			ok = true
		}
	}
	if !ok {
		t.Fatal("两个 rank 选同一候选(身份交换)必须被检出")
	}
}
