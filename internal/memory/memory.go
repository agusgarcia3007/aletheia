package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const DefaultDBPath = "./data/memory.sqlite"

type Store struct {
	db *sql.DB
}

type Evidence struct {
	ID        int64
	EpisodeID int64
	Verifier  string
	Status    string
	Score     float64
	Stdout    string
	Stderr    string
	Artifacts []string
	Payload   string
	Timestamp time.Time
}

func Open(path string) (*Store, error) {
	if path == "" {
		path = DefaultDBPath
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

func (s *Store) CreateEpisode(ctx context.Context, goal string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
INSERT INTO episodes(goal, result, reward, created_at)
VALUES (?, '', 0, ?)
`, goal, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateEpisodeResult(ctx context.Context, episodeID int64, result string, reward float64) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE episodes SET result = ?, reward = ? WHERE id = ?
`, result, reward, episodeID)
	return err
}

func (s *Store) RecordEvidence(ctx context.Context, ev Evidence) (int64, error) {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	if ev.Payload == "" {
		ev.Payload = "{}"
	}
	artifacts, err := json.Marshal(ev.Artifacts)
	if err != nil {
		return 0, fmt.Errorf("marshal evidence artifacts: %w", err)
	}

	res, err := s.db.ExecContext(ctx, `
INSERT INTO evidence(episode_id, verifier, status, score, stdout, stderr, artifacts, payload, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, ev.EpisodeID, ev.Verifier, ev.Status, ev.Score, ev.Stdout, ev.Stderr, string(artifacts), ev.Payload, ev.Timestamp.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) EvidenceCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM evidence`).Scan(&count)
	return count, err
}

const schema = `
CREATE TABLE IF NOT EXISTS documents (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	path TEXT NOT NULL,
	hash TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	text TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS chunks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	document_id INTEGER NOT NULL REFERENCES documents(id),
	offset_start INTEGER NOT NULL,
	offset_end INTEGER NOT NULL,
	text TEXT NOT NULL,
	embedding_id TEXT
);

CREATE TABLE IF NOT EXISTS episodes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	goal TEXT NOT NULL,
	result TEXT NOT NULL DEFAULT '',
	reward REAL NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS evidence (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	episode_id INTEGER NOT NULL REFERENCES episodes(id),
	verifier TEXT NOT NULL,
	status TEXT NOT NULL,
	score REAL NOT NULL,
	stdout TEXT NOT NULL DEFAULT '',
	stderr TEXT NOT NULL DEFAULT '',
	artifacts TEXT NOT NULL DEFAULT '[]',
	payload TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS skills (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	trigger TEXT NOT NULL,
	action_sequence TEXT NOT NULL,
	success_rate REAL NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS nodes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	type TEXT NOT NULL,
	label TEXT NOT NULL,
	payload TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS edges (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	from_node INTEGER NOT NULL REFERENCES nodes(id),
	to_node INTEGER NOT NULL REFERENCES nodes(id),
	relation TEXT NOT NULL,
	weight REAL NOT NULL DEFAULT 1
);
`
