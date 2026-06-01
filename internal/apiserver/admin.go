package apiserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"aletheia/internal/config"
	"aletheia/internal/harvest"
	"aletheia/internal/research"
	"aletheia/internal/training"
)

// adminPipeline tracks the single in-flight self-improvement run triggered over
// HTTP. The production VPS is hard to reach by shell, so this lets an operator
// drive the loop — seed (research topics) -> harvest (verified answers) ->
// train — by hitting one endpoint and polling for status.
//
// It is OFF unless ALETHEIA_ADMIN_TOKEN is set, and every call is constant-time
// checked against the X-Admin-Token header. It intentionally runs work
// server-side (training), which the request path otherwise avoids
// (principle #6); this is an explicit, token-gated operational tool, not part of
// how chat requests are served.
type adminPipeline struct {
	mu        sync.Mutex
	running   bool
	phase     string // idle | seeding | harvesting | training | done | error
	message   string
	errText   string
	startedAt time.Time
	endedAt   time.Time
	seeded    int
	harvested int
	report    *training.Report
}

func newAdminPipeline() *adminPipeline {
	return &adminPipeline{phase: "idle"}
}

func (p *adminPipeline) set(phase, message string) {
	p.mu.Lock()
	p.phase = phase
	p.message = message
	p.mu.Unlock()
}

func (p *adminPipeline) snapshot() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]any{
		"running":   p.running,
		"phase":     p.phase,
		"message":   p.message,
		"seeded":    p.seeded,
		"harvested": p.harvested,
	}
	if p.errText != "" {
		out["error"] = p.errText
	}
	if !p.startedAt.IsZero() {
		out["started_at"] = p.startedAt.UTC().Format(time.RFC3339)
	}
	if !p.endedAt.IsZero() {
		out["ended_at"] = p.endedAt.UTC().Format(time.RFC3339)
	}
	if p.report != nil {
		out["train"] = map[string]any{
			"initial_loss":     p.report.InitialLoss,
			"final_loss":       p.report.FinalLoss,
			"initial_accuracy": p.report.InitialAccuracy,
			"final_accuracy":   p.report.FinalAccuracy,
			"steps":            p.report.Steps,
			"checkpoint":       p.report.CheckpointPath,
		}
	}
	return out
}

// adminAuthorized constant-time compares the X-Admin-Token header against the
// configured admin token. Returns false (treated as "not found") when admin is
// disabled or the token is missing/wrong.
func (s *Server) adminAuthorized(r *http.Request) bool {
	token := strings.TrimSpace(s.opts.AdminToken)
	if token == "" {
		return false
	}
	got := strings.TrimSpace(r.Header.Get("X-Admin-Token"))
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

type adminPipelineRequest struct {
	Topics        []string `json:"topics"`
	MaxSources    int      `json:"max_sources"`
	Config        string   `json:"config"`
	Dataset       string   `json:"dataset"`
	Out           string   `json:"out"`
	Steps         int      `json:"steps"`
	MinConfidence float64  `json:"min_confidence"`
}

func (s *Server) handleAdminPipeline(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		writeAPIError(w, http.StatusNotFound, "invalid_request_error", "", "not_found", "not found")
		return
	}
	if s.store == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "store", "store_unavailable", "memory store is not configured")
		return
	}

	var req adminPipelineRequest
	body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, s.opts.MaxBodyBytes))
	if len(strings.TrimSpace(string(body))) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "", "invalid_json", err.Error())
			return
		}
	}
	// Default outputs under the persistent data dir (a mounted volume), writable
	// AND surviving redeploys — unlike checkpoints/ and datasets/ which ship in
	// the read-only image layer.
	dataDir := strings.TrimSpace(s.opts.DataDir)
	if dataDir == "" {
		dataDir = "data"
	}
	if req.Dataset == "" {
		req.Dataset = filepath.Join(dataDir, "generated", "mikros_chat_harvested.jsonl")
	}
	if req.Out == "" {
		req.Out = filepath.Join(dataDir, "checkpoints", "aletheia-mikros-gen")
	}
	if req.MinConfidence <= 0 {
		req.MinConfidence = 0.5
	}

	p := s.adminState
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		writeAPIError(w, http.StatusConflict, "invalid_request_error", "pipeline", "already_running", "a pipeline run is already in progress")
		return
	}
	p.running = true
	p.phase = "starting"
	p.message = ""
	p.errText = ""
	p.seeded = 0
	p.harvested = 0
	p.report = nil
	p.startedAt = time.Now()
	p.endedAt = time.Time{}
	p.mu.Unlock()

	go s.runAdminPipeline(req)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "started",
		"message": "pipeline running in background; poll GET /v1/aletheia/admin/pipeline for status",
	})
}

func (s *Server) runAdminPipeline(req adminPipelineRequest) {
	p := s.adminState
	ctx := context.Background()
	finish := func(phase, msg, errText string) {
		p.mu.Lock()
		p.running = false
		p.phase = phase
		if msg != "" {
			p.message = msg
		}
		p.errText = errText
		p.endedAt = time.Now()
		p.mu.Unlock()
	}

	// 1) Seed (optional): fill the corpus by researching the given topics so
	// there is verified material to harvest and train on.
	if len(req.Topics) > 0 && s.research.Enabled {
		p.set("seeding", "researching seed topics")
		worker := research.NewWorker(s.store, s.research)
		for _, topic := range req.Topics {
			topic = strings.TrimSpace(topic)
			if topic == "" {
				continue
			}
			job, err := s.enqueueResearch(ctx, topic, "sync", req.MaxSources)
			if err != nil {
				continue
			}
			if _, err := worker.RunJob(ctx, job); err == nil {
				p.mu.Lock()
				p.seeded++
				p.mu.Unlock()
			}
		}
	}

	// 2) Harvest verified (query -> answer) pairs into a training dataset.
	p.set("harvesting", "harvesting verified answers into a training set")
	n, err := harvest.HarvestChatDataset(ctx, s.store, req.Dataset, req.MinConfidence)
	if err != nil {
		finish("error", "harvest failed", err.Error())
		return
	}
	p.mu.Lock()
	p.harvested = n
	p.mu.Unlock()
	if n == 0 {
		finish("done", "no verified examples to train on yet — seed more topics first", "")
		return
	}

	// 3) Train the model on the harvested dataset (writes a checkpoint; does not
	// hot-swap the served model — point `serve --checkpoint` at it to use it).
	p.set("training", "training model on harvested dataset")
	trainOpts := training.Options{
		DatasetPath:   req.Dataset,
		OutDir:        req.Out,
		Steps:         req.Steps,
		OverrideSteps: req.Steps > 0,
	}
	// Use an explicit config path when given; otherwise the built-in default so
	// training works in a container that ships no configs/ directory.
	if strings.TrimSpace(req.Config) != "" {
		trainOpts.ConfigPath = req.Config
	} else {
		cfg := config.Default()
		trainOpts.Config = &cfg
	}
	report, err := training.Train(ctx, trainOpts)
	if err != nil {
		finish("error", "training failed", err.Error())
		return
	}
	p.mu.Lock()
	p.report = &report
	p.mu.Unlock()
	finish("done", "pipeline complete — point serve at the new checkpoint to use it", "")
}

func (s *Server) handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	if !s.adminAuthorized(r) {
		writeAPIError(w, http.StatusNotFound, "invalid_request_error", "", "not_found", "not found")
		return
	}
	writeJSON(w, http.StatusOK, s.adminState.snapshot())
}
