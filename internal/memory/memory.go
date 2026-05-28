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

type Document struct {
	ID        int64
	Path      string
	Hash      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Text      string
}

type Chunk struct {
	ID          int64
	DocumentID  int64
	Path        string
	OffsetStart int
	OffsetEnd   int
	Text        string
	EmbeddingID string
	UpdatedAt   time.Time
}

type Node struct {
	ID      int64
	Type    string
	Label   string
	Payload string
}

type Edge struct {
	ID       int64
	FromNode int64
	ToNode   int64
	Relation string
	Weight   float64
}

type TrajectoryRecord struct {
	SearchNodeID       int     `json:"search_node_id"`
	ParentSearchNodeID int     `json:"parent_search_node_id,omitempty"`
	Action             string  `json:"action,omitempty"`
	Source             string  `json:"source,omitempty"`
	Depth              int     `json:"depth"`
	Reward             float64 `json:"reward"`
	Score              float64 `json:"score"`
	Status             string  `json:"status,omitempty"`
	VerifierStatus     string  `json:"verifier_status,omitempty"`
	Error              string  `json:"error,omitempty"`
	Verified           bool    `json:"verified"`
	Completed          bool    `json:"completed"`
	Selected           bool    `json:"selected"`
}

type InspectStats struct {
	Documents   int
	Chunks      int
	Nodes       int
	Edges       int
	LatestPaths []string
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

func (s *Store) EvidenceByVerifier(ctx context.Context, episodeID int64, verifier string) ([]Evidence, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, episode_id, verifier, status, score, stdout, stderr, artifacts, payload, created_at
FROM evidence
WHERE episode_id = ? AND verifier = ?
ORDER BY id
`, episodeID, verifier)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Evidence
	for rows.Next() {
		var ev Evidence
		var artifactsJSON string
		var createdAt string
		if err := rows.Scan(&ev.ID, &ev.EpisodeID, &ev.Verifier, &ev.Status, &ev.Score, &ev.Stdout, &ev.Stderr, &artifactsJSON, &ev.Payload, &createdAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(artifactsJSON), &ev.Artifacts)
		if ts, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
			ev.Timestamp = ts
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) UpsertDocument(ctx context.Context, path string, hash string, text string) (Document, bool, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Document{}, false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var doc Document
	var createdAt string
	var updatedAt string
	err = s.db.QueryRowContext(ctx, `
SELECT id, path, hash, created_at, updated_at, text
FROM documents
WHERE path = ?
ORDER BY id DESC
LIMIT 1
`, abs).Scan(&doc.ID, &doc.Path, &doc.Hash, &createdAt, &updatedAt, &doc.Text)
	if err == nil {
		parseTimes(&doc, createdAt, updatedAt)
		if doc.Hash == hash {
			return doc, false, nil
		}
		_, err = s.db.ExecContext(ctx, `
UPDATE documents SET hash = ?, updated_at = ?, text = ? WHERE id = ?
`, hash, now, text, doc.ID)
		if err != nil {
			return Document{}, false, err
		}
		doc.Hash = hash
		doc.Text = text
		doc.UpdatedAt = time.Now().UTC()
		return doc, true, nil
	}
	if err != sql.ErrNoRows {
		return Document{}, false, err
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO documents(path, hash, created_at, updated_at, text)
VALUES (?, ?, ?, ?, ?)
`, abs, hash, now, now, text)
	if err != nil {
		return Document{}, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Document{}, false, err
	}
	ts, _ := time.Parse(time.RFC3339Nano, now)
	return Document{ID: id, Path: abs, Hash: hash, CreatedAt: ts, UpdatedAt: ts, Text: text}, true, nil
}

func (s *Store) ReplaceChunks(ctx context.Context, documentID int64, chunks []Chunk) ([]Chunk, error) {
	if err := s.clearGraphForDocument(ctx, documentID); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = ?`, documentID); err != nil {
		return nil, err
	}
	out := make([]Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		res, err := s.db.ExecContext(ctx, `
INSERT INTO chunks(document_id, offset_start, offset_end, text, embedding_id)
VALUES (?, ?, ?, ?, ?)
`, documentID, chunk.OffsetStart, chunk.OffsetEnd, chunk.Text, chunk.EmbeddingID)
		if err != nil {
			return nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		chunk.ID = id
		chunk.DocumentID = documentID
		out = append(out, chunk)
	}
	return out, nil
}

func (s *Store) EnsureNode(ctx context.Context, typ string, label string, payload string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM nodes WHERE type = ? AND label = ? ORDER BY id LIMIT 1`, typ, label).Scan(&id)
	if err == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE nodes SET payload = ? WHERE id = ?`, payload, id)
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO nodes(type, label, payload) VALUES (?, ?, ?)`, typ, label, payload)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) EnsureEdge(ctx context.Context, fromNode int64, toNode int64, relation string, weight float64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
SELECT id FROM edges WHERE from_node = ? AND to_node = ? AND relation = ? ORDER BY id LIMIT 1
`, fromNode, toNode, relation).Scan(&id)
	if err == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE edges SET weight = ? WHERE id = ?`, weight, id)
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO edges(from_node, to_node, relation, weight) VALUES (?, ?, ?, ?)`, fromNode, toNode, relation, weight)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) RecordTrajectory(ctx context.Context, episodeID int64, records []TrajectoryRecord) error {
	nodeIDs := make(map[int]int64, len(records))
	for _, record := range records {
		payload, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshal trajectory state: %w", err)
		}
		label := fmt.Sprintf("episode:%d:trajectory:%d", episodeID, record.SearchNodeID)
		nodeID, err := s.EnsureNode(ctx, "trajectory_state", label, string(payload))
		if err != nil {
			return err
		}
		nodeIDs[record.SearchNodeID] = nodeID
	}
	for _, record := range records {
		if record.ParentSearchNodeID == 0 {
			continue
		}
		parentID, ok := nodeIDs[record.ParentSearchNodeID]
		if !ok {
			continue
		}
		nodeID, ok := nodeIDs[record.SearchNodeID]
		if !ok {
			continue
		}
		if _, err := s.EnsureEdge(ctx, parentID, nodeID, "expands_to", 1); err != nil {
			return err
		}
		if record.Selected {
			if _, err := s.EnsureEdge(ctx, parentID, nodeID, "selected", 1); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) RecordSelectorExample(ctx context.Context, episodeID int64, labelSuffix string, payload string) (int64, error) {
	if payload == "" {
		payload = "{}"
	}
	label := fmt.Sprintf("episode:%d:selector_example:%s", episodeID, labelSuffix)
	return s.EnsureNode(ctx, "selector_example", label, payload)
}

func (s *Store) Chunks(ctx context.Context) ([]Chunk, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT c.id, c.document_id, d.path, c.offset_start, c.offset_end, c.text, COALESCE(c.embedding_id, ''), d.updated_at
FROM chunks c
JOIN documents d ON d.id = c.document_id
ORDER BY c.id
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chunk
	for rows.Next() {
		var chunk Chunk
		var updatedAt string
		if err := rows.Scan(&chunk.ID, &chunk.DocumentID, &chunk.Path, &chunk.OffsetStart, &chunk.OffsetEnd, &chunk.Text, &chunk.EmbeddingID, &updatedAt); err != nil {
			return nil, err
		}
		if ts, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
			chunk.UpdatedAt = ts
		}
		out = append(out, chunk)
	}
	return out, rows.Err()
}

func (s *Store) GraphEdges(ctx context.Context) ([]Edge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, from_node, to_node, relation, weight FROM edges ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Edge
	for rows.Next() {
		var edge Edge
		if err := rows.Scan(&edge.ID, &edge.FromNode, &edge.ToNode, &edge.Relation, &edge.Weight); err != nil {
			return nil, err
		}
		out = append(out, edge)
	}
	return out, rows.Err()
}

func (s *Store) NodeCountByType(ctx context.Context, typ string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE type = ?`, typ).Scan(&count)
	return count, err
}

func (s *Store) Inspect(ctx context.Context, latestLimit int) (InspectStats, error) {
	stats := InspectStats{}
	for _, q := range []struct {
		dst *int
		sql string
	}{
		{&stats.Documents, `SELECT COUNT(*) FROM documents`},
		{&stats.Chunks, `SELECT COUNT(*) FROM chunks`},
		{&stats.Nodes, `SELECT COUNT(*) FROM nodes`},
		{&stats.Edges, `SELECT COUNT(*) FROM edges`},
	} {
		if err := s.db.QueryRowContext(ctx, q.sql).Scan(q.dst); err != nil {
			return InspectStats{}, err
		}
	}
	if latestLimit <= 0 {
		latestLimit = 5
	}
	rows, err := s.db.QueryContext(ctx, `SELECT path FROM documents ORDER BY updated_at DESC, id DESC LIMIT ?`, latestLimit)
	if err != nil {
		return InspectStats{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return InspectStats{}, err
		}
		stats.LatestPaths = append(stats.LatestPaths, path)
	}
	return stats, rows.Err()
}

func (s *Store) clearGraphForDocument(ctx context.Context, documentID int64) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM chunks WHERE document_id = ?`, documentID)
	if err != nil {
		return err
	}
	var labels []string
	for rows.Next() {
		var chunkID int64
		if err := rows.Scan(&chunkID); err != nil {
			rows.Close()
			return err
		}
		labels = append(labels, fmt.Sprintf("chunk:%d", chunkID))
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, label := range labels {
		var nodeID int64
		err := s.db.QueryRowContext(ctx, `SELECT id FROM nodes WHERE label = ?`, label).Scan(&nodeID)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM edges WHERE from_node = ? OR to_node = ?`, nodeID, nodeID); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM nodes WHERE id = ?`, nodeID); err != nil {
			return err
		}
	}
	return nil
}

func parseTimes(doc *Document, createdAt string, updatedAt string) {
	if ts, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		doc.CreatedAt = ts
	}
	if ts, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
		doc.UpdatedAt = ts
	}
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
