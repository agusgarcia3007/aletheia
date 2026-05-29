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
- Route chat action requests before retrieval/research. Requests like “haz un componente de React” are generated by the checkpoint instead of SearXNG/retrieval, and repo repair requests are directed to `solve`; they must not reuse web docs as an answer.
- Route programming-help questions before retrieval/research. Prompts such as “como es el codigo en Rust?” are answered from the chat checkpoint/profile and must not match unrelated stored web evidence such as MCP pages.
- Train `aletheia-mikros` on behavior/guardrails, not facts. The chat dataset now includes code-snippet behavior, repair-via-`solve`, abstention for future results, and “answer first, cite second” examples.
- Keep deterministic chat replies only as a zero-step bootstrap fallback. Once a checkpoint has training steps, `/v1/chat/completions` should rely on the model plus router/tool policy, not a catalog of hardcoded answers.
- Treat future outcome questions as insufficient evidence unless direct current evidence exists. A 2038 outcome cannot be marked `web_verified` from 1938 or speculative/forum sources.
- Public citations filter blocked/social/forum sources and pages titled `Blocked`; research may store raw evidence for audit, but chat should not present those as trusted citations.

## Milestone Hephaestus Coding Model

- Keep `aletheia-mikros` as the only public model name. Hephaestus remains an internal coding skill/checkpoint concept, not a required user-facing model.
- Load multiple checkpoints from a registry directory in `serve`, but `GET /v1/models` advertises only the public Mikros surface by default.
- Auto-route coding/programming-help prompts inside Mikros before retrieval/research, and keep the OpenAI-compatible response model as `aletheia-mikros` for public requests.
- Support OpenAI-style `tools`/`tool_choice` wire fields and return `assistant.tool_calls` for coding-agent clients without executing tools server-side.
- Keep OpenCode compatibility as the primary target; Cursor is documented as best-effort through custom OpenAI-compatible Base URL surfaces.

## Milestone Mikros Functional V1

- A single tiny chat dataset is not enough; Mikros V1 uses router modes, curated textbook-style examples, tool-use examples, abstention examples, and evidence-grounded research answers.
- Coding prompts such as Rust/Go/JavaScript/React explanations are handled locally before research so stale web chunks cannot answer code questions.
- Research answers are canonicalized into natural language before citation; raw HTML, page chrome, internal paths, and chunk dumps are not acceptable product responses.
- `TransformerV2` is introduced as the real decoder architecture target while legacy checkpoints remain loadable for compatibility; large checkpoint training/promotion stays manual.
- Dataset and tokenizer commands are added so the model artifact can be rebuilt reproducibly without paid APIs.

## Milestone OpenCode Ready V1

- Treat OpenCode as the primary coding-agent compatibility target through the OpenAI-compatible Chat Completions surface.
- Aletheia is a passive provider for tools: it may return `assistant.tool_calls`, but the client executes filesystem and shell tools locally.
- Advertise the real served context and output limits through health/readiness metadata; do not pretend the byte-tokenizer Mikros checkpoint has a 65K context.
- Add deterministic tool-loop state: respect `tool_choice`, avoid repeated tool fingerprints, cap tool calls with `ALETHEIA_AGENT_MAX_TOOL_CALLS`, and prefer list/read/search progression for repo analysis.
- Keep command suggestions read-only or verifier-oriented by default; do not emit destructive shell commands.

## Milestone Mikros Artifact V1

- Treat bad answers as product failures, not model personality. Factual/current questions may not fall through to free generation; they must use canonical evidence, start research, or abstain.
- Keep deterministic smalltalk/help replies available for trained checkpoints when the intent is clearly smalltalk; this prevents a trained-but-weak checkpoint from answering capability questions with unrelated greetings.
- Expand contextual follow-ups with the previous user topic for retrieval/research, but ask for context on ambiguous follow-ups such as `y entonces?`.
- Add `mikros-curriculum-v1` as the first structured curriculum profile with intent, expected mode, answer style, tags, and negative examples.
- Add `evals/mikros_artifact` as the product gate for natural answers, zero links-only responses, correct coding routing, factual abstention, and canonical research answers.
- Add deterministic answer synthesis before public research responses. Winner/list questions must extract the requested entity or list from evidence; page titles and generic snippets are not valid answers.
- For OpenCode repo analysis, synthesize tool arguments from the tool schema and task intent, not from the raw prompt. `glob` uses a real wildcard, `read` uses concrete manifests, and no-files results trigger a different useful probe before the agent gives up.

## Milestone Mikros Vivo V1

- Make Mikros a small verified agent instead of a tiny chatbot with a growing answer dictionary.
- Add a trainable `internal/router` with word/char n-gram features, short-history features, tool presence, evidence signals, and guardrails for abstention/repo/tool boundaries.
- Keep the deterministic router only as the safe fallback when `checkpoints/router-mikros/router.json` is absent.
- Add parametric answerers before retrieval/research: coding slots, simple math, short translation, smalltalk, repo/tool boundary, and abstention.
- Reserve SearXNG research for factual/current/document questions; Python/SQL/Go/Rust/React/math/translation prompts must resolve locally or ask for constraints.
- Add `datasets/router_mikros.jsonl`, `train-router`, and `dataset build --profile mikros-live-v1` so routing and curriculum can improve from data rather than hardcoded final answers.
- Add `evals/mikros_live` as the 250-case live gate: natural answer rate, zero links-only responses, no raw chunk leakage, coding language accuracy, math exactness, short translation, research synthesis, and abstention.
- Legacy generation remains a last fallback until `TransformerV2` is connected, trained, and promoted by evals.

## Milestone Verified Honesty V1

- Never serve raw byte-model generation: `cleanGeneration` rejects replacement runes, control chars and leftover action tokens, and `safeGenerate` only trusts a checkpoint with real training steps. Anything else abstains honestly instead of emitting noise.
- Knowledge lives outside the weights, not in code: coding answers come from an indexed, citable corpus under `knowledge/coding/` (one worked example per file). Curated examples answer first; unseen tasks are retrieved with a `Fuente:` citation; a miss asks for the missing detail. No new hardcoded answer maps.
- Math is real computation, not a lookup: `internal/answerer/math.go` evaluates percentages, powers, roots, linear equations and parenthesized expressions via a recursive-descent parser. Two-operand arithmetic keeps its existing output for gate stability.
- World-knowledge questions are evidence-first by guardrail: a generalized `isFactualKnowledgeQuestion` plus a `forceEvidence` gate prevent the smalltalk/coding answerers or generation from ever answering a factual prompt; it must cite, research, or abstain.
- Low-signal/nonsense input is detected structurally (repeated tokens, keyboard mashing, vowel-starved gibberish), not via a hardcoded list, and asks for a reformulation.
- The trainable router reports validation accuracy from a held-out split (`train-router --val-split`) and warns on a large train/validation gap; deterministic server guardrails (math, coding, factual, nonsense, tool, repo) carry correctness so a weak router only acts as a hint.
- An out-of-distribution battery (`internal/apiserver/battery_*_test.go`) is a permanent gate: zero raw-chunk/links-only/hallucination, >=99% natural-answer rate, capability above a floor. Passing the curated suites is no longer mistaken for generalization.

## Milestone Learn-On-Demand V1

- Coding knowledge is not a pre-loaded dictionary: the corpus under `knowledge/coding/` is only a small verified seed. Unknown coding questions (e.g. Swift) are answered by the learning loop, not by hand-written files.
- The coding answerer's knowledge hook is `codingKnowledgeOrLearn`: (1) return what was already learned (persisted research jobs), (2) else the seed/indexed corpus, (3) else say "I don't know yet" and enqueue a learning job; when it completes, the next ask answers from memory with a citation. Knowledge sticks because jobs/chunks live in SQLite across restarts.
- Coding knowledge gaps are allowed into the learning loop (previously coding was fully blocked from research); repo-repair and curated/seed-covered prompts still never trigger learning, so existing no-research gates stay green.
- Duplicate learning is avoided via `matchingLearningJob`; a question being learned reports "still learning" instead of re-enqueuing.
- Open follow-up: learned coding answers are web-sourced and not yet verified by compiling/running. Verifier-first verification of learned snippets (and `min_trust_score` tuning for code) is the next step before treating a learned answer as fully trusted.

## Milestone Productive V1

- Mixture of experts is architectural, not neural (for now): the router is the gating network and each mode (math, coding, smalltalk, translation, tool, research, abstain) is a sparse expert — exactly one runs per request, cheap experts short-circuit before expensive ones. `/metrics` exposes `aletheia_expert_total{expert=...}` so the routing distribution is observable. A neural MoE belongs to the real `TransformerV2` training milestone, not to a gated-off byte model.
- Speed: retrieval memoizes per-chunk embeddings (`retriever.EmbeddingCache`, keyed by immutable chunk ID) instead of recomputing hash vectors per query; the server reuses one cached retriever. Deterministic chat latency is ~0.5 ms/req on CPU (`TestChatLatencyDeterministicExperts`, a regression gate at 25 ms).
- Verifier-first knowledge: shipped Go knowledge must parse (`go/parser`, in-process, gated by `TestShippedGoKnowledgeParses`); learned Go answers are parser-checked and annotated as verified/unverified before being presented.
- Router is honest and lean: trained with a held-out validation split (`--val-split`) that surfaces the train/validation gap, and pruned of rare single-occurrence features (`--prune-min-count`), shrinking the checkpoint ~66% and reducing memorization. Deterministic server guardrails carry correctness; the linear router is only a hint.
- Operability: `serve --knowledge` configures the indexed corpus; `/readyz` and `/metrics` reflect real state (memory, research, expert distribution). The deployed artifact stays the single public `aletheia-mikros` surface.

## Milestone Self-Improving V1

- Semantic verification of code: shipped and learned Go is checked with the in-process type checker (`go/types`), not just the parser — real type errors are rejected while missing externals in teaching fragments are tolerated. `TestShippedGoKnowledgeParses` gates the corpus; learned Go answers are annotated verified/unverified.
- Closed learning loop for routing: the server records verified routing labels (the deterministic guardrails ARE the labels — when math/coding/factual/tool/abstain fire, the intent is known) as `router_example` nodes. `aletheia learn --train-router-out` harvests them, retrains on base+harvested, and promotes the new router only if it does not regress on a shared held-out set (`router_promoted` + `router_promotion` in the report). Real usage improves the router without shipping a worse model.
- Neural Mixture-of-Experts in `TransformerV2`: when `num_experts > 1`, each layer's feed-forward becomes a sparse MoE — a gating network routes each token to its `top_k_experts` experts (default 2), DeepSeek-style. Forward, gating, top-k selection and the load-balancing aux-loss are implemented and tested; `configs/aletheia-mikros-moe.yaml` is a target config. Backprop/training the MoE at scale is the remaining compute step — the byte-model fallback stays gated to honest abstention until a trained checkpoint passes the eval gates.
