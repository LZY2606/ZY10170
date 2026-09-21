package ops

import (
	"sort"

	"voiceprintbench/internal/fixture"
	"voiceprintbench/internal/model"
)

// Touch 是一次操作在某帧某个 rank 上留下的“触点”: 最终应选哪个候选。
type Touch struct {
	Frame       int     `json:"frame"`
	Rank        int     `json:"rank"`
	CandidateID string  `json:"candidateId"`
	Frequency   float64 `json:"frequency"`
	FromOpID    string  `json:"fromOpId"`
}

// OpTouches 展开一条操作覆盖的全部触点。
func OpTouches(d *model.Dataset, uid string, op model.Operation) []Touch {
	var ts []Touch
	add := func(frame, rank int, cid string, freq float64) {
		ts = append(ts, Touch{Frame: frame, Rank: rank, CandidateID: cid, Frequency: freq, FromOpID: op.ID})
	}
	switch op.Type {
	case model.OpDrag:
		cs := FrameCandidates(d, uid, op.Gen, op.Frame)
		if c, ok := CandidateByID(cs, op.Drag.ToCandidateID); ok {
			add(op.Frame, op.Rank, c.ID, c.Frequency)
		}
	case model.OpReselect:
		frames := make([]int, 0, len(op.Reselect.Picked))
		for f := range op.Reselect.Picked {
			frames = append(frames, f)
		}
		sort.Ints(frames)
		for _, f := range frames {
			cid := op.Reselect.Picked[f]
			cs := FrameCandidates(d, uid, op.Gen, f)
			if c, ok := CandidateByID(cs, cid); ok {
				add(f, op.Rank, c.ID, c.Frequency)
			}
		}
	case model.OpLock:
		cs := FrameCandidates(d, uid, op.Gen, op.Lock.Frame)
		if c, ok := CandidateByID(cs, op.Lock.CandidateID); ok {
			// 锁定边界帧不改变任何 rank 的选择, 仅记录锚点; 不产生覆盖触点。
			_ = c
		}
	}
	return ts
}

// MergeTouches 按操作顺序合并触点(后写覆盖), 返回 frame->rank->Touch。
func MergeTouches(d *model.Dataset, uid string, ops []model.Operation) map[int]map[int]Touch {
	out := map[int]map[int]Touch{}
	for _, op := range ops {
		for _, t := range OpTouches(d, uid, op) {
			if out[t.Frame] == nil {
				out[t.Frame] = map[int]Touch{}
			}
			out[t.Frame][t.Rank] = t
		}
	}
	return out
}

// FinalPoint 是最终轨迹上的一个点。Missing 表示缺帧(无值, 不补零)。
type FinalPoint struct {
	Frame       int     `json:"frame"`
	Rank        int     `json:"rank"`
	Frequency   float64 `json:"frequency"`
	Confidence  float64 `json:"confidence"`
	CandidateID string  `json:"candidateId"`
	Missing     bool    `json:"missing"`
	Edited      bool    `json:"edited"`
}

// BuildFinal 以某代自动轨迹为底, 叠加该代触点, 生成最终轨迹。
// 缺帧不输出点; 其余帧输出 rank 1..4, 每帧频率要求严格递增(由 ValidateFinal 检查)。
func BuildFinal(d *model.Dataset, uid string, gen int, touches map[int]map[int]Touch) [][]FinalPoint {
	u := utterance(d, uid)
	out := make([][]FinalPoint, 0, u.NumFrames)
	for f := 0; f < u.NumFrames; f++ {
		cs := FrameCandidates(d, uid, gen, f)
		if fixture.MissingFrame(uid, f) || len(cs) == 0 {
			out = append(out, []FinalPoint{{Frame: f, Missing: true}})
			continue
		}
		row := make([]FinalPoint, 0, 4)
		for rank := 1; rank <= 4; rank++ {
			if t, ok := touches[f][rank]; ok {
				row = append(row, FinalPoint{
					Frame: f, Rank: rank, Frequency: t.Frequency,
					CandidateID: t.CandidateID, Confidence: confidenceOf(cs, t.CandidateID),
					Edited: true,
				})
				continue
			}
			if a, ok := AutoAt(d, uid, gen, f, rank); ok {
				row = append(row, FinalPoint{
					Frame: f, Rank: rank, Frequency: a.Frequency,
					CandidateID: a.CandidateID, Confidence: a.Confidence,
				})
			}
		}
		out = append(out, row)
	}
	return out
}

func confidenceOf(cs []model.Candidate, id string) float64 {
	if c, ok := CandidateByID(cs, id); ok {
		return c.Confidence
	}
	return 0
}

// ValidateFinal 检查最终轨迹的口径:
//  1. 每个可见帧 F1<F2<F3<F4 严格递增, 且四个 rank 不得选中同一候选;
//  2. 轨迹身份不得跨缺帧缺口偷偷交换: 缺口两侧同一 rank 用线性外推核对,
//     偏差超过 GapContinuityTolHz 即报 cross_gap_swap。
func ValidateFinal(d *model.Dataset, uid string, final [][]FinalPoint) []ValidationIssue {
	var issues []ValidationIssue

	for f, row := range final {
		if len(row) == 1 && row[0].Missing {
			continue
		}
		freqs := make([]float64, 0, 4)
		seen := map[string]bool{}
		for _, p := range row {
			freqs = append(freqs, p.Frequency)
			if seen[p.CandidateID] {
				issues = append(issues, ValidationIssue{
					Kind: "order", Frame: f, Rank: p.Rank,
					Message: "同一候选被两个 rank 选中, 轨迹交换身份",
				})
			}
			seen[p.CandidateID] = true
		}
		if len(freqs) == 4 && !strictlyIncreasing(freqs) {
			issues = append(issues, ValidationIssue{
				Kind: "order", Frame: f,
				Message: "同帧 F1..F4 未严格递增, 存在跳轨/换轨",
			})
		}
	}

	issues = append(issues, checkGapSwaps(final)...)
	return issues
}

// visibleRows 返回非缺帧的行索引。
func visibleRows(final [][]FinalPoint) []int {
	var out []int
	for f, row := range final {
		if !(len(row) == 1 && row[0].Missing) {
			out = append(out, f)
		}
	}
	return out
}

// checkGapSwaps 对每个缺帧缺口, 用缺口前最后两个可见帧线性外推到缺口后第一帧,
// 按 rank 逐一核对; 任一 rank 偏差超容差即判定发生跨不可见帧的身份交换。
func checkGapSwaps(final [][]FinalPoint) []ValidationIssue {
	vis := visibleRows(final)
	var issues []ValidationIssue
	for i := 1; i < len(vis); i++ {
		prev, cur := vis[i-1], vis[i]
		gap := cur - prev
		if gap <= 1 {
			continue
		}
		prevRow, curRow := final[prev], final[cur]
		// 至少需要缺口前两个连续可见帧才能做线性外推。
		if i < 2 || vis[i-2] != prev-1 {
			continue
		}
		prev2Row := final[prev-1]
		for rank := 1; rank <= 4; rank++ {
			p0 := pointAt(prev2Row, rank)
			p1 := pointAt(prevRow, rank)
			q := pointAt(curRow, rank)
			if p0 == nil || p1 == nil || q == nil {
				continue
			}
			pred := p1.Frequency + float64(gap)*(p1.Frequency-p0.Frequency)
			delta := absFloat(q.Frequency - pred)
			if delta > fixture.GapContinuityTolHz {
				issues = append(issues, ValidationIssue{
					Kind: "cross_gap_swap", Frame: cur, Rank: rank, Detail: delta,
					Message: "轨迹跨过缺帧缺口后偏离线性外推, 疑似身份交换",
				})
			}
		}
	}
	return issues
}

func pointAt(row []FinalPoint, rank int) *FinalPoint {
	for i := range row {
		if row[i].Rank == rank {
			return &row[i]
		}
	}
	return nil
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
