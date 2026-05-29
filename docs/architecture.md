# Aletheia-mu Architecture

Aletheia-mu is a local, verifier-first Go system. It reads a task, chooses functional actions, runs allowlisted verifiers, applies only verified changes, and persists evidence locally.

Current loop:

1. `cmd/aletheia` parses commands with the Go standard library.
2. `internal/config` provides strict typed YAML defaults when commands opt in with `--config`; explicit CLI flags still take precedence.
3. `internal/verifier` runs allowlisted checks such as `go test ./...` and `static_go_parse` through a centralized no-shell command runner.
4. `internal/cognitivevm` coordinates task execution, action traces, deterministic repair candidates, verification, and rollback.
5. `internal/selector` chooses actions from model/mock candidates. The default is a safe heuristic; an opt-in linear selector can be trained from JSONL examples.
6. `internal/search` can run opt-in beam or MCTS search over action branches in temporary repository copies.
7. `internal/skills` compresses known verified traces into deterministic reusable skills and replays them only when their trigger still matches the current repository.
8. `internal/memory` stores episodes, verifier evidence, document chunks, causal graph nodes, search trajectories, selector examples, and compressed skills in SQLite.
9. `internal/tokenizer`, `internal/model`, and `internal/runner` provide the local micro-model path for action candidates, trajectory scoring, and opt-in sampling.
10. `internal/repair` builds bounded Go patch candidates from verifier failures without writing files before verification.
11. `internal/learning` exports local memory into reproducible JSONL datasets and can retrain the selector manually.
12. `internal/apiserver` exposes checkpoint inference through OpenAI-compatible HTTP endpoints for deployment.

Checkpoints are split by responsibility:

- model checkpoints store `manifest.json` and `weights.f32`;
- `aletheia-mikros` is the public router/basic-chat checkpoint for `train`, `run`, and `serve`;
- `aletheia-hephaestus` is the first specialist coding checkpoint for snippets, explanations, and OpenAI-compatible coding-agent tool-call formatting;
- `solve` remains verifier-first and does not require a served planner checkpoint by default;
- selector checkpoints store `selector.json` with fixed feature names and linear weights.

Skill reuse is opt-in with `solve --use-skills`. A successful normal solve can write a compressed skill to the existing `skills` table, while a later matching solve can skip the initial verifier, replay the compressed action sequence, verify the patch, and report fewer tool calls. Failed skill reuse restores the touched files, marks the skill success rate as `0`, and falls back to the normal solver path.

Configuration is opt-in and strict. Commands keep their current defaults without `--config`; with `--config`, `project.memory_db`, search defaults, verifier defaults, inference defaults, and memory indexing defaults come from YAML unless a flag overrides them.

Evaluation is programmatic and verifier-first. `evals/bootstrap` guards bootstrap behavior, while `evals/production` is the release gate for the verified-agent target with 100 deterministic checks covering doc QA, abstention, Go repair, repo QA, and research evidence policy. `eval --json` emits machine-readable metrics for learning loops.

Memory graph payloads are append-only JSON records on the existing `nodes`/`edges` tables. Solve and search now record `test_failure`, `counterexample`, `repair_attempt`, `patch_candidate`, and `verified_patch` nodes with causal edges such as `derived_from`, `failed_because`, `verifies`, `fixes`, and `breaks`. `memory graph` provides a textual filtered view.

Document QA is citation-gated. `ask` returns `verified: true` only when the answer cites existing chunks and the local text verifier finds answer terms supported by those chunks; otherwise it abstains.

MCTS is opt-in with `solve --search mcts` or `search.strategy: mcts`. Beam remains the stable search baseline and greedy remains the default fallback.

Costly verifiers are opt-in. `go_test_fuzz` and `go_test_bench` run through the same allowlisted no-shell runner and are selected with config or `solve --fuzz` / `solve --bench`.

The repair engine is intentionally deterministic. Go Repair V1 handles small verifier-driven patches such as wrong arithmetic, known undefined function rename, missing/unused imports, simple int return mismatches, and a narrow nil-pointer guard. It still produces patch candidates only; verification remains mandatory before materialization.

Model scaling is manual. `configs/core-100m.yaml` is a target configuration, not an automatic test path; scaling should wait for eval evidence that a checkpoint beats the current mock/heuristic stack.

Deployment is inference/research-only in public v1. `aletheia serve` can load a checkpoint directory containing multiple model subdirectories and serves `/v1/models`, `/v1/chat/completions`, and `/v1/completions` behind local Bearer auth, plus `/readyz`, `/metrics`, and authenticated research job inspection. The Docker/Dokploy default loads `aletheia-mikros` and `aletheia-hephaestus`; Mikros is the user-facing router, and coding/help prompts auto-route to Hephaestus when available. Action-like chat and programming-help requests are handled before retrieval: code snippets are generated by the checkpoint/profile without web research, while repo repair is directed to `solve` unless an OpenAI-compatible coding-agent client provides tools. Deterministic chat responses are only a zero-step bootstrap fallback; trained checkpoints use the model. The API does not execute tools server-side, expose `solve`, verifiers, repository access, or file mutation.

Research is opt-in. The chat router handles smalltalk and action requests locally, prefers completed verified research answers, searches memory for factual questions, and can queue background SearXNG research jobs on knowledge gaps when enabled. Completed research answers are synthesized from stored claims instead of returning source titles alone, citations use public URLs instead of internal paths, and blocked/social/forum sources are filtered from public answers. Future outcome questions without direct current evidence abstain instead of becoming `web_verified`. Research fetches pages with bounded HTTP policy, extracts text and claims, stores pages in documents/chunks, persists `research_jobs`/`web_sources`/`web_claims`, and writes graph nodes for future local retrieval. Failed jobs are hidden from default job listing so the UI can focus on active/completed work.
