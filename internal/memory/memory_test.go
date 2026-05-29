package memory

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"aletheia/internal/selector"
	"aletheia/internal/skills"
)

func TestMigrateAndRecordEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	episodeID, err := store.CreateEpisode(ctx, "fix failing test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordEvidence(ctx, Evidence{
		EpisodeID: episodeID,
		Verifier:  "go_test",
		Status:    "pass",
		Score:     1,
		Stdout:    "ok",
	}); err != nil {
		t.Fatal(err)
	}

	count, err := store.EvidenceCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("evidence count = %d, want 1", count)
	}
	rows, err := store.EvidenceByVerifier(ctx, episodeID, "go_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Verifier != "go_test" || rows[0].Status != "pass" {
		t.Fatalf("evidence rows = %+v", rows)
	}
}

func TestDocumentChunkGraphAndInspect(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	docPath := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(docPath, []byte("selector decision"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, changed, err := store.UpsertDocument(ctx, docPath, "hash-1", "selector decision")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first upsert should change")
	}
	unchanged, changed, err := store.UpsertDocument(ctx, docPath, "hash-1", "selector decision")
	if err != nil {
		t.Fatal(err)
	}
	if changed || unchanged.ID != doc.ID {
		t.Fatalf("unchanged upsert = doc:%+v changed:%v", unchanged, changed)
	}

	chunks, err := store.ReplaceChunks(ctx, doc.ID, []Chunk{
		{OffsetStart: 0, OffsetEnd: 8, Text: "selector", EmbeddingID: "hashing-v1:64"},
		{OffsetStart: 9, OffsetEnd: 17, Text: "decision", EmbeddingID: "hashing-v1:64"},
	})
	if err != nil {
		t.Fatal(err)
	}
	docNode, err := store.EnsureNode(ctx, "document", doc.Path, "{}")
	if err != nil {
		t.Fatal(err)
	}
	var prev int64
	for _, chunk := range chunks {
		chunkNode, err := store.EnsureNode(ctx, "chunk", "chunk:"+itoa64(chunk.ID), "{}")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.EnsureEdge(ctx, docNode, chunkNode, "contains", 1); err != nil {
			t.Fatal(err)
		}
		if prev != 0 {
			if _, err := store.EnsureEdge(ctx, prev, chunkNode, "next_chunk", 1); err != nil {
				t.Fatal(err)
			}
		}
		prev = chunkNode
	}
	stats, err := store.Inspect(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Documents != 1 || stats.Chunks != 2 || stats.Nodes != 3 || stats.Edges != 3 {
		t.Fatalf("inspect stats = %+v", stats)
	}
	allChunks, err := store.Chunks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(allChunks) != 2 || allChunks[0].Path == "" {
		t.Fatalf("chunks = %+v", allChunks)
	}
	edges, err := store.GraphEdges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 3 {
		t.Fatalf("edges = %+v", edges)
	}
}

func TestRecordTrajectoryCreatesGraph(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	episodeID, err := store.CreateEpisode(ctx, "fix failing test")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordTrajectory(ctx, episodeID, []TrajectoryRecord{
		{SearchNodeID: 1, Depth: 0, Score: 0, Status: "root", Selected: true},
		{SearchNodeID: 2, ParentSearchNodeID: 1, Action: "<ACT_RUN_TESTS>", Source: "test", Depth: 1, Reward: 10, Score: 9.9, Status: "fail", Selected: true},
		{SearchNodeID: 3, ParentSearchNodeID: 1, Action: "<ACT_RESPOND>", Source: "test", Depth: 1, Reward: -20, Score: -20, Status: "unverified"},
	}); err != nil {
		t.Fatal(err)
	}
	stats, err := store.Inspect(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Nodes != 3 || stats.Edges != 3 {
		t.Fatalf("stats = %+v, want 3 nodes and 3 edges", stats)
	}
	edges, err := store.GraphEdges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var selected int
	for _, edge := range edges {
		if edge.Relation == "selected" {
			selected++
		}
	}
	if selected != 1 {
		t.Fatalf("selected edges = %d, want 1: %+v", selected, edges)
	}
}

func TestRecordSelectorExampleCreatesNode(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	episodeID, err := store.CreateEpisode(ctx, "fix failing test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordSelectorExample(ctx, episodeID, "node-2", `{"chosen":"<ACT_RUN_TESTS>"}`); err != nil {
		t.Fatal(err)
	}
	stats, err := store.Inspect(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Nodes != 1 {
		t.Fatalf("nodes = %d, want 1", stats.Nodes)
	}
	count, err := store.NodeCountByType(ctx, "selector_example")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("selector_example nodes = %d, want 1", count)
	}
}

func TestRecordCausalNodeCountsAndGraphFilter(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	episodeID, err := store.CreateEpisode(ctx, "fix failing test")
	if err != nil {
		t.Fatal(err)
	}
	failureID, err := store.RecordCausalNode(ctx, episodeID, "test_failure", "001", map[string]any{"status": "fail"})
	if err != nil {
		t.Fatal(err)
	}
	patchID, err := store.RecordCausalNode(ctx, episodeID, "patch_candidate", "002", map[string]any{"status": "candidate_patch"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureEdge(ctx, patchID, failureID, "fixes", 1); err != nil {
		t.Fatal(err)
	}
	stats, err := store.Inspect(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Nodes != 2 || len(stats.NodeTypes) != 2 {
		t.Fatalf("stats = %+v, want causal node type counts", stats)
	}
	patches, err := store.GraphNodes(ctx, "patch_candidate")
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 1 || patches[0].Type != "patch_candidate" || !strings.Contains(patches[0].Payload, "candidate_patch") {
		t.Fatalf("patch nodes = %+v", patches)
	}
}

func TestSkillsUpsertLookupListAndInspect(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	first, err := store.UpsertSkill(ctx, Skill{
		Name:           skills.FixSimpleGoTestFailure,
		Trigger:        skills.TriggerCalculatorSub,
		ActionSequence: []string{selector.ActParseCode, selector.ActMutateCode, selector.ActVerify, selector.ActRespond},
		SuccessRate:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.UpsertSkill(ctx, Skill{
		Name:           skills.FixSimpleGoTestFailure,
		Trigger:        skills.TriggerCalculatorSub,
		ActionSequence: []string{selector.ActParseCode, selector.ActMutateCode, selector.ActVerify, selector.ActRespond},
		SuccessRate:    0.75,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("upsert duplicated skill: first=%d second=%d", first.ID, second.ID)
	}
	got, ok, err := store.BestSkillByTrigger(ctx, skills.TriggerCalculatorSub)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.ID != first.ID || got.SuccessRate != 0.75 {
		t.Fatalf("skill lookup = %+v ok=%v", got, ok)
	}
	if err := store.UpdateSkillSuccessRate(ctx, got.ID, 0); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListSkills(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].SuccessRate != 0 {
		t.Fatalf("skills = %+v", list)
	}
	stats, err := store.Inspect(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Skills != 1 {
		t.Fatalf("skills count = %d, want 1", stats.Skills)
	}
}

func TestResearchJobSourceClaimLifecycle(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := store.CreateResearchJob(ctx, ResearchJob{Query: "what is mcp", MaxSources: 2})
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimQueuedResearchJob(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || claimed.ID != job.ID || claimed.Status != "running" {
		t.Fatalf("claimed = %+v ok=%v", claimed, ok)
	}
	source, err := store.UpsertWebSource(ctx, WebSource{
		ID:          "source-1",
		JobID:       job.ID,
		URL:         "https://example.com/mcp",
		FinalURL:    "https://example.com/mcp",
		Title:       "MCP",
		Status:      "stored",
		ContentHash: "hash",
		TrustScore:  0.7,
		ByteSize:    128,
		ContentType: "text/html",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordWebClaim(ctx, WebClaim{ID: "claim-1", SourceID: source.ID, Claim: "MCP is a protocol.", Confidence: 0.8}); err != nil {
		t.Fatal(err)
	}
	claimed.Status = "completed"
	claimed.Answer = "MCP is a protocol."
	claimed.Confidence = 0.8
	if err := store.UpdateResearchJob(ctx, claimed); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.ResearchJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Status != "completed" || got.Answer == "" {
		t.Fatalf("job = %+v ok=%v", got, ok)
	}
	sources, err := store.WebSourcesByJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := store.WebClaimsByJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || len(claims) != 1 {
		t.Fatalf("sources=%+v claims=%+v", sources, claims)
	}
	byID, ok, err := store.WebSourceByID(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || byID.FinalURL != source.FinalURL {
		t.Fatalf("source by id = %+v ok=%v", byID, ok)
	}
}

func itoa64(v int64) string {
	return strconv.FormatInt(v, 10)
}
