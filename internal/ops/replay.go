package ops

import (
	"fmt"
	"math"
	"sort"

	"voiceprintbench/internal/fixture"
	"voiceprintbench/internal/model"
)

// MappedTouch 是触点映射到新代后的结果。
type MappedTouch struct {
	Touch
	Status  string            `json:"status"`
	Matches []model.Candidate `json:"matches"`
}

// MappedLock 是边界锁定锚点的映射结果。
type MappedLock struct {
	Op      model.Operation   `json:"op"`
	Status  string            `json:"status"`
	Matches []model.Candidate `json:"matches"`
}

// ReplayReport 是把 fromGen 的操作重放到 toGen 的结果。
type ReplayReport struct {
	UtteranceID string           `json:"utteranceId"`
	FromGen     int              `json:"fromGen"`
	ToGen       int              `json:"toGen"`
	Touches     []MappedTouch    `json:"touches"`
	Locks       []MappedLock     `json:"locks"`
	Conflicts   []model.Conflict `json:"conflicts"`
	// Final: 在不强行解决歧义的前提下, 歧义触点回退为新代自动轨迹, 供页面展示。
	Final  [][]FinalPoint    `json:"final"`
	Issues []ValidationIssue `json:"issues"`
}

// mapCandidate 在同帧新候选中, 找出与旧频率相差 <= MapTolHz 的候选。
// 候选数: 1 唯一; >1 歧义(全部保留, 不按距离选); 0 unmatched。
func mapCandidate(old model.Candidate, next []model.Candidate) (string, []model.Candidate) {
	var hits []model.Candidate
	for _, c := range next {
		if math.Abs(c.Frequency-old.Frequency) <= fixture.MapTolHz {
			hits = append(hits, c)
		}
	}
	switch {
	case len(hits) == 1:
		return "unique", hits
	case len(hits) > 1:
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].Frequency < hits[j].Frequency })
		return "ambiguous", hits
	default:
		return "unmatched", nil
	}
}

// Replay 把操作序列从 fromGen 重放到 toGen。
// 旧操作不会被改写; 仅返回映射结果、冲突列表与“歧义回退到自动轨迹”的最终轨迹。
func Replay(d *model.Dataset, uid string, ops []model.Operation, fromGen, toGen int) ReplayReport {
	rep := ReplayReport{UtteranceID: uid, FromGen: fromGen, ToGen: toGen}
	conflictSeq := 0
	nextConflictID := func(opID string, frame, rank int) string {
		conflictSeq++
		return fmt.Sprintf("conflict-%s-g%dto%d-f%d-r%d-%d", opID, fromGen, toGen, frame, rank, conflictSeq)
	}

	// 触点: 旧代触点 -> 新代候选。
	oldTouches := MergeTouches(d, uid, filterGen(ops, fromGen))
	frames := make([]int, 0, len(oldTouches))
	for f := range oldTouches {
		frames = append(frames, f)
	}
	sort.Ints(frames)
	for _, f := range frames {
		next := FrameCandidates(d, uid, toGen, f)
		ranks := []int{1, 2, 3, 4}
		for _, rank := range ranks {
			t, ok := oldTouches[f][rank]
			if !ok {
				continue
			}
			oldCand := model.Candidate{ID: t.CandidateID, Frequency: t.Frequency}
			status, hits := mapCandidate(oldCand, next)
			mt := MappedTouch{Touch: t, Status: status, Matches: hits}
			rep.Touches = append(rep.Touches, mt)
			if status == "unique" {
				continue
			}
			ids := make([]string, 0, len(hits))
			for _, h := range hits {
				ids = append(ids, h.ID)
			}
			kind := status
			rep.Conflicts = append(rep.Conflicts, model.Conflict{
				ID:          nextConflictID(t.FromOpID, f, rank),
				UtteranceID: uid, FromOpID: t.FromOpID,
				FromGen: fromGen, ToGen: toGen, Type: kind,
				Frame: f, Rank: rank, OldCandidateID: t.CandidateID,
				OldFrequency: t.Frequency, CandidateIDs: ids,
			})
		}
	}

	// 锁定锚点: 同样要求唯一映射, 否则进入冲突(rank=0)。
	for _, op := range ops {
		if op.Gen != fromGen || op.Type != model.OpLock {
			continue
		}
		f := op.Lock.Frame
		next := FrameCandidates(d, uid, toGen, f)
		oldCand := model.Candidate{ID: op.Lock.CandidateID}
		if c, ok := CandidateByID(FrameCandidates(d, uid, fromGen, f), op.Lock.CandidateID); ok {
			oldCand = c
		}
		status, hits := mapCandidate(oldCand, next)
		rep.Locks = append(rep.Locks, MappedLock{Op: op, Status: status, Matches: hits})
		if status == "unique" {
			continue
		}
		ids := make([]string, 0, len(hits))
		for _, h := range hits {
			ids = append(ids, h.ID)
		}
		rep.Conflicts = append(rep.Conflicts, model.Conflict{
			ID:          nextConflictID(op.ID, f, 0),
			UtteranceID: uid, FromOpID: op.ID,
			FromGen: fromGen, ToGen: toGen, Type: status,
			Frame: f, OldCandidateID: op.Lock.CandidateID,
			OldFrequency: oldCand.Frequency, CandidateIDs: ids,
		})
	}

	// 歧义/未匹配不强行选: 仅唯一映射触点进入最终轨迹, 其余沿用新代自动轨迹。
	finalTouches := map[int]map[int]Touch{}
	for _, mt := range rep.Touches {
		if mt.Status != "unique" {
			continue
		}
		if finalTouches[mt.Frame] == nil {
			finalTouches[mt.Frame] = map[int]Touch{}
		}
		finalTouches[mt.Frame][mt.Rank] = Touch{
			Frame: mt.Frame, Rank: mt.Rank,
			CandidateID: mt.Matches[0].ID, Frequency: mt.Matches[0].Frequency,
			FromOpID: mt.FromOpID,
		}
	}
	rep.Final = BuildFinal(d, uid, toGen, finalTouches)
	rep.Issues = ValidateFinal(d, uid, rep.Final)
	return rep
}

func filterGen(ops []model.Operation, gen int) []model.Operation {
	var out []model.Operation
	for _, op := range ops {
		if op.Gen == gen {
			out = append(out, op)
		}
	}
	return out
}

// FinalWithResolution 在重放结果上叠加“人工解决冲突”的选择, 重新生成最终轨迹。
// picks 为 conflictID -> 新代候选 ID; 仅可选择该冲突候选列表中的候选。
func FinalWithResolution(d *model.Dataset, uid string, rep ReplayReport, picks map[string]string) ([][]FinalPoint, error) {
	touches := map[int]map[int]Touch{}
	for _, mt := range rep.Touches {
		if mt.Status != "unique" {
			continue
		}
		if touches[mt.Frame] == nil {
			touches[mt.Frame] = map[int]Touch{}
		}
		touches[mt.Frame][mt.Rank] = Touch{
			Frame: mt.Frame, Rank: mt.Rank,
			CandidateID: mt.Matches[0].ID, Frequency: mt.Matches[0].Frequency,
		}
	}
	confByID := map[string]model.Conflict{}
	for _, c := range rep.Conflicts {
		confByID[c.ID] = c
	}
	for cid, picked := range picks {
		conf, ok := confByID[cid]
		if !ok {
			return nil, fmt.Errorf("未知冲突 %s", cid)
		}
		allowed := false
		var cand model.Candidate
		for _, x := range FrameCandidates(d, uid, rep.ToGen, conf.Frame) {
			if x.ID == picked {
				allowed = true
				cand = x
				break
			}
		}
		if !allowed {
			return nil, fmt.Errorf("冲突 %s 的选择 %s 不在候选列表内", cid, picked)
		}
		if touches[conf.Frame] == nil {
			touches[conf.Frame] = map[int]Touch{}
		}
		if conf.Rank > 0 {
			touches[conf.Frame][conf.Rank] = Touch{
				Frame: conf.Frame, Rank: conf.Rank,
				CandidateID: cand.ID, Frequency: cand.Frequency, FromOpID: conf.FromOpID,
			}
		}
	}
	return BuildFinal(d, uid, rep.ToGen, touches), nil
}

// LockedFrames 返回某代已成功(唯一)锁定的边界帧集合。
func LockedFrames(rep ReplayReport) map[int]MappedLock {
	out := map[int]MappedLock{}
	for _, ml := range rep.Locks {
		if ml.Status == "unique" {
			out[ml.Op.Lock.Frame] = ml
		}
	}
	return out
}
