package httpapi

import (
	"context"

	"voiceprintbench/internal/export"
	"voiceprintbench/internal/fixture"
	"voiceprintbench/internal/model"
	"voiceprintbench/internal/ops"
)

// State 是页面首屏所需全部数据。
type State struct {
	Speakers   []model.Speaker           `json:"speakers"`
	Utterances []model.Utterance         `json:"utterances"`
	View       map[string]*UtteranceView `json:"view"`
}

// UtteranceView 单个发音的候选、自动轨迹、元音、操作、冲突与最终轨迹。
type UtteranceView struct {
	Utterance  model.Utterance       `json:"utterance"`
	Vowels     []model.VowelRange    `json:"vowels"`
	Gens       []model.GenCandidates `json:"gens"`
	Autos      []model.AutoTrack     `json:"autos"`
	Operations []model.Operation     `json:"operations"`
	Conflicts  []model.Conflict      `json:"conflicts"`
	PerGen     map[string]*GenView   `json:"perGen"`
	Replay     *ops.ReplayReport     `json:"replay,omitempty"`
}

// GenView 某代候选上的最终轨迹与校验。
type GenView struct {
	Gen    int                   `json:"gen"`
	Final  [][]ops.FinalPoint    `json:"final"`
	Issues []ops.ValidationIssue `json:"issues"`
}

// ComputeView 计算单个发音视图。gen1 叠加 gen1 操作; gen2 叠加
// “gen2 原生操作”和“gen1 重放到 gen2 的唯一映射触点”; 歧义触点不强行选择。
func ComputeView(ctx context.Context, d *model.Dataset, uid string,
	operations []model.Operation, conflicts []model.Conflict) *UtteranceView {
	u := utterance(d, uid)
	v := &UtteranceView{
		Utterance:  *u,
		Vowels:     d.Vowels[uid],
		Operations: nonNilOps(operations),
		Conflicts:  nonNilConflicts(conflicts),
		PerGen:     map[string]*GenView{},
	}
	for _, gc := range d.Gens[uid] {
		v.Gens = append(v.Gens, gc)
	}
	for _, a := range d.Autos[uid] {
		v.Autos = append(v.Autos, a)
	}

	genCount := fixture.GenCount(uid)

	// gen1: 直接叠加 gen1 操作。
	g1Ops := filterOps(operations, uid, 1)
	g1Touches := ops.MergeTouches(d, uid, g1Ops)
	g1Final := ops.BuildFinal(d, uid, 1, g1Touches)
	v.PerGen["gen1"] = &GenView{Gen: 1, Final: g1Final, Issues: ops.ValidateFinal(d, uid, g1Final)}

	if genCount >= 2 {
		// gen2 原生触点。
		g2Native := filterOps(operations, uid, 2)
		// gen1 -> gen2 重放。
		rep := ops.Replay(d, uid, g1Ops, 1, 2)
		v.Replay = &rep
		touches := map[int]map[int]ops.Touch{}
		for _, mt := range rep.Touches {
			if mt.Status != "unique" {
				continue
			}
			if touches[mt.Frame] == nil {
				touches[mt.Frame] = map[int]ops.Touch{}
			}
			touches[mt.Frame][mt.Rank] = ops.Touch{
				Frame: mt.Frame, Rank: mt.Rank,
				CandidateID: mt.Matches[0].ID, Frequency: mt.Matches[0].Frequency,
				FromOpID: mt.FromOpID,
			}
		}
		// 叠加 gen2 原生操作(冲突解决产生的 gen2 操作优先)。
		for _, t := range ops.MergeTouches(d, uid, g2Native) {
			for rank, touch := range t {
				if touches[touch.Frame] == nil {
					touches[touch.Frame] = map[int]ops.Touch{}
				}
				touches[touch.Frame][rank] = touch
			}
		}
		g2Final := ops.BuildFinal(d, uid, 2, touches)
		v.PerGen["gen2"] = &GenView{Gen: 2, Final: g2Final, Issues: ops.ValidateFinal(d, uid, g2Final)}
	}
	return v
}

func utterance(d *model.Dataset, uid string) *model.Utterance {
	for i := range d.Utterances {
		if d.Utterances[i].ID == uid {
			return &d.Utterances[i]
		}
	}
	return nil
}

func filterOps(all []model.Operation, uid string, gen int) []model.Operation {
	var out []model.Operation
	for _, op := range all {
		if op.UtteranceID == uid && op.Gen == gen {
			out = append(out, op)
		}
	}
	return out
}

// BuildExportResult 汇总导出所需的每代最终轨迹与冲突。
func BuildExportResult(d *model.Dataset, allOps []model.Operation,
	confs map[string][]model.Conflict) export.Result {
	res := export.Result{
		Finals: map[string]map[int][][]ops.FinalPoint{},
		Issues: map[string]map[int][]ops.ValidationIssue{},
		Confs:  confs,
	}
	for _, u := range d.Utterances {
		uid := u.ID
		view := ComputeView(context.Background(), d, uid, allOps, confs[uid])
		res.Finals[uid] = map[int][][]ops.FinalPoint{}
		res.Issues[uid] = map[int][]ops.ValidationIssue{}
		for label, gv := range view.PerGen {
			gen := 1
			if label == "gen2" {
				gen = 2
			}
			res.Finals[uid][gen] = gv.Final
			res.Issues[uid][gen] = gv.Issues
		}
	}
	return res
}

func nonNilOps(xs []model.Operation) []model.Operation {
	if xs == nil {
		return []model.Operation{}
	}
	return xs
}

func nonNilConflicts(xs []model.Conflict) []model.Conflict {
	if xs == nil {
		return []model.Conflict{}
	}
	return xs
}
