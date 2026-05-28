# Aletheia-mu Architecture

Aletheia-mu is a local, verifier-first Go system. It reads a task, chooses functional actions, runs allowlisted verifiers, applies only verified changes, and persists evidence locally.

Current loop:

1. `cmd/aletheia` parses commands with the Go standard library.
2. `internal/config` provides strict typed YAML defaults when commands opt in with `--config`; explicit CLI flags still take precedence.
3. `internal/verifier` runs allowlisted checks such as `go test ./...` and `static_go_parse`.
4. `internal/cognitivevm` coordinates task execution, action traces, toy patch candidates, verification, and rollback.
5. `internal/selector` chooses actions from model/mock candidates. The default is a safe heuristic; an opt-in linear selector can be trained from JSONL examples.
6. `internal/search` can run opt-in beam search over action branches in temporary repository copies.
7. `internal/skills` compresses known verified traces into deterministic reusable skills and replays them only when their trigger still matches the current repository.
8. `internal/memory` stores episodes, verifier evidence, document chunks, search trajectories, selector examples, and compressed skills in SQLite.
9. `internal/tokenizer`, `internal/model`, and `internal/runner` provide the local micro-model path for action candidates.

Checkpoints are split by responsibility:

- model checkpoints store `manifest.json` and `weights.f32`;
- selector checkpoints store `selector.json` with fixed feature names and linear weights.

Skill reuse is opt-in with `solve --use-skills`. A successful normal solve can write a compressed skill to the existing `skills` table, while a later matching solve can skip the initial verifier, replay the compressed action sequence, verify the patch, and report fewer tool calls. Failed skill reuse restores the touched files, marks the skill success rate as `0`, and falls back to the normal solver path.

Configuration is opt-in and strict. Commands keep their current defaults without `--config`; with `--config`, `project.memory_db`, search defaults, verifier defaults, inference defaults, and memory indexing defaults come from YAML unless a flag overrides them.

The toy patcher is still intentionally bounded to `examples/buggy-go`. Later milestones can replace it with generated patches while keeping the same verifier, selector, search, and memory contracts.
