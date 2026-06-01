# Contributing to Aletheia

Thanks for your interest in Aletheia. This is a **local, verifier-first cognitive
architecture in Go** — a sparse-expert agent, not a monolithic LLM. Before contributing,
please read [`CLAUDE.md`](./CLAUDE.md), [`README.md`](./README.md), and
[`docs/architecture.md`](./docs/architecture.md) to understand the thesis: *a tiny
parametric model handles intent, routing and language; truth lives outside the weights*
— in tools, retrieval, verifiers, memory, and a learning loop that compounds over time.

## The non-negotiables

Contributions must respect the core principles. PRs that violate these will not be merged:

1. **Honest abstention over hallucination.** Returning *"I don't know / I'm learning this"*
   is correct behavior, not a bug. Never make an expert emit unverified content to look
   more capable.
2. **Verification is the reward.** Where a domain has reliable verifiers, use them instead
   of a judge model (`go test`, the Go type checker, citation support checks).
3. **Facts in storage, not in weights.** Answers cite chunks (source + date) from SQLite,
   or they abstain.
4. **Deterministic guardrails carry correctness; the router is only a hint.** `abstain`,
   `tool_call`, and `repo_agent` are always routed deterministically.
5. **Capability comes from the loop, not from parameters.** Keep the parametric model
   minimal. Never promote a model/router that regresses on the held-out set.
6. **No server-side execution.** The API never runs tools, shell, verifiers, or file
   mutations server-side — the client executes the model's tool calls.

## Tech stack & constraints

- **Go 1.26.3**, module `aletheia`. Only **two** direct dependencies: `gopkg.in/yaml.v3`
  and `modernc.org/sqlite` (pure-Go, **no cgo**). **Do not add heavy / cgo / GPU
  dependencies.** Standard-library first.
- **CPU-only**, designed to run on a VPS. No external model calls for
  coding/math/smalltalk/translation. Embeddings are hashing-based (FNV, 64 dims).
- Idiomatic Go. Mirror the existing package layout and naming.

## Before you open a PR

Run the full local gate and make sure everything is green:

```bash
go build ./...
go vet ./...
go test ./...
```

If your change touches the **router, model, or experts**, you must also run the eval
gates and confirm **no regression**:

```bash
go run ./cmd/aletheia eval --suite evals/production --json
go run ./cmd/aletheia eval --suite evals/mikros_live --json
```

`evals/production` must stay clean: **zero hallucination, zero raw-chunk leakage, zero
links-only answers, ~100% natural-answer rate.** `mikros_live` must not regress.

Other gates to keep in mind:

- **Latency is a gate.** `internal/apiserver/latency_test.go` enforces avg < 25 ms/req
  (real ~0.5 ms). Don't add per-request work that breaks this; reuse the embedding
  memoization pattern.
- **Determinism.** Routing/training/eval paths avoid RNG where reproducibility matters
  (deterministic validation splits, deterministic top-k). Preserve this.

## Adding or extending an expert

An expert without a guardrail and eval coverage is **incomplete**. When you add or extend
a sparse expert, submit these together in the same PR:

1. the sparse expert itself,
2. its deterministic guardrail, and
3. eval cases covering it.

## Pull request process

1. **Fork** the repo and create a topic branch off `main` (e.g. `feat/factual-dates`).
   Direct pushes to `main` are blocked — all changes go through a PR.
2. Keep PRs **focused and small**. One logical change per PR.
3. Write a clear description: what changed, why, and which gates you ran (paste the eval
   output if you touched router/model/experts).
4. Match the codebase style. Run `gofmt`/`go vet` before pushing.
5. Make sure CI (when present) and all checks pass, and that conversations are resolved.

## Commit messages

Use clear, conventional-style prefixes that match the existing history, e.g.:

```
feat(apiserver): add factual date expert with guardrail
fix(router): unwrap context-packed user messages before routing
docs: add contributing guide
refactor: honesty pass on research synthesis
```

## Reporting bugs & proposing changes

- **Bugs:** open an issue with a minimal reproduction, the command you ran, and the
  expected vs. actual behavior. Include the eval suite output if relevant.
- **Larger changes:** open an issue to discuss the design *before* writing code, so we can
  confirm it fits the thesis (external verifiable truth over bigger weights).

## Code of conduct

This project follows the [Contributor Covenant](./CODE_OF_CONDUCT.md). By participating,
you are expected to uphold it.

## License

By contributing, you agree that your contributions will be licensed under the
[MIT License](./LICENSE) that covers this project.
