# Aletheia-mu

Local verifier-first cognitive architecture in Go. The default path is deterministic and local: plan actions, build patch candidates, verify them with allowlisted tools, and persist evidence to SQLite.

Core commands:

```bash
go run ./cmd/aletheia init --db data/memory.sqlite
go run ./cmd/aletheia train --config configs/tiny.yaml --dataset datasets/bootstrap_actions.jsonl --out checkpoints/tiny-actions
go run ./cmd/aletheia train-selector --dataset datasets/selector_bootstrap.jsonl --out checkpoints/selector-bootstrap
go run ./cmd/aletheia solve --task examples/buggy-go/task.json --trace
go run ./cmd/aletheia eval --suite evals/bootstrap --json
```

Useful inspection commands:

```bash
go run ./cmd/aletheia config inspect --config configs/micro.yaml
go run ./cmd/aletheia memory inspect --db data/memory.sqlite
go run ./cmd/aletheia memory graph --db data/memory.sqlite
go run ./cmd/aletheia memory skills --db data/memory.sqlite
```

Opt-in features:

- `solve --search beam` or `solve --search mcts` for branch search.
- `solve --selector-checkpoint checkpoints/selector-bootstrap` for the learned selector.
- `solve --use-skills` for verified skill reuse.
- `solve --fuzz` and `solve --bench` for costly Go verifiers.
- `learn --db ... --out ...` for manual local dataset export.

See [docs/testing.md](docs/testing.md) for the smoke suite and [docs/architecture.md](docs/architecture.md) for subsystem contracts.
