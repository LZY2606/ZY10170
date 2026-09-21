package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"voicebench/internal/model"
	"voicebench/internal/server"
	"voicebench/internal/service"
	"voicebench/internal/store"
)

func newSrv(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := server.SeedStore(st); err != nil {
		t.Fatal(err)
	}
	s := server.New(service.New(st), st)
	return httptest.NewServer(s.Mux), st
}

func TestIndexShowsTitle(t *testing.T) {
	ts, _ := newSrv(t)
	defer ts.Close()
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(res.Body)
	if !strings.Contains(buf.String(), "声纹校形台") {
		t.Fatal("page must show the bench title")
	}
}

func TestStateAndRerunFlow(t *testing.T) {
	ts, _ := newSrv(t)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/api/state?speaker=s1&utter=ai-a")
	if err != nil {
		t.Fatal(err)
	}
	var st service.State
	if err := json.NewDecoder(res.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.Version != "v1" || len(st.Candidates) == 0 {
		t.Fatalf("bad initial state: set=%s cands=%d",
			st.Version, len(st.Candidates))
	}
	var orderIssue bool
	for _, is := range st.Issues {
		if is.Code == model.IssueOrder {
			orderIssue = true
		}
	}
	if !orderIssue {
		t.Fatal("fixture auto track must present an order violation to fix")
	}

	// Add a path op that covers the designed ambiguous frame 10 F2.
	pathBody, _ := json.Marshal(map[string]any{
		"speaker": "s1", "utter": "ai-a", "kind": "path",
		"items": func() []map[string]any {
			var items []map[string]any
			for f := 9; f <= 11; f++ {
				var c model.Candidate
				for _, x := range st.Candidates {
					if x.Frame == f && x.Band == 1 {
						c = x
					}
				}
				items = append(items, map[string]any{
					"frame": f, "band": 1,
					"candidate_id": c.ID, "freq": c.Freq})
			}
			return items
		}(),
	})
	pr, err := http.Post(ts.URL+"/api/operations", "application/json",
		bytes.NewReader(pathBody))
	if err != nil {
		t.Fatal(err)
	}
	pr.Body.Close()

	// Rerun and confirm the conflict is exposed via HTTP.
	body, _ := json.Marshal(map[string]string{"speaker": "s1", "utter": "ai-a"})
	rr, err := http.Post(ts.URL+"/api/rerun", "application/json",
		bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var rerun service.RerunResult
	if err := json.NewDecoder(rr.Body).Decode(&rerun); err != nil {
		t.Fatal(err)
	}
	rr.Body.Close()
	if rerun.ConflictCount == 0 {
		t.Fatal("expected at least the designed ambiguous remap conflict")
	}
}

func TestInvalidMoveRejected(t *testing.T) {
	ts, _ := newSrv(t)
	defer ts.Close()
	// point an F1 item at an F2 candidate -> 422
	var st service.State
	res, _ := http.Get(ts.URL + "/api/state?speaker=s1&utter=ai-a")
	_ = json.NewDecoder(res.Body).Decode(&st)
	res.Body.Close()
	var f12b1 model.Candidate
	for _, c := range st.Candidates {
		if c.Frame == 12 && c.Band == 1 {
			f12b1 = c
		}
	}
	req := map[string]any{
		"speaker": "s1", "utter": "ai-a", "kind": "move",
		"items": []map[string]any{{"frame": 12, "band": 0,
			"candidate_id": f12b1.ID, "freq": f12b1.Freq}},
	}
	raw, _ := json.Marshal(req)
	rr, err := http.Post(ts.URL+"/api/operations", "application/json",
		bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Body.Close()
	if rr.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("cross-band move must be 422, got %d", rr.StatusCode)
	}
}

func TestExportImportHTTP(t *testing.T) {
	ts, _ := newSrv(t)
	defer ts.Close()
	er, err := http.Get(ts.URL + "/api/export")
	if err != nil {
		t.Fatal(err)
	}
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(er.Body)
	er.Body.Close()
	var b model.Bundle
	if err := json.Unmarshal(buf.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if len(b.Candidates) == 0 || len(b.Auto) == 0 {
		t.Fatal("export must include raw candidates and auto points")
	}
	rr, err := http.Post(ts.URL+"/api/import", "application/json",
		bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer rr.Body.Close()
	if rr.StatusCode != 200 {
		t.Fatalf("reimport failed: %d", rr.StatusCode)
	}
}
