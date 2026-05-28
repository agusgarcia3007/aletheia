# Decisions

## Milestone 0

- Use `module aletheia` to avoid assuming a remote repository path.
- Use only the standard library for CLI parsing.
- Use `modernc.org/sqlite` for SQLite so the project stays pure Go and avoids cgo.
- Keep the first solver deterministic: it patches the known `examples/buggy-go` failure and proves the verifier/evidence loop.
- Resolve relative task repository paths from the process working directory.
- Reject unsupported success commands before execution.
- Keep `ALETHEIA_MU_PROMPT.md` as the source specification.

## Milestone 1

- Add a tiny CPU training path for automated acceptance and keep `seed-10m` as a manual config target.
- Use a deterministic causal next-token model with float32 tensors and manual gradients so overfit checks run quickly on CPU.
- Store checkpoints as `manifest.json` plus little-endian `weights.f32`.
- Keep generated checkpoints ignored by git.

## Milestone 2

- Route `solve` through an explicit Cognitive VM with functional action tokens and action trace.
- Keep the toy patcher bounded to `examples/buggy-go`, but delay file writes until `<ACT_VERIFY>`.
- Use a heuristic selector that can fall back safely when model candidates are missing or invalid.
- Allow optional model-backed planning from a checkpoint without requiring it for `solve`.

## Milestone 3

- Use a verifier bus so VM actions can run one or more required verifiers and aggregate evidence.
- Execute verifier commands without a shell and only through an exact allowlist.
- Keep `go_test` as the default required verifier; make `static_go_parse`, `go_vet`, and `go_test_race` opt-in.
- Store structured verifier payloads in the existing evidence table instead of adding a migration.

## Milestone 4

- Keep `run` as the model-generation command and add `ask` for local document QA.
- Reuse the existing SQLite document/chunk/node/edge schema for indexing and retrieval.
- Use deterministic keyword plus hashing-vector retrieval with no remote API or model dependency.
- Answer only from indexed local chunks and abstain when confidence is too low.

## Milestone 5

- Keep greedy `solve` as the default path and make beam search opt-in with `--search beam`.
- Implement `internal/search` as a generic callback-driven package so it does not import `cognitivevm` and create an import cycle.
- Run beam branches in temporary repository copies, then apply the verified winner to the original repository and verify it again.
- Score beam states with a deterministic local reward based on verifier pass/fail, candidate patches, discovered patterns, evidence, depth, and branch errors.
- Persist beam trajectories in the existing `nodes` and `edges` graph as `trajectory_state` records without adding a migration.
- Use a programmatic bootstrap eval where candidate-greedy fails on a noisy planner and beam preserves the verified branch.

## Milestone 6

- Use logistic regression as the first trainable selector instead of reusing the micro-transformer.
- Keep the learned selector opt-in through `--selector-checkpoint`; the heuristic selector remains the default safe path.
- Store selector checkpoints as JSON in a separate directory from generative model checkpoints.
- Train and evaluate the selector from a deterministic local JSONL bootstrap dataset.
- Persist selector examples from beam as `selector_example` graph nodes without adding a SQLite migration.
- Extend bootstrap eval with an A/B where candidate-greedy fails and the learned selector passes.
- Attach verifier status to trace entries only when that action actually produced new verifier evidence.

## Milestone 7

- Implement skill compression as a verified solver post-process instead of adding `<ACT_COMPRESS_SKILL>` to the VM action set.
- Keep skill reuse opt-in through `solve --use-skills`; normal `solve`, learned selector, and beam behavior remain unchanged by default.
- Use the deterministic trigger `go_test:calculator_return_subtract` for the current `examples/buggy-go` case and detect it before mutating the repository.
- Store skills in the existing `skills` table with JSON action sequences and no SQLite migration.
- Compress only verified traces into `fix_simple_go_test_failure` with `<ACT_PARSE_CODE>`, `<ACT_MUTATE_CODE>`, `<ACT_VERIFY>`, and `<ACT_RESPOND>`.
- On failed skill reuse, restore touched files, set the skill success rate to `0`, and fall back to the normal solver path.
- Report skill reuse explicitly in CLI output with `skill: <name>`, `initial verifier: skipped`, and `tool_calls`.
- Extend bootstrap eval with `skill_reuse_cost_reduction` so the learned skill must pass with fewer verifier tool calls than the baseline solve.
