CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS speakers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS utterances (
  id TEXT PRIMARY KEY,
  speaker_id TEXT NOT NULL,
  label TEXT NOT NULL,
  frame_rate REAL NOT NULL,
  frame_ms INTEGER NOT NULL,
  num_frames INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS candidates (
  id TEXT PRIMARY KEY,
  utterance_id TEXT NOT NULL,
  gen INTEGER NOT NULL,
  frame INTEGER NOT NULL,
  frequency REAL NOT NULL,
  energy REAL NOT NULL,
  confidence REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_cand_lookup ON candidates(utterance_id, gen, frame);

CREATE TABLE IF NOT EXISTS auto_tracks (
  utterance_id TEXT NOT NULL,
  gen INTEGER NOT NULL,
  frame INTEGER NOT NULL,
  rank INTEGER NOT NULL,
  candidate_id TEXT NOT NULL,
  frequency REAL NOT NULL,
  confidence REAL NOT NULL,
  visible INTEGER NOT NULL,
  missing INTEGER NOT NULL,
  PRIMARY KEY (utterance_id, gen, frame, rank)
);

CREATE TABLE IF NOT EXISTS vowels (
  id TEXT NOT NULL,
  utterance_id TEXT NOT NULL,
  label TEXT NOT NULL,
  start_frame INTEGER NOT NULL,
  end_frame INTEGER NOT NULL,
  PRIMARY KEY (utterance_id, id)
);

CREATE TABLE IF NOT EXISTS operations (
  id TEXT PRIMARY KEY,
  utterance_id TEXT NOT NULL,
  gen INTEGER NOT NULL,
  frame INTEGER NOT NULL,
  rank INTEGER NOT NULL,
  type TEXT NOT NULL,
  payload TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  resolved_from TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_ops_utt ON operations(utterance_id);

CREATE TABLE IF NOT EXISTS conflicts (
  id TEXT PRIMARY KEY,
  utterance_id TEXT NOT NULL,
  from_op_id TEXT NOT NULL,
  from_gen INTEGER NOT NULL,
  to_gen INTEGER NOT NULL,
  type TEXT NOT NULL,
  frame INTEGER NOT NULL,
  rank INTEGER NOT NULL,
  old_candidate_id TEXT NOT NULL,
  old_frequency REAL NOT NULL,
  candidate_ids TEXT NOT NULL,
  resolved INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_conf_utt ON conflicts(utterance_id, resolved);

CREATE TABLE IF NOT EXISTS run_records (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  detail TEXT NOT NULL,
  created_at TEXT NOT NULL
);
