// Package store 负责 SQLite 持久化: 固定数据、稀疏操作、冲突、运行记录。
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"sync"
	"time"

	"voiceprintbench/internal/model"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Store 是 SQLite 存储。
type Store struct {
	mu sync.Mutex
	db *sql.DB
}

// Open 打开(必要时创建)数据库并初始化表结构。
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// ImportDataset 用固定数据整体替换库内容(在事务内先清空再导入)。
func (s *Store) ImportDataset(ctx context.Context, d *model.Dataset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{
		"run_records", "conflicts", "operations", "vowels", "auto_tracks",
		"candidates", "utterances", "speakers", "meta",
	} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+t); err != nil {
			return err
		}
	}
	for _, sp := range d.Speakers {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO speakers(id,name) VALUES(?,?)", sp.ID, sp.Name); err != nil {
			return err
		}
	}
	for _, u := range d.Utterances {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO utterances(id,speaker_id,label,frame_rate,frame_ms,num_frames)
			 VALUES(?,?,?,?,?,?)`,
			u.ID, u.SpeakerID, u.Label, u.FrameRate, u.FrameMs, u.NumFrames); err != nil {
			return err
		}
	}
	for uid, gcs := range d.Gens {
		for _, gc := range gcs {
			for _, c := range gc.Candidates {
				if _, err := tx.ExecContext(ctx,
					`INSERT INTO candidates(id,utterance_id,gen,frame,frequency,energy,confidence)
					 VALUES(?,?,?,?,?,?,?)`,
					c.ID, uid, gc.Gen, gc.Frame, c.Frequency, c.Energy, c.Confidence); err != nil {
					return err
				}
			}
		}
	}
	for uid, autos := range d.Autos {
		for _, a := range autos {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO auto_tracks(utterance_id,gen,frame,rank,candidate_id,frequency,confidence,visible,missing)
				 VALUES(?,?,?,?,?,?,?,?,?)`,
				uid, a.Gen, a.Frame, a.Rank, a.CandidateID, a.Frequency, a.Confidence,
				boolInt(a.Visible), boolInt(a.Missing)); err != nil {
				return err
			}
		}
	}
	for uid, vs := range d.Vowels {
		for _, v := range vs {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO vowels(id,utterance_id,label,start_frame,end_frame) VALUES(?,?,?,?,?)",
				v.ID, uid, v.Label, v.Start, v.End); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO meta(key,value) VALUES('imported_at',?)", nowUTC()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO run_records(id,kind,detail,created_at) VALUES(?,?,?,?)",
		fmt.Sprintf("run-%d", time.Now().UnixNano()), "import", "导入固定 fixture", nowUTC()); err != nil {
		return err
	}
	return tx.Commit()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ResetAndImport 清空数据库并重新导入固定数据(复核入口)。
func (s *Store) ResetAndImport(ctx context.Context, d *model.Dataset) error {
	if err := s.ImportDataset(ctx, d); err != nil {
		return err
	}
	return s.AddRun(ctx, "reset", "清空数据库并重新导入固定 fixture")
}
