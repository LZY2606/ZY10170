// Package server exposes the bench over HTTP with a server-rendered SVG page.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"voicebench/internal/fixture"
	"voicebench/internal/model"
	"voicebench/internal/service"
	"voicebench/internal/store"
)

// Server holds HTTP dependencies.
type Server struct {
	Svc *service.Service
	St  *store.Store
	Mux *http.ServeMux
}

// New wires routes.
func New(svc *service.Service, st *store.Store) *Server {
	s := &Server{Svc: svc, St: st}
	m := http.NewServeMux()
	m.Handle("GET /app.js", staticHandler())
	m.HandleFunc("GET /", s.index)
	m.HandleFunc("GET /api/state", s.getState)
	m.HandleFunc("POST /api/operations", s.addOp)
	m.HandleFunc("POST /api/undo", s.undo)
	m.HandleFunc("POST /api/rerun", s.rerun)
	m.HandleFunc("POST /api/conflicts/resolve", s.resolveConflict)
	m.HandleFunc("GET /api/export", s.exportBundle)
	m.HandleFunc("POST /api/import", s.importBundle)
	m.HandleFunc("POST /api/reset", s.reset)
	m.HandleFunc("GET /api/runlog", s.runLog)
	s.Mux = m
	return s
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	var ve *service.ValidationError
	if errors.As(err, &ve) {
		writeJSON(w, http.StatusUnprocessableEntity,
			map[string]string{"error": err.Error()})
		return
	}
	if errors.Is(err, errNotFound) {
		writeJSON(w, http.StatusNotFound,
			map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusInternalServerError,
		map[string]string{"error": err.Error()})
}

var errNotFound = fmt.Errorf("not found")

type target struct{ speaker, utter string }

func (s *Server) targetFromReq(r *http.Request) (target, error) {
	sp := r.URL.Query().Get("speaker")
	u := r.URL.Query().Get("utter")
	if sp == "" || u == "" {
		spk, _ := s.St.ListSpeakers()
		if len(spk) > 0 {
			sp = spk[0].ID
		}
		if u == "" {
			u = "ai-a"
		}
	}
	if sp == "" {
		return target{}, errNotFound
	}
	return target{speaker: sp, utter: u}, nil
}

func (s *Server) getState(w http.ResponseWriter, r *http.Request) {
	t, err := s.targetFromReq(r)
	if err != nil {
		fail(w, err)
		return
	}
	st, err := s.Svc.State(t.speaker, t.utter)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, st)
}

type opReq struct {
	Speaker string         `json:"speaker"`
	Utter   string         `json:"utter"`
	Kind    string         `json:"kind"`
	Note    string         `json:"note"`
	Author  string         `json:"author"`
	Items   []model.OpItem `json:"items"`
}

func (s *Server) addOp(w http.ResponseWriter, r *http.Request) {
	var req opReq
	if err := decode(r, &req); err != nil {
		fail(w, &service.ValidationError{Err: err})
		return
	}
	if req.Author == "" {
		req.Author = "researcher"
	}
	id, err := s.Svc.AddOp(req.Speaker, req.Utter, model.Operation{
		Kind: req.Kind, Note: req.Note, Author: req.Author, Items: req.Items,
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "status": "saved"})
}

type simpleTarget struct {
	Speaker string `json:"speaker"`
	Utter   string `json:"utter"`
}

func (s *Server) undo(w http.ResponseWriter, r *http.Request) {
	var req simpleTarget
	if err := decode(r, &req); err != nil {
		fail(w, &service.ValidationError{Err: err})
		return
	}
	ok, err := s.Svc.Undo(req.Speaker, req.Utter)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"undone": ok})
}

func (s *Server) rerun(w http.ResponseWriter, r *http.Request) {
	var req simpleTarget
	if err := decode(r, &req); err != nil {
		fail(w, &service.ValidationError{Err: err})
		return
	}
	res, err := s.Svc.Rerun(req.Speaker, req.Utter)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, res)
}

type resolveReq struct {
	Speaker     string `json:"speaker"`
	Utter       string `json:"utter"`
	FromOpID    int64  `json:"from_op_id"`
	Frame       int    `json:"frame"`
	Band        int    `json:"band"`
	CandidateID int64  `json:"candidate_id"`
}

func (s *Server) resolveConflict(w http.ResponseWriter, r *http.Request) {
	var req resolveReq
	if err := decode(r, &req); err != nil {
		fail(w, &service.ValidationError{Err: err})
		return
	}
	err := s.Svc.ResolveConflict(req.Speaker, req.Utter, req.FromOpID,
		req.Frame, req.Band, req.CandidateID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "resolved"})
}

func (s *Server) exportBundle(w http.ResponseWriter, r *http.Request) {
	b, err := s.St.Export()
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition",
		`attachment; filename="voicebench-export-`+
			time.Now().Format("20060102-150405")+`.json"`)
	_ = json.NewEncoder(w).Encode(b)
}

func (s *Server) importBundle(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		fail(w, &service.ValidationError{Err: err})
		return
	}
	var b model.Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		fail(w, &service.ValidationError{Err: fmt.Errorf(
			"导入包不是合法 JSON: %w", err)})
		return
	}
	if b.Schema == "" || len(b.Speakers) == 0 {
		fail(w, &service.ValidationError{Err: fmt.Errorf(
			"导入包缺少 schema 或说话人数据")})
		return
	}
	if err := s.St.ReplaceBundle(&b, "重新导入导出包并复核"); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"status":     "imported",
		"speakers":   len(b.Speakers),
		"candidates": len(b.Candidates),
		"operations": len(b.Operations),
	})
}

func (s *Server) reset(w http.ResponseWriter, r *http.Request) {
	if err := SeedStore(s.St); err != nil {
		fail(w, err)
		return
	}
	_ = s.St.Log("system", "reset", "清空并重新导入固定 fixture")
	writeJSON(w, 200, map[string]string{"status": "reset"})
}

func (s *Server) runLog(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	logs, err := s.St.RunLogTail(limit)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, logs)
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 8<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("请求体解析失败: %w", err)
	}
	return nil
}

// SeedStore builds the fixed fixture and (re)loads it into the database.
func SeedStore(st *store.Store) error {
	data := fixture.Build()
	b := &model.Bundle{
		Schema:     store.SchemaVersion,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Speakers:   data.Speakers,
		Utterances: data.Utterances,
		Frames:     data.Frames,
		Candidates: data.Candidates,
		Auto:       data.Auto,
		Vowels:     data.Vowels,
		Spectrum:   data.Spectrum,
		ActiveSet:  map[string]string{},
	}
	for _, u := range data.Utterances {
		b.ActiveSet[u.SpeakerID+"/"+u.ID] = fixture.SetV1
	}
	return st.ReplaceBundle(b, "导入固定 fixture")
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	b, err := staticFS.ReadFile("web_assets/index.html")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}

// ListenAddr normalises a listen string.
func ListenAddr(v string) string {
	if strings.TrimSpace(v) == "" {
		return "127.0.0.1:5510"
	}
	return v
}
