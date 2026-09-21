package ops

import (
	"fmt"

	"voiceprintbench/internal/fixture"
	"voiceprintbench/internal/model"
)

// ValidationIssue 描述一处最终轨迹违背口径的问题。
type ValidationIssue struct {
	Kind    string  `json:"kind"` // order | cross_gap_swap
	Frame   int     `json:"frame"`
	Rank    int     `json:"rank,omitempty"`
	Message string  `json:"message"`
	Detail  float64 `json:"detail,omitempty"`
}

// ValidateOp 校验一条操作是否引用了合法候选, 且半开区间、rank 等口径正确。
func ValidateOp(d *model.Dataset, uid string, op model.Operation) error {
	u := utterance(d, uid)
	if u == nil {
		return fmt.Errorf("未知发音 %q", uid)
	}
	if op.Gen < 1 || op.Gen > fixture.GenCount(uid) {
		return fmt.Errorf("非法候选代 %d", op.Gen)
	}
	if op.Frame < 0 || op.Frame >= u.NumFrames {
		return fmt.Errorf("帧 %d 越界 [0,%d)", op.Frame, u.NumFrames)
	}
	switch op.Type {
	case model.OpDrag:
		if op.Rank < 1 || op.Rank > 4 {
			return fmt.Errorf("rank 必须在 1..4")
		}
		if op.Drag == nil {
			return fmt.Errorf("drag 缺少载荷")
		}
		cs := FrameCandidates(d, uid, op.Gen, op.Frame)
		if len(cs) == 0 {
			return fmt.Errorf("帧 %d 为缺帧, 不能拖点(不做零频率插值)", op.Frame)
		}
		if _, ok := CandidateByID(cs, op.Drag.ToCandidateID); !ok {
			return fmt.Errorf("目标候选 %s 不在第 %d 代帧 %d", op.Drag.ToCandidateID, op.Gen, op.Frame)
		}
		if op.Drag.FromCandidateID != "" {
			if _, ok := CandidateByID(cs, op.Drag.FromCandidateID); !ok {
				return fmt.Errorf("原候选 %s 不在第 %d 代帧 %d", op.Drag.FromCandidateID, op.Gen, op.Frame)
			}
		}
	case model.OpReselect:
		if op.Rank < 1 || op.Rank > 4 {
			return fmt.Errorf("rank 必须在 1..4")
		}
		if op.Reselect == nil {
			return fmt.Errorf("reselect_path 缺少载荷")
		}
		p := op.Reselect
		if p.StartFrame < 0 || p.EndFrame > u.NumFrames || p.StartFrame >= p.EndFrame {
			return fmt.Errorf("半开区间 [%d,%d) 非法", p.StartFrame, p.EndFrame)
		}
		for f, cid := range p.Picked {
			if f < p.StartFrame || f >= p.EndFrame {
				return fmt.Errorf("重选帧 %d 不在 [%d,%d)", f, p.StartFrame, p.EndFrame)
			}
			cs := FrameCandidates(d, uid, op.Gen, f)
			if len(cs) == 0 {
				return fmt.Errorf("帧 %d 为缺帧, 不能重选(缺帧保持无值)", f)
			}
			if _, ok := CandidateByID(cs, cid); !ok {
				return fmt.Errorf("候选 %s 不在第 %d 代帧 %d", cid, op.Gen, f)
			}
		}
	case model.OpLock:
		if op.Lock == nil {
			return fmt.Errorf("lock_boundary 缺少载荷")
		}
		if op.Lock.Edge != "start" && op.Lock.Edge != "end" {
			return fmt.Errorf("edge 必须为 start 或 end")
		}
		cs := FrameCandidates(d, uid, op.Gen, op.Lock.Frame)
		if len(cs) == 0 {
			return fmt.Errorf("边界帧 %d 为缺帧, 不能锁定", op.Lock.Frame)
		}
		if _, ok := CandidateByID(cs, op.Lock.CandidateID); !ok {
			return fmt.Errorf("锁定候选 %s 不在第 %d 代帧 %d", op.Lock.CandidateID, op.Gen, op.Lock.Frame)
		}
	default:
		return fmt.Errorf("未知操作类型 %q", op.Type)
	}
	return nil
}

func utterance(d *model.Dataset, uid string) *model.Utterance {
	for i := range d.Utterances {
		if d.Utterances[i].ID == uid {
			return &d.Utterances[i]
		}
	}
	return nil
}
