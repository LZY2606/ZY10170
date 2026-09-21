// Package httpapi 提供“声纹校形台”本地 HTTP 服务与 JSON API。
package httpapi

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"sync"

	"voiceprintbench/internal/model"
	"voiceprintbench/internal/store"
)

//go:embed web/*
var webFS embed.FS

// Server 持有存储并序列化写操作。
type Server struct {
	store *store.Store
	mu    sync.Mutex
}

// New 创建服务。
func New(st *store.Store) *Server {
	return &Server{store: st}
}

// Handler 注册全部路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /api/operations", s.handleAddOp)
	mux.HandleFunc("DELETE /api/operations/{id}", s.handleDeleteOp)
	mux.HandleFunc("POST /api/replay", s.handleReplay)
	mux.HandleFunc("POST /api/conflicts/resolve", s.handleResolve)
	mux.HandleFunc("GET /api/export", s.handleExport)
	mux.HandleFunc("GET /api/runs", s.handleRuns)
	mux.HandleFunc("POST /api/admin/reimport", s.handleReimport)

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", http.FileServer(http.FS(static)))
	return logging(mux)
}

func logging(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func (s *Server) dataset(ctx context.Context) (*model.Dataset, error) {
	return s.store.LoadDataset(ctx)
}
