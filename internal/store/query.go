package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"voiceprintbench/internal/model"
)

// LoadDataset 读出固定数据(候选、自动轨迹、元音、说话人、发音)。
func (s *Store) LoadDataset(ctx context.Context) (*model.Dataset, error) {
	d := &model.Dataset{
		Gens:   map[string][]model.GenCandidates{},
		Autos:  map[string][]model.AutoTrack{},
		Vowels: map[string][]model.VowelRange{},
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,name FROM speakers ORDER BY id")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var sp model.Speaker
		if err := rows.Scan(&sp.ID, &sp.Name); err != nil {
			rows.Close()
			return nil, err
		}
		d.Speakers = append(d.Speakers, sp)
	}
	rows.Close()

	rows, err = s.db.QueryContext(ctx,
		"SELECT id,speaker_id,label,frame_rate,frame_ms,num_frames FROM utterances ORDER BY id")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u model.Utterance
		if err := rows.Scan(&u.ID, &u.SpeakerID, &u.Label, &u.FrameRate, &u.FrameMs, &u.NumFrames); err != nil {
			rows.Close()
			return nil, err
		}
		d.Utterances = append(d.Utterances, u)
	}
	rows.Close()

	crows, err := s.db.QueryContext(ctx,
		`SELECT utterance_id,gen,frame,id,frequency,energy,confidence
		 FROM candidates ORDER BY utterance_id,gen,frame,frequency`)
	if err != nil {
		return nil, err
	}
	candIndex := map[string]int{}
	for crows.Next() {
		var uid string
		var gc model.GenCandidates
		var c model.Candidate
		if err := crows.Scan(&uid, &gc.Gen, &gc.Frame, &c.ID, &c.Frequency, &c.Energy, &c.Confidence); err != nil {
			crows.Close()
			return nil, err
		}
		key := uid
		idx, ok := candIndex[key+"#"+itoa(gc.Gen)+"#"+itoa(gc.Frame)]
		if !ok {
			d.Gens[uid] = append(d.Gens[uid], model.GenCandidates{Gen: gc.Gen, Frame: gc.Frame})
			idx = len(d.Gens[uid]) - 1
			candIndex[key+"#"+itoa(gc.Gen)+"#"+itoa(gc.Frame)] = idx
		}
		d.Gens[uid][idx].Candidates = append(d.Gens[uid][idx].Candidates, c)
	}
	crows.Close()

	arows, err := s.db.QueryContext(ctx,
		`SELECT utterance_id,gen,frame,rank,candidate_id,frequency,confidence,visible,missing
		 FROM auto_tracks ORDER BY utterance_id,gen,frame,rank`)
	if err != nil {
		return nil, err
	}
	for arows.Next() {
		var uid string
		var vis, miss int
		var a model.AutoTrack
		if err := arows.Scan(&uid, &a.Gen, &a.Frame, &a.Rank, &a.CandidateID,
			&a.Frequency, &a.Confidence, &vis, &miss); err != nil {
			arows.Close()
			return nil, err
		}
		a.Visible = vis == 1
		a.Missing = miss == 1
		d.Autos[uid] = append(d.Autos[uid], a)
	}
	arows.Close()

	vrows, err := s.db.QueryContext(ctx,
		"SELECT id,utterance_id,label,start_frame,end_frame FROM vowels ORDER BY utterance_id,start_frame")
	if err != nil {
		return nil, err
	}
	for vrows.Next() {
		var uid string
		var v model.VowelRange
		if err := vrows.Scan(&v.ID, &uid, &v.Label, &v.Start, &v.End); err != nil {
			vrows.Close()
			return nil, err
		}
		d.Vowels[uid] = append(d.Vowels[uid], v)
	}
	vrows.Close()

	// 让每段候选按 (gen,frame) 稳定排序。
	for uid := range d.Gens {
		gcs := d.Gens[uid]
		sort.SliceStable(gcs, func(i, j int) bool {
			if gcs[i].Gen != gcs[j].Gen {
				return gcs[i].Gen < gcs[j].Gen
			}
			return gcs[i].Frame < gcs[j].Frame
		})
		d.Gens[uid] = gcs
	}
	return d, nil
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// CandidatesFrame 取某发音、某代、某帧的候选(缺帧返回空切片)。
func (s *Store) CandidatesFrame(ctx context.Context, uid string, gen, frame int) ([]model.Candidate, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,frequency,energy,confidence FROM candidates
		 WHERE utterance_id=? AND gen=? AND frame=? ORDER BY frequency`, uid, gen, frame)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Candidate
	for rows.Next() {
		var c model.Candidate
		if err := rows.Scan(&c.ID, &c.Frequency, &c.Energy, &c.Confidence); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// ListOperations 读取某发音的全部稀疏操作(按创建时间)。
func (s *Store) ListOperations(ctx context.Context, uid string) ([]model.Operation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,utterance_id,gen,frame,rank,type,payload,note,created_at,resolved_from
		 FROM operations WHERE utterance_id=? ORDER BY created_at,id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Operation
	for rows.Next() {
		op, err := scanOp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, nil
}

func scanOp(rows *sql.Rows) (model.Operation, error) {
	var op model.Operation
	var payload, note, resolved sql.NullString
	if err := rows.Scan(&op.ID, &op.UtteranceID, &op.Gen, &op.Frame, &op.Rank,
		&op.Type, &payload, &note, &op.CreatedAt, &resolved); err != nil {
		return op, err
	}
	if err := json.Unmarshal([]byte(payload.String), &op); err != nil {
		return op, fmt.Errorf("decode op %s: %w", op.ID, err)
	}
	// json.Unmarshal 已填充 op 的 id 以外字段; 再回填标量列。
	op.Note = note.String
	op.ResolvedFrom = resolved.String
	return op, nil
}

// AddOperation 保存一条稀疏操作。
func (s *Store) AddOperation(ctx context.Context, op model.Operation) error {
	if op.ID == "" {
		op.ID = fmt.Sprintf("op-%d", time.Now().UnixNano())
	}
	if op.CreatedAt == "" {
		op.CreatedAt = nowUTC()
	}
	payload, err := json.Marshal(op)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO operations(id,utterance_id,gen,frame,rank,type,payload,note,created_at,resolved_from)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		op.ID, op.UtteranceID, op.Gen, op.Frame, op.Rank, op.Type, string(payload),
		op.Note, op.CreatedAt, op.ResolvedFrom)
	return err
}

// DeleteOperation 删除一条操作(撤销)。
func (s *Store) DeleteOperation(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx, "DELETE FROM operations WHERE id=?", id)
	return err
}

// ClearOperations 清空某发音的操作(重新修订)。
func (s *Store) ClearOperations(ctx context.Context, uid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx, "DELETE FROM operations WHERE utterance_id=?", uid)
	return err
}

// ListConflicts 读取冲突(可按是否已解决过滤; resolved<0 表示全部)。
func (s *Store) ListConflicts(ctx context.Context, uid string, resolved int) ([]model.Conflict, error) {
	q := `SELECT id,utterance_id,from_op_id,from_gen,to_gen,type,frame,rank,
	 old_candidate_id,old_frequency,candidate_ids,resolved FROM conflicts WHERE utterance_id=?`
	args := []any{uid}
	if resolved >= 0 {
		q += " AND resolved=?"
		args = append(args, resolved)
	}
	q += " ORDER BY frame,id"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Conflict
	for rows.Next() {
		var c model.Conflict
		var idsJSON string
		var res int
		if err := rows.Scan(&c.ID, &c.UtteranceID, &c.FromOpID, &c.FromGen, &c.ToGen,
			&c.Type, &c.Frame, &c.Rank, &c.OldCandidateID, &c.OldFrequency, &idsJSON, &res); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(idsJSON), &c.CandidateIDs); err != nil {
			return nil, err
		}
		c.Resolved = res == 1
		out = append(out, c)
	}
	return out, nil
}

// ReplaceConflictsForReplay 重放后以新结果替换该发音“指向 toGen”的冲突集合。
func (s *Store) ReplaceConflictsForReplay(ctx context.Context, uid string, toGen int, cs []model.Conflict) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM conflicts WHERE utterance_id=? AND to_gen=?", uid, toGen); err != nil {
		return err
	}
	for _, c := range cs {
		ids, _ := json.Marshal(c.CandidateIDs)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO conflicts(id,utterance_id,from_op_id,from_gen,to_gen,type,frame,rank,
			 old_candidate_id,old_frequency,candidate_ids,resolved)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			c.ID, uid, c.FromOpID, c.FromGen, c.ToGen, c.Type, c.Frame, c.Rank,
			c.OldCandidateID, c.OldFrequency, string(ids), boolInt(c.Resolved)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MarkConflictResolved 标记冲突已解决。
func (s *Store) MarkConflictResolved(ctx context.Context, id string, resolved bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx, "UPDATE conflicts SET resolved=? WHERE id=?",
		boolInt(resolved), id)
	return err
}

// AddRun 记录一次运行动作。
func (s *Store) AddRun(ctx context.Context, kind, detail string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO run_records(id,kind,detail,created_at) VALUES(?,?,?,?)",
		fmt.Sprintf("run-%d", time.Now().UnixNano()), kind, detail, nowUTC())
	return err
}

// ListRuns 读取运行记录。
func (s *Store) ListRuns(ctx context.Context, limit int) ([]model.RunRecord, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT id,kind,detail,created_at FROM run_records ORDER BY created_at DESC,id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.RunRecord
	for rows.Next() {
		var r model.RunRecord
		if err := rows.Scan(&r.ID, &r.Kind, &r.Detail, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// Meta 读取 meta 值。
func (s *Store) Meta(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key=?", key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}
