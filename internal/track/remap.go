package track

import (
	"sort"

	"voicebench/internal/model"
)

// Remap replays an operation sequence from an old candidate universe onto a
// recomputed one.
//
// Matching is deliberately conservative: same frame, same band, frequency
// within tolerance. When more than one recomputed candidate lies in the window
// the item becomes a conflict and is NEVER resolved by "nearest distance".
// Items whose frames vanished (e.g. still missing) are unresolved.
//
// Fully mapped move/path/lock operations are returned as AutoOps ready to be
// applied on the new set; any item that is conflict or unresolved suppresses
// automatic restoration of the containing operation and lands in Refs.
func Remap(from, to *Data, ops []model.Operation,
	tolerance float64) (*model.Mapping, error) {
	m := &model.Mapping{
		SpeakerID:   opsSpeaker(ops),
		FromVersion: from.Version,
		ToVersion:   to.Version,
		ToleranceHz: tolerance,
	}
	for _, op := range ops {
		if op.Version != from.Version {
			continue // only replay operations of the old active set
		}
		items := make([]model.OpItem, len(op.Items))
		clean := true
		for i, it := range op.Items {
			old, ok := from.ByID[it.CandidateID]
			if !ok {
				items[i] = it
				m.Refs = append(m.Refs, model.Ref{
					FromOpID: op.ID, Frame: it.Frame, Band: it.Band,
					OldCandID: it.CandidateID, Status: model.StatusUnresolved,
				})
				m.UnresolvedCount++
				clean = false
				continue
			}
			options := candidatesInWindow(to, it.Frame, it.Band, old.Freq, tolerance)
			ref := model.Ref{FromOpID: op.ID, Frame: it.Frame, Band: it.Band,
				OldCandID: it.CandidateID}
			switch len(options) {
			case 1:
				ref.Status = model.StatusUnique
				ref.UniqueCand = options[0].ID
				items[i] = model.OpItem{
					Frame: it.Frame, Band: it.Band,
					CandidateID: options[0].ID, Freq: options[0].Freq,
				}
				m.UniqueCount++
			case 0:
				ref.Status = model.StatusUnresolved
				items[i] = it
				clean = false
				m.UnresolvedCount++
			default:
				ids := make([]int64, len(options))
				for j, c := range options {
					ids[j] = c.ID
				}
				ref.Status = model.StatusConflict
				ref.Options = ids
				items[i] = it
				clean = false
				m.ConflictCount++
			}
			m.Refs = append(m.Refs, ref)
		}
		if clean {
			restored := model.Operation{
				SpeakerID: op.SpeakerID, UtterID: op.UtterID,
				Version: to.Version, Kind: op.Kind,
				Note:   "自动映射自操作 #" + itoa(int(op.ID)),
				Author: "remap", Items: items,
			}
			m.AutoOps = append(m.AutoOps, restored)
		}
	}
	sort.SliceStable(m.Refs, func(i, j int) bool {
		if m.Refs[i].FromOpID != m.Refs[j].FromOpID {
			return m.Refs[i].FromOpID < m.Refs[j].FromOpID
		}
		if m.Refs[i].Frame != m.Refs[j].Frame {
			return m.Refs[i].Frame < m.Refs[j].Frame
		}
		return m.Refs[i].Band < m.Refs[j].Band
	})
	return m, nil
}

// candidatesInWindow lists recomputed candidates in [f-tol, f+tol], ascending.
func candidatesInWindow(d *Data, frame, band int, f, tol float64) []model.Candidate {
	var out []model.Candidate
	for _, c := range d.Cand[frame][band] {
		if c.Freq >= f-tol && c.Freq <= f+tol {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Freq < out[j].Freq })
	return out
}

func opsSpeaker(ops []model.Operation) string {
	for _, op := range ops {
		if op.SpeakerID != "" {
			return op.SpeakerID
		}
	}
	return ""
}
