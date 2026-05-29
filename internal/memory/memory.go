package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	skillutil "aletheia/internal/skills"

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

type NodeTypeCount struct {
	Type  string
	Count int
}

type TrajectoryRecord struct {
	SearchNodeID       int     `json:"search_node_id"`
	ParentSearchNodeID int     `json:"parent_search_node_id,omitempty"`
	Action             string  `json:"action,omitempty"`
	Source             string  `json:"source,omitempty"`
	Depth              int     `json:"depth"`
	Visits             int     `json:"visits,omitempty"`
	Prior              float64 `json:"prior,omitempty"`
	Value              float64 `json:"value,omitempty"`
	Reward             float64 `json:"reward"`
	Score              float64 `json:"score"`
	Status             string  `json:"status,omitempty"`
	VerifierStatus     string  `json:"verifier_status,omitempty"`
	Error              string  `json:"error,omitempty"`
	Verified           bool    `json:"verified"`
	Completed          bool    `json:"completed"`
	Selected           bool    `json:"selected"`
}

type Skill struct {
	ID             int64
	Name           string
	Trigger        string
	ActionSequence []string
	SuccessRate    float64
	CreatedAt      time.Time
}

type ResearchJob struct {
	ID          string    `json:"id"`
	Query       string    `json:"query"`
	Status      string    `json:"status"`
	Mode        string    `json:"mode"`
	CreatedAt   time.Time `json:"created_at"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Error       string    `json:"error,omitempty"`
	MaxSources  int       `json:"max_sources"`
	Answer      string    `json:"answer,omitempty"`
	Confidence  float64   `json:"confidence"`
}

type WebSource struct {
	ID          string    `json:"id"`
	JobID       string    `json:"job_id"`
	URL         string    `json:"url"`
	FinalURL    string    `json:"final_url"`
	Title       string    `json:"title"`
	Snippet     string    `json:"snippet"`
	SourceRank  float64   `json:"source_rank"`
	FetchedAt   time.Time `json:"fetched_at"`
	Status      string    `json:"status"`
	ContentHash string    `json:"content_hash"`
	TrustScore  float64   `json:"trust_score"`
	ByteSize    int64     `json:"byte_size"`
	ContentType string    `json:"content_type"`
}

type WebClaim struct {
	ID         string    `json:"id"`
	SourceID   string    `json:"source_id"`
	Claim      string    `json:"claim"`
	Confidence float64   `json:"confidence"`
	CreatedAt  time.Time `json:"created_at"`
}

type InspectStats struct {
	Documents   int
	Chunks      int
	Skills      int
	Nodes       int
	Edges       int
	NodeTypes   []NodeTypeCount
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
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
		_ = db.Close()
		return nil, err
	}
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

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("memory store is not open")
	}
	return s.db.PingContext(ctx)
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

func (s *Store) RecordCausalNode(ctx context.Context, episodeID int64, typ string, labelSuffix string, payload any) (int64, error) {
	if typ == "" {
		return 0, fmt.Errorf("causal node type is required")
	}
	if labelSuffix == "" {
		labelSuffix = "state"
	}
	if payload == nil {
		payload = map[string]any{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal causal node payload: %w", err)
	}
	label := fmt.Sprintf("episode:%d:%s:%s", episodeID, typ, labelSuffix)
	return s.EnsureNode(ctx, typ, label, string(raw))
}

func (s *Store) UpsertSkill(ctx context.Context, skill Skill) (Skill, error) {
	if skill.Name == "" {
		return Skill{}, fmt.Errorf("skill name is required")
	}
	if skill.Trigger == "" {
		return Skill{}, fmt.Errorf("skill trigger is required")
	}
	if skill.SuccessRate == 0 {
		skill.SuccessRate = 1
	}
	sequence, err := skillutil.MarshalActionSequence(skill.ActionSequence)
	if err != nil {
		return Skill{}, err
	}

	var id int64
	var createdAt string
	err = s.db.QueryRowContext(ctx, `
SELECT id, created_at FROM skills WHERE name = ? AND trigger = ? ORDER BY id LIMIT 1
`, skill.Name, skill.Trigger).Scan(&id, &createdAt)
	if err == nil {
		if _, err := s.db.ExecContext(ctx, `
UPDATE skills SET action_sequence = ?, success_rate = ? WHERE id = ?
`, sequence, skill.SuccessRate, id); err != nil {
			return Skill{}, err
		}
		skill.ID = id
		if ts, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
			skill.CreatedAt = ts
		}
		return skill, nil
	}
	if err != sql.ErrNoRows {
		return Skill{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
INSERT INTO skills(name, trigger, action_sequence, success_rate, created_at)
VALUES (?, ?, ?, ?, ?)
`, skill.Name, skill.Trigger, sequence, skill.SuccessRate, now)
	if err != nil {
		return Skill{}, err
	}
	id, err = res.LastInsertId()
	if err != nil {
		return Skill{}, err
	}
	skill.ID = id
	if ts, err := time.Parse(time.RFC3339Nano, now); err == nil {
		skill.CreatedAt = ts
	}
	return skill, nil
}

func (s *Store) BestSkillByTrigger(ctx context.Context, trigger string) (Skill, bool, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, trigger, action_sequence, success_rate, created_at
FROM skills
WHERE trigger = ?
ORDER BY success_rate DESC, id ASC
LIMIT 1
`, trigger)
	skill, err := scanSkill(row)
	if err == sql.ErrNoRows {
		return Skill{}, false, nil
	}
	if err != nil {
		return Skill{}, false, err
	}
	return skill, true, nil
}

func (s *Store) ListSkills(ctx context.Context) ([]Skill, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, trigger, action_sequence, success_rate, created_at
FROM skills
ORDER BY success_rate DESC, id ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Skill
	for rows.Next() {
		skill, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, skill)
	}
	return out, rows.Err()
}

func (s *Store) UpdateSkillSuccessRate(ctx context.Context, skillID int64, successRate float64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE skills SET success_rate = ? WHERE id = ?`, successRate, skillID)
	return err
}

func (s *Store) CreateResearchJob(ctx context.Context, job ResearchJob) (ResearchJob, error) {
	if job.ID == "" {
		job.ID = fmt.Sprintf("research_%d", time.Now().UTC().UnixNano())
	}
	if job.Query == "" {
		return ResearchJob{}, fmt.Errorf("research query is required")
	}
	if job.Status == "" {
		job.Status = "queued"
	}
	if job.Mode == "" {
		job.Mode = "background"
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO research_jobs(id, query, status, mode, created_at, started_at, completed_at, error, max_sources, answer, confidence)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, job.ID, job.Query, job.Status, job.Mode, formatTime(job.CreatedAt), nullableTime(job.StartedAt), nullableTime(job.CompletedAt), job.Error, job.MaxSources, job.Answer, job.Confidence)
	if err != nil {
		return ResearchJob{}, err
	}
	return job, nil
}

func (s *Store) ClaimQueuedResearchJob(ctx context.Context) (ResearchJob, bool, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, query, status, mode, created_at, started_at, completed_at, error, max_sources, answer, confidence
FROM research_jobs
WHERE status = 'queued'
ORDER BY created_at ASC
LIMIT 1
`)
	job, err := scanResearchJob(row)
	if err == sql.ErrNoRows {
		return ResearchJob{}, false, nil
	}
	if err != nil {
		return ResearchJob{}, false, err
	}
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `UPDATE research_jobs SET status = 'running', started_at = ? WHERE id = ? AND status = 'queued'`, formatTime(now), job.ID)
	if err != nil {
		return ResearchJob{}, false, err
	}
	job.Status = "running"
	job.StartedAt = now
	return job, true, nil
}

func (s *Store) UpdateResearchJob(ctx context.Context, job ResearchJob) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE research_jobs
SET status = ?, started_at = ?, completed_at = ?, error = ?, answer = ?, confidence = ?
WHERE id = ?
`, job.Status, nullableTime(job.StartedAt), nullableTime(job.CompletedAt), job.Error, job.Answer, job.Confidence, job.ID)
	return err
}

func (s *Store) ResearchJob(ctx context.Context, id string) (ResearchJob, bool, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, query, status, mode, created_at, started_at, completed_at, error, max_sources, answer, confidence
FROM research_jobs
WHERE id = ?
`, id)
	job, err := scanResearchJob(row)
	if err == sql.ErrNoRows {
		return ResearchJob{}, false, nil
	}
	if err != nil {
		return ResearchJob{}, false, err
	}
	return job, true, nil
}

func (s *Store) ListResearchJobs(ctx context.Context, limit int) ([]ResearchJob, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, query, status, mode, created_at, started_at, completed_at, error, max_sources, answer, confidence
FROM research_jobs
ORDER BY created_at DESC
LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ResearchJob
	for rows.Next() {
		job, err := scanResearchJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *Store) UpsertWebSource(ctx context.Context, source WebSource) (WebSource, error) {
	if source.ID == "" {
		source.ID = fmt.Sprintf("source_%d", time.Now().UTC().UnixNano())
	}
	if source.JobID == "" || source.URL == "" {
		return WebSource{}, fmt.Errorf("web source job_id and url are required")
	}
	if source.FetchedAt.IsZero() {
		source.FetchedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO web_sources(id, job_id, url, final_url, title, snippet, source_rank, fetched_at, status, content_hash, trust_score, byte_size, content_type)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	final_url = excluded.final_url,
	title = excluded.title,
	snippet = excluded.snippet,
	source_rank = excluded.source_rank,
	fetched_at = excluded.fetched_at,
	status = excluded.status,
	content_hash = excluded.content_hash,
	trust_score = excluded.trust_score,
	byte_size = excluded.byte_size,
	content_type = excluded.content_type
`, source.ID, source.JobID, source.URL, source.FinalURL, source.Title, source.Snippet, source.SourceRank, formatTime(source.FetchedAt), source.Status, source.ContentHash, source.TrustScore, source.ByteSize, source.ContentType)
	if err != nil {
		return WebSource{}, err
	}
	return source, nil
}

func (s *Store) RecordWebClaim(ctx context.Context, claim WebClaim) (WebClaim, error) {
	if claim.ID == "" {
		claim.ID = fmt.Sprintf("claim_%d", time.Now().UTC().UnixNano())
	}
	if claim.SourceID == "" || claim.Claim == "" {
		return WebClaim{}, fmt.Errorf("web claim source_id and claim are required")
	}
	if claim.CreatedAt.IsZero() {
		claim.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO web_claims(id, source_id, claim, confidence, created_at)
VALUES (?, ?, ?, ?, ?)
`, claim.ID, claim.SourceID, claim.Claim, claim.Confidence, formatTime(claim.CreatedAt))
	if err != nil {
		return WebClaim{}, err
	}
	return claim, nil
}

func (s *Store) WebSourcesByJob(ctx context.Context, jobID string) ([]WebSource, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, job_id, url, final_url, title, snippet, source_rank, fetched_at, status, content_hash, trust_score, byte_size, content_type
FROM web_sources
WHERE job_id = ?
ORDER BY source_rank DESC, id ASC
`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebSource
	for rows.Next() {
		source, err := scanWebSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, source)
	}
	return out, rows.Err()
}

func (s *Store) WebSourceByID(ctx context.Context, id string) (WebSource, bool, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, job_id, url, final_url, title, snippet, source_rank, fetched_at, status, content_hash, trust_score, byte_size, content_type
FROM web_sources
WHERE id = ?
`, id)
	source, err := scanWebSource(row)
	if err == sql.ErrNoRows {
		return WebSource{}, false, nil
	}
	if err != nil {
		return WebSource{}, false, err
	}
	return source, true, nil
}

func (s *Store) WebClaimsByJob(ctx context.Context, jobID string) ([]WebClaim, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT c.id, c.source_id, c.claim, c.confidence, c.created_at
FROM web_claims c
JOIN web_sources s ON s.id = c.source_id
WHERE s.job_id = ?
ORDER BY c.confidence DESC, c.id ASC
`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WebClaim
	for rows.Next() {
		claim, err := scanWebClaim(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, claim)
	}
	return out, rows.Err()
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

func (s *Store) GraphNodes(ctx context.Context, typ string) ([]Node, error) {
	query := `SELECT id, type, label, payload FROM nodes ORDER BY id`
	args := []any{}
	if typ != "" {
		query = `SELECT id, type, label, payload FROM nodes WHERE type = ? ORDER BY id`
		args = append(args, typ)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		var node Node
		if err := rows.Scan(&node.ID, &node.Type, &node.Label, &node.Payload); err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, rows.Err()
}

func (s *Store) NodeCountByType(ctx context.Context, typ string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE type = ?`, typ).Scan(&count)
	return count, err
}

func (s *Store) NodeCountsByType(ctx context.Context) ([]NodeTypeCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT type, COUNT(*) FROM nodes GROUP BY type ORDER BY type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NodeTypeCount
	for rows.Next() {
		var item NodeTypeCount
		if err := rows.Scan(&item.Type, &item.Count); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) Inspect(ctx context.Context, latestLimit int) (InspectStats, error) {
	stats := InspectStats{}
	for _, q := range []struct {
		dst *int
		sql string
	}{
		{&stats.Documents, `SELECT COUNT(*) FROM documents`},
		{&stats.Chunks, `SELECT COUNT(*) FROM chunks`},
		{&stats.Skills, `SELECT COUNT(*) FROM skills`},
		{&stats.Nodes, `SELECT COUNT(*) FROM nodes`},
		{&stats.Edges, `SELECT COUNT(*) FROM edges`},
	} {
		if err := s.db.QueryRowContext(ctx, q.sql).Scan(q.dst); err != nil {
			return InspectStats{}, err
		}
	}
	nodeTypes, err := s.NodeCountsByType(ctx)
	if err != nil {
		return InspectStats{}, err
	}
	stats.NodeTypes = nodeTypes
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

type skillScanner interface {
	Scan(dest ...any) error
}

func scanSkill(row skillScanner) (Skill, error) {
	var skill Skill
	var sequence string
	var createdAt string
	if err := row.Scan(&skill.ID, &skill.Name, &skill.Trigger, &sequence, &skill.SuccessRate, &createdAt); err != nil {
		return Skill{}, err
	}
	actions, err := skillutil.UnmarshalActionSequence(sequence)
	if err != nil {
		return Skill{}, err
	}
	skill.ActionSequence = actions
	if ts, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		skill.CreatedAt = ts
	}
	return skill, nil
}

func scanResearchJob(row skillScanner) (ResearchJob, error) {
	var job ResearchJob
	var createdAt string
	var startedAt sql.NullString
	var completedAt sql.NullString
	if err := row.Scan(&job.ID, &job.Query, &job.Status, &job.Mode, &createdAt, &startedAt, &completedAt, &job.Error, &job.MaxSources, &job.Answer, &job.Confidence); err != nil {
		return ResearchJob{}, err
	}
	job.CreatedAt = parseTime(createdAt)
	if startedAt.Valid {
		job.StartedAt = parseTime(startedAt.String)
	}
	if completedAt.Valid {
		job.CompletedAt = parseTime(completedAt.String)
	}
	return job, nil
}

func scanWebSource(row skillScanner) (WebSource, error) {
	var source WebSource
	var fetchedAt string
	if err := row.Scan(&source.ID, &source.JobID, &source.URL, &source.FinalURL, &source.Title, &source.Snippet, &source.SourceRank, &fetchedAt, &source.Status, &source.ContentHash, &source.TrustScore, &source.ByteSize, &source.ContentType); err != nil {
		return WebSource{}, err
	}
	source.FetchedAt = parseTime(fetchedAt)
	return source, nil
}

func scanWebClaim(row skillScanner) (WebClaim, error) {
	var claim WebClaim
	var createdAt string
	if err := row.Scan(&claim.ID, &claim.SourceID, &claim.Claim, &claim.Confidence, &createdAt); err != nil {
		return WebClaim{}, err
	}
	claim.CreatedAt = parseTime(createdAt)
	return claim, nil
}

func parseTimes(doc *Document, createdAt string, updatedAt string) {
	if ts, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		doc.CreatedAt = ts
	}
	if ts, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
		doc.UpdatedAt = ts
	}
}

func parseTime(value string) time.Time {
	ts, _ := time.Parse(time.RFC3339Nano, value)
	return ts
}

func formatTime(ts time.Time) string {
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func nullableTime(ts time.Time) any {
	if ts.IsZero() {
		return nil
	}
	return formatTime(ts)
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

CREATE TABLE IF NOT EXISTS research_jobs (
	id TEXT PRIMARY KEY,
	query TEXT NOT NULL,
	status TEXT NOT NULL,
	mode TEXT NOT NULL,
	created_at TEXT NOT NULL,
	started_at TEXT,
	completed_at TEXT,
	error TEXT NOT NULL DEFAULT '',
	max_sources INTEGER NOT NULL DEFAULT 0,
	answer TEXT NOT NULL DEFAULT '',
	confidence REAL NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS web_sources (
	id TEXT PRIMARY KEY,
	job_id TEXT NOT NULL REFERENCES research_jobs(id),
	url TEXT NOT NULL,
	final_url TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	snippet TEXT NOT NULL DEFAULT '',
	source_rank REAL NOT NULL DEFAULT 0,
	fetched_at TEXT NOT NULL,
	status TEXT NOT NULL,
	content_hash TEXT NOT NULL DEFAULT '',
	trust_score REAL NOT NULL DEFAULT 0,
	byte_size INTEGER NOT NULL DEFAULT 0,
	content_type TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS web_claims (
	id TEXT PRIMARY KEY,
	source_id TEXT NOT NULL REFERENCES web_sources(id),
	claim TEXT NOT NULL,
	confidence REAL NOT NULL,
	created_at TEXT NOT NULL
);
`
