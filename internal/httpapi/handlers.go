package httpapi

import (
	"encoding/json"
	"net/http"

	"voiceprintbench/internal/export"
	"voiceprintbench/internal/fixture"
	"voiceprintbench/internal/model"
	"voiceprintbench/internal/ops"
)

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	d, err := s.dataset(r.Context())
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	st := State{Speakers: d.Speakers, Utterances: d.Utterances, View: map[string]*UtteranceView{}}
	for _, u := range d.Utterances {
		ops, err := s.store.ListOperations(r.Context(), u.ID)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		confs, err := s.store.ListConflicts(r.Context(), u.ID, -1)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		st.View[u.ID] = ComputeView(r.Context(), d, u.ID, ops, confs)
	}
	writeJSON(w, st)
}

func (s *Server) handleAddOp(w http.ResponseWriter, r *http.Request) {
	var op model.Operation
	if err := json.NewDecoder(r.Body).Decode(&op); err != nil {
		writeErr(w, 400, err)
		return
	}
	d, err := s.dataset(r.Context())
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if op.Gen == 0 {
		op.Gen = 1
	}
	if op.UtteranceID == "" {
		writeErr(w, 400, errBad("utteranceId 必填"))
		return
	}
	if err := ops.ValidateOp(d, op.UtteranceID, op); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.AddOperation(r.Context(), op); err != nil {
		writeErr(w, 500, err)
		return
	}
	_ = s.store.AddRun(r.Context(), "operation", "新增 "+op.Type+" 于 "+op.UtteranceID)
	writeJSON(w, op)
}

func (s *Server) handleDeleteOp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.DeleteOperation(r.Context(), id); err != nil {
		writeErr(w, 500, err)
		return
	}
	_ = s.store.AddRun(r.Context(), "operation", "撤销操作 "+id)
	w.WriteHeader(http.StatusNoContent)
}

type replayReq struct {
	UtteranceID string `json:"utteranceId"`
	FromGen     int    `json:"fromGen"`
	ToGen       int    `json:"toGen"`
}

func (s *Server) handleReplay(w http.ResponseWriter, r *http.Request) {
	var req replayReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.UtteranceID == "" {
		req.UtteranceID = "u01"
	}
	if req.FromGen == 0 {
		req.FromGen = 1
	}
	if req.ToGen == 0 {
		req.ToGen = 2
	}
	d, err := s.dataset(r.Context())
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	opsList, err := s.store.ListOperations(r.Context(), req.UtteranceID)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	g1Ops := filterOps(opsList, req.UtteranceID, req.FromGen)
	rep := ops.Replay(d, req.UtteranceID, g1Ops, req.FromGen, req.ToGen)

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.ReplaceConflictsForReplay(r.Context(),
		req.UtteranceID, req.ToGen, rep.Conflicts); err != nil {
		writeErr(w, 500, err)
		return
	}
	_ = s.store.AddRun(r.Context(), "replay",
		"重放 "+req.UtteranceID+": 唯一映射自动恢复, 歧义/未匹配进入冲突列表")
	writeJSON(w, rep)
}

type resolveReq struct {
	UtteranceID string            `json:"utteranceId"`
	ToGen       int               `json:"toGen"`
	Picks       map[string]string `json:"picks"` // conflictID -> 新代候选 ID
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	var req resolveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.ToGen == 0 {
		req.ToGen = 2
	}
	d, err := s.dataset(r.Context())
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	allOps, err := s.store.ListOperations(r.Context(), req.UtteranceID)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	g1Ops := filterOps(allOps, req.UtteranceID, 1)
	rep := ops.Replay(d, req.UtteranceID, g1Ops, 1, req.ToGen)
	final, err := ops.FinalWithResolution(d, req.UtteranceID, rep, req.Picks)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// 以稀疏 gen2 操作记录人工选择(drag), 原始 gen1 操作保持不变。
	for cid, pickedID := range req.Picks {
		var conf *model.Conflict
		for i := range rep.Conflicts {
			if rep.Conflicts[i].ID == cid {
				conf = &rep.Conflicts[i]
				break
			}
		}
		if conf == nil {
			writeErr(w, 400, errBad("未知冲突 "+cid))
			return
		}
		if conf.Rank == 0 {
			// 边界锁定冲突: 记录一条 gen2 lock。
			_ = s.store.AddOperation(r.Context(), model.Operation{
				UtteranceID: req.UtteranceID, Gen: req.ToGen, Type: model.OpLock,
				Frame: conf.Frame,
				Lock:  &model.LockPayload{Frame: conf.Frame, CandidateID: pickedID, Edge: "start"},
				Note:  "冲突解决: 边界锁定重选", ResolvedFrom: cid,
			})
		} else {
			_ = s.store.AddOperation(r.Context(), model.Operation{
				UtteranceID: req.UtteranceID, Gen: req.ToGen, Type: model.OpDrag,
				Frame: conf.Frame, Rank: conf.Rank,
				Drag: &model.DragPayload{FromCandidateID: conf.OldCandidateID, ToCandidateID: pickedID},
				Note: "冲突解决: 歧义候选人工指定", ResolvedFrom: cid,
			})
		}
		_ = s.store.MarkConflictResolved(r.Context(), cid, true)
	}
	_ = s.store.AddRun(r.Context(), "resolve", "人工解决冲突 "+req.UtteranceID)
	issues := ops.ValidateFinal(d, req.UtteranceID, final)
	writeJSON(w, map[string]any{"final": final, "issues": issues})
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	d, err := s.dataset(r.Context())
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	opsBy := map[string][]model.Operation{}
	confs := map[string][]model.Conflict{}
	for _, u := range d.Utterances {
		ol, err := s.store.ListOperations(r.Context(), u.ID)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		opsBy[u.ID] = ol
		cf, err := s.store.ListConflicts(r.Context(), u.ID, -1)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		confs[u.ID] = cf
	}
	res := BuildExportResult(d, flattenOps(opsBy), confs)
	bundle := export.Build(d, opsBy, res)
	_ = s.store.AddRun(r.Context(), "export", "导出可重放修订包")
	writeJSON(w, bundle)
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.store.ListRuns(r.Context(), 300)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, runs)
}

func (s *Server) handleReimport(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.ResetAndImport(r.Context(), fixture.Build()); err != nil {
		writeErr(w, 500, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func flattenOps(m map[string][]model.Operation) []model.Operation {
	var out []model.Operation
	for _, v := range m {
		out = append(out, v...)
	}
	return out
}

type errString string

func (e errString) Error() string { return string(e) }
func errBad(s string) error       { return errString(s) }
