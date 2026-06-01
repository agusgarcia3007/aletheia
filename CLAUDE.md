# CLAUDE.md

Guidance for Claude Code when working in this repository.

## What Aletheia is

Aletheia (μ = *mikros*) is a **local, verifier-first cognitive architecture in Go**. It exposes an
OpenAI-compatible API under the model name `aletheia-mikros`, but it is **not a monolithic LLM** — it
is a sparse-expert agent.

**The thesis (do not violate it):** a tiny parametric model handles *intent, routing, and language*;
**truth lives outside the weights** — in tools, retrieval, verifiers, memory, and a learning loop that
compounds over time. Aletheia is not a small model imitating a large one; it is a different bet about
where intelligence should live. When in doubt, prefer *external verifiable truth* over *bigger weights*.

## Core principles (the non-negotiables)

1. **Honest abstention over hallucination.** Generation is gated off (`safeGenerate`) for checkpoints
   without real training. Returning "I don't know / I'm learning this" is correct behavior, not a bug.
   Never make an expert emit unverified content to look more capable.
2. **Verification is the (free) reward.** When a domain has reliable verifiers, use them instead of a
   judge model: `go test` passes or fails, Go snippets type-check or don't, a citation is supported or
   it isn't. Code knowledge is checked in-process with the Go type checker, not just parsed.
3. **Facts in storage, not in weights.** RAG-style: answers cite chunks (source + date) from SQLite, or
   they abstain. Cited `verified: true` requires a text verifier to confirm answer terms are supported.
4. **Deterministic guardrails carry correctness; the router is only a hint.** `abstain`, `tool_call`,
   and `repo_agent` are always routed deterministically. A weak/untrained linear router must never be
   able to produce an unsafe outcome.
5. **Capability comes from the loop, not from parameters.** The parametric model stays *minimal*
   (smaller is better) — it handles intent, routing and language, nothing more. Intelligence grows
   through the self-improving loop (harvest verified labels → retrain → gate → promote), retrieval,
   verifiers and memory — never by silently scaling weights. There is **no predefined large-model
   training target**; the bet is external verifiable truth, not bigger weights. The dataset and evals
   are the product. Never promote a model/router that regresses on the held-out set.
6. **No server-side execution.** The API does not run tools, shell, `solve`, verifiers, or file
   mutation server-side. The model emits valid tool calls; the *client* executes them.

## Architecture & request pipeline

```
request
  → router (trainable linear n-gram classifier + deterministic guardrails)
  → exactly ONE sparse expert (math | coding | translation | smalltalk | tool | factual | abstain)
  → answer with provenance / store evidence
  → harvest verified labels → retrain router → gate on evals → promote only if not worse
```

Two distinct MoE layers — keep them straight:
- **Architectural MoE (live today):** the router is the gating network; each *mode* is a sparse expert;
  exactly one runs per request and cheap experts short-circuit before expensive ones.
  See `internal/apiserver/server.go` and `internal/answerer/answerer.go`.
- **Neural MoE (forward-only, NOT trained — aspirational):** in `internal/model/transformer_v2.go`,
  when `num_experts > 1` each FFN becomes a DeepSeek-style sparse MoE (gating + top-k=2 + load-balancing
  aux-loss). Only the **forward** path (gating/top-k/aux-loss) is implemented and tested
  (`internal/model/moe_test.go`). **`TransformerV2` has no backprop**, is not wired to the training
  loop, and no large checkpoint exists — today only the tiny legacy `model.Model` is ever trained, and
  `safeGenerate` keeps generation gated off (`Step <= 0`) until a real checkpoint passes the eval
  gates. Per principle #5, growing a large neural model is **not** the plan; capability is meant to
  compound through the loop, not through `configs/aletheia-mikros-moe.yaml`'s 360M target.

## Key packages

| Package | Path | Role |
|---|---|---|
| router | `internal/router/router.go` | Linear n-gram classifier + deterministic guardrails (fallback) |
| answerer | `internal/answerer/answerer.go` | Parametric experts (smalltalk/math/coding/translation/abstain) |
| apiserver | `internal/apiserver/server.go` | OpenAI-compatible server; routing orchestration; expert metrics |
| apiserver | `internal/apiserver/knowledge.go` | Knowledge indexing, retrieval, learn-on-demand |
| model | `internal/model/transformer_v2.go` | Decoder: RoPE, RMSNorm, SwiGLU, optional neural MoE FFN |
| cognitivevm | `internal/cognitivevm/` | Verifier-first repair loop (beam/MCTS over action branches) |
| verifier | `internal/verifier/verifier.go` | Allowlisted no-shell checks (`go test`, `go vet`, parse) |
| memory | `internal/memory/memory.go` | SQLite store (modernc.org/sqlite): episodes, chunks, graph, jobs |
| retriever | `internal/retriever/retriever.go` | Hashing-vector retrieval (FNV, 64d, `hashing-v1:64`), memoized |
| research | `internal/research/` | SearXNG-backed web research pipeline (no paid APIs) |
| learning | `internal/learning/learning.go` | Harvest labels, retrain, gate-and-promote loop |
| config | `internal/config/config.go` | Typed YAML config (`NumExperts`, `TopKExperts`, …) |

Docs worth reading: `README.md`, `docs/architecture.md`, `docs/small-models-research.md` (the
small-model thesis + 8 papers), `docs/decisions.md` (26-milestone decision log).

## Tech stack & constraints

- **Go 1.26.3**, module `aletheia`. Only 2 direct deps: `gopkg.in/yaml.v3`, `modernc.org/sqlite`
  (pure-Go SQLite, **no cgo**). Keep it that way — do not add heavy/cgo/GPU dependencies.
- **CPU-only**, designed for a VPS. No external model calls for coding/math/smalltalk/translation.
- Embeddings are hashing-based (FNV, 64 dims) — no embedding models. Research is self-hosted SearXNG.
- Tensors, SGD/AdamW, RoPE/RMSNorm/SwiGLU are implemented from scratch in `internal/tensor`.

## Common commands

```bash
# Build / test
go build ./...
go test ./...
go vet ./...

# Init DB
go run ./cmd/aletheia init --db data/memory.sqlite

# Data → tokenizer → router
go run ./cmd/aletheia dataset build --profile mikros-live-v1 --out datasets/generated/mikros_live_v1.jsonl
go run ./cmd/aletheia tokenizer train --dataset datasets/generated/mikros_live_v1.jsonl --out checkpoints/aletheia-mikros/tokenizer.json
go run ./cmd/aletheia train-router --dataset datasets/router_mikros.jsonl --out checkpoints/router-mikros --val-split 0.2

# Solve (verifier-first repair, client-side only)
go run ./cmd/aletheia solve --task examples/buggy-go/task.json --trace

# Evals (gates — must not regress)
go run ./cmd/aletheia eval --suite evals/production --json
go run ./cmd/aletheia eval --suite evals/mikros_live --json

# Self-improving loop (harvest verified labels, retrain, gate, promote)
go run ./cmd/aletheia learn --db data/memory.sqlite --out datasets/generated

# Serve the OpenAI-compatible API
ALETHEIA_API_KEY=local-dev go run ./cmd/aletheia serve --checkpoint checkpoints/... --addr :8080
```

CLI subcommands: `config init train dataset tokenizer train-selector train-router run index ask
memory solve eval learn research research-status jobs serve inspect`.

Eval suites live in `evals/` (`production`, `mikros_live`, `mikros_artifact`, `mikros_functional`,
`bootstrap`). Configs in `configs/` (`tiny`, `micro`, `seed-10m`, `core-100m`, `aletheia-mikros*`,
`aletheia-mikros-moe`, `aletheia-hephaestus`).

## Working agreements for Claude

- **Respect the gates.** Any change touching router/model/experts must keep `evals/production` clean
  (zero hallucination / zero raw-chunk leakage / zero links-only / ~100% natural-answer) and not
  regress `mikros_live`. Run the relevant eval suite before claiming a behavioral change works.
- **Latency is a gate.** `internal/apiserver/latency_test.go` enforces avg < 25 ms/req (real ~0.5 ms).
  Don't add per-request work that breaks this; reuse the embedding memoization pattern.
- **Determinism.** Routing/training/eval paths avoid RNG where reproducibility matters (deterministic
  validation splits, deterministic top-k). Preserve this.
- **Match the codebase.** Idiomatic Go, standard-library-first, no new heavy deps. Mirror the existing
  package layout and naming.
- **When extending experts:** add the sparse expert + deterministic guardrail + eval cases together.
  An expert without a guardrail and eval coverage is incomplete.
