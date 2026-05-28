# Aletheia-mu Architecture

Aletheia-mu is a local, verifier-first Go system. It reads a task, chooses functional actions, runs allowlisted verifiers, applies only verified changes, and persists evidence locally.

Current loop:

1. `cmd/aletheia` parses commands with the Go standard library.
2. `internal/verifier` runs allowlisted checks such as `go test ./...` and `static_go_parse`.
3. `internal/cognitivevm` coordinates task execution, action traces, toy patch candidates, verification, and rollback.
4. `internal/selector` chooses actions from model/mock candidates. The default is a safe heuristic; an opt-in linear selector can be trained from JSONL examples.
5. `internal/search` can run opt-in beam search over action branches in temporary repository copies.
6. `internal/memory` stores episodes, verifier evidence, document chunks, search trajectories, and selector examples in SQLite.
7. `internal/tokenizer`, `internal/model`, and `internal/runner` provide the local micro-model path for action candidates.

Checkpoints are split by responsibility:

- model checkpoints store `manifest.json` and `weights.f32`;
- selector checkpoints store `selector.json` with fixed feature names and linear weights.

The toy patcher is still intentionally bounded to `examples/buggy-go`. Later milestones can replace it with generated patches while keeping the same verifier, selector, search, and memory contracts.
