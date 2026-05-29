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

## Milestone 8

- Treat `configs/*.yaml` as a strict typed contract for runtime defaults.
- Keep config opt-in through `--config`; without it, existing CLI defaults remain unchanged.
- Let explicit CLI flags override all config-derived defaults.
- Validate verifier config against the existing allowlist and keep enabled verifier order deterministic.
- Declare `fuzz` in config as disabled until a local verifier exists.
- Use `memory.graph_enabled=false` to skip retriever graph writes without changing the SQLite schema.

## Milestone 9

- Centralize local command execution behind one no-shell allowlist used by verifiers and future VM tool actions.
- Keep `<ACT_RUN_CMD>` constrained to allowlisted verifier/read-only commands; no arbitrary shell or mutating commands.
- Mark blocked commands as `unknown` evidence with `blocked_reason=command_allowlist`.
- Add read-only allowlist entries for `rg`, `git diff`, `git status`, `cat`, and `ls`, with repo-relative path checks.

## Milestone 10

- Make bootstrap eval a programmatic suite with explicit pass/fail cases and aggregate metrics.
- Keep text eval output stable for humans and add `eval --json` for machine-readable learning loops.
- Measure verified success, abstention, memory hit rate, tool-call cost, and elapsed seconds per success.
- Evaluate verifiable behavior only: compile detection, Go test repair, doc QA, abstention, memory recall, beam, learned selector, and skill reuse.

## Milestone 11

- Add a deterministic Go repair package before expanding beyond the known calculator example.
- Introduce `<ACT_FIND_COUNTEREXAMPLE>` and `<ACT_REPAIR>` while keeping `<ACT_MUTATE_CODE>` as a compatibility path.
- Keep repairs as patch candidates only; files are still written only during `<ACT_VERIFY>`.
- Start with small text/AST-adjacent rules and reject unknown failures instead of generating broad edits.

## Milestone 12

- Persist causal solve state in the existing `nodes` and `edges` graph without migrations.
- Use typed causal nodes for `test_failure`, `counterexample`, `repair_attempt`, `patch_candidate`, and `verified_patch`.
- Record causal edges such as `derived_from`, `failed_because`, `verifies`, `fixes`, and `breaks` only from verified local events.
- Extend `memory inspect` and add `memory graph` for textual graph inspection instead of adding a new graph database.

## Milestone 13

- Make document QA citation-gated: answered responses must cite existing chunks.
- Add `TextEvidenceVerifier` as a local verifier for answer/citation consistency.
- Print `verified: true|false` from `ask` and abstain when citation evidence is missing or unsupported.
- Keep hallucination accounting tied to answered responses without valid citation support.

## Milestone 14

- Add generic MCTS to `internal/search` without importing `cognitivevm`.
- Keep MCTS opt-in through `solve --search mcts` and config `search.strategy: mcts`; beam remains the stable search option.
- Persist MCTS nodes as `trajectory_state` records with `visits`, `value`, `prior`, and reward metadata.
- Add a bootstrap eval where candidate-greedy fails and MCTS preserves the verified branch.

## Milestone 15

- Add `aletheia learn` as a manual local learning command, not a daemon.
- Export `selector_example` nodes and verified trajectories from memory into reproducible JSONL files.
- Optionally retrain the linear selector from exported examples with `--train-selector-out`.
- Report eval metrics before and after the learning run when a suite is provided.

## Milestone 16

- Enable costly verifiers only opt-in: `go_test_fuzz` and `go_test_bench`.
- Keep fuzz and bench execution behind the centralized no-shell allowlist with strict command shapes.
- Add `solve --fuzz` and `solve --bench` so normal solve cost is unchanged.
- Store command, timeout behavior, truncated output markers, and status in the existing evidence payload path.

## Milestone 17

- Keep `Runner.Score(sequence)` as the local logprob path for trajectories.
- Add real temperature/top-p/top-k sampling to generation while preserving greedy behavior when temperature is zero.
- Add `configs/core-100m.yaml` as a manual target only; it is not part of automatic tests.
- Defer model scaling until evals show a checkpoint beats the mock/heuristic baseline.

## Milestone 18

- Consolidate public contracts in docs instead of relying only on tests.
- Document real CLI smoke checks in `docs/testing.md`.
- Keep memory graph payloads and generated datasets JSON-based for inspectability.
- Leave ternary/int8, mixture of experts, overnight daemon, and private frontier benchmarks out of the core path for now.

## Milestone 19

- Add an OpenAI-compatible inference server for deploying a local checkpoint behind the official SDKs.
- Implement Chat Completions and legacy Completions first; keep Responses API out of v1 to reduce compatibility surface.
- Require local Bearer auth through `ALETHEIA_API_KEY`; this is the API key clients pass to the OpenAI SDK.
- Serve inference only. Do not expose `solve`, verifiers, repository access, shell commands, or file mutation over the public API.
- Use a self-contained Dockerfile for Dokploy so deployable checkpoints are produced during image build.

## Milestone 20

- Use `aletheia-mikros` as the first public local prompt/completion checkpoint.
- Keep the first public model intentionally narrow: greetings, identity, limits, Aletheia commands, API usage, and soft abstention.
- Train it locally from `datasets/aletheia_mikros.jsonl` with short `<EOS>`-terminated completions.
- Add a deterministic chat profile for `aletheia-mikros` in Chat Completions so the first public API returns stable basic responses while checkpoint training remains primitive.
- Keep `solve` verifier-first and separate from the public chat API; it does not require a served planner checkpoint by default.
- Make Docker/Dokploy build and serve one checkpoint only: `aletheia-mikros`.
- Use a zero-step bootstrap checkpoint in Docker builds so deploys do not retrain on every build; real training remains a local/manual command.

## Milestone Research-2

- Add opt-in SearXNG-backed research for knowledge gaps while keeping local memory first.
- Persist research jobs, web sources, claims, documents, chunks, and graph evidence with additive SQLite tables only.
- Keep chat research background by default; synchronous research is explicit only.
- Block social domains by default and enforce timeout, max bytes, redirect, content-type, and user-agent policy in the HTTP fetcher.
- Treat single-source web evidence as unverified support, not verified truth.
- Keep research free of paid APIs, remote LLM APIs, shell execution, and repository mutation.

## Milestone Production V1

- Compete as a verified agent on VPS CPU, not as a general chatbot or parametric knowledge model.
- Keep `aletheia-mikros` narrow; use memory, research, repair, and verifiers as the production differentiator.
- Add `evals/production` as a deterministic 100-case release gate with false-verified, citation-validity, abstention, repair, latency, and cost metrics.
- Prefer completed research answers before extracting ad hoc sentences from chunks; never reuse unrelated web memory when substantive query terms do not overlap.
- Use SQLite WAL, `/readyz`, `/metrics`, and filtered job listings as the minimum production operations surface.
- Extend Go repair only through bounded deterministic patch candidates that still require verifier pass before materialization.
