package ops

import (
	"sort"

	"voiceprintbench/internal/model"
)

// FrameCandidates 取某发音某代某帧的候选集合(缺帧为空)。
func FrameCandidates(d *model.Dataset, uid string, gen, frame int) []model.Candidate {
	for _, gc := range d.Gens[uid] {
		if gc.Gen == gen && gc.Frame == frame {
			return gc.Candidates
		}
	}
	return nil
}

// CandidateByID 按 ID 找候选。
func CandidateByID(cs []model.Candidate, id string) (model.Candidate, bool) {
	for _, c := range cs {
		if c.ID == id {
			return c, true
		}
	}
	return model.Candidate{}, false
}

// AutoAt 取某代某帧某 rank 的自动轨迹点。
func AutoAt(d *model.Dataset, uid string, gen, frame, rank int) (model.AutoTrack, bool) {
	for _, a := range d.Autos[uid] {
		if a.Gen == gen && a.Frame == frame && a.Rank == rank {
			return a, true
		}
	}
	return model.AutoTrack{}, false
}

// sortedFrequencies 返回候选频率升序拷贝。
func sortedFrequencies(cs []model.Candidate) []float64 {
	out := make([]float64, len(cs))
	for i, c := range cs {
		out[i] = c.Frequency
	}
	sort.Float64s(out)
	return out
}

// strictlyIncreasing 判断切片是否严格递增。
func strictlyIncreasing(xs []float64) bool {
	for i := 1; i < len(xs); i++ {
		if xs[i] <= xs[i-1] {
			return false
		}
	}
	return true
}

// VowelAt 找某帧所在元音区间(半开); 无则 false。
func VowelAt(vs []model.VowelRange, frame int) (model.VowelRange, bool) {
	for _, v := range vs {
		if frame >= v.Start && frame < v.End {
			return v, true
		}
	}
	return model.VowelRange{}, false
}
