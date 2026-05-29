# Aletheia

**A local, verifier-first cognitive architecture in Go — a small system that turns verification into intelligence.**

Aletheia (Greek *ἀλήθεια*, "truth / un-concealment"; **μ** = micro) is not a small language model trying to imitate a large one. It is a different bet about where intelligence should live. Frontier models put almost everything into parameters and hope the weights both *know* the world and *reason* about it. Aletheia splits the problem: a tiny model handles **intent, routing, and language**, while **truth lives outside the weights** — in tools, retrieval, verifiers, memory, and a learning loop that compounds over time.

The public product surface is a single model, `aletheia-mikros`, served behind an OpenAI-compatible API. Underneath, it is an agent.

---

## The thesis

> Useful intelligence = a small model + search + verifiers + causal memory + local tools + continuous learning + hypothesis selection.

A frontier model answers with fluency. **Aletheia answers with evidence.** The wager is that for a large and valuable class of tasks — code with tests, internal documentation, private research, auditable QA, anything where *"I don't know"* beats a confident fabrication — a small verified agent can be **more trustworthy per dollar** than a giant generalist, while running on a CPU.

The intelligence does not live in the weights. It lives in the loop:

```
request → route intent → pick expert → compute / retrieve / call tool / verify
        → answer with provenance → store evidence → harvest examples
        → retrain router/selector → gate on evals → promote only if better
```

---

## Why it is a game changer

Most "small model" projects lose because they ask a tiny network to do a giant network's job: memorize the world, guess facts, simulate an IDE, repair code blindly. Aletheia refuses that job. Five properties make it structurally different from a general chatbot:

1. **It does not hallucinate by construction.** Factual/world-knowledge questions can never fall through to free generation. They are routed — by deterministic guardrails that override the model — to verified evidence, to a research/learning job, or to honest abstention. In an out-of-distribution stress test of ~100 unseen prompts, hallucination, raw-context leakage, and links-only answers are all **zero**, with a 100% natural-answer rate.

2. **It learns on demand, and it sticks.** Ask it something it doesn't know — say, a Swift idiom — and it says *"I don't know that yet, I'm learning it"*, runs a background job that fetches and stores the answer as citable knowledge, and answers from memory the next time. The knowledge persists in SQLite across restarts. **Coverage grows from use, not from a hand-written dictionary.**

3. **Verification is the reward — and it's free.** The truth signal does not need an expensive judge model when the domain has reliable verifiers. `go test` passes or fails. A Go snippet type-checks or it doesn't. A citation is supported or it isn't. The code knowledge Aletheia ships and serves is checked in-process with the Go type checker, not just parsed.

4. **It improves itself with a promotion gate.** Every time a deterministic expert handles a request, that decision is a *verified label*. The server records these; `aletheia learn` harvests them, retrains the router on real traffic, and **promotes the new router only if it does not regress** on a held-out set. Real usage makes it better — and it can never ship a worse model.

5. **It is sparse and fast by design.** Routing is a mixture of experts: exactly one capability expert (math, coding, smalltalk, translation, tool-use, research, abstention) handles each request, and cheap experts short-circuit before expensive ones. On a CPU this is **sub-millisecond** per request (~0.5 ms, ~1.8–2.1k req/s in-process). The architecture also supports a **neural** MoE (DeepSeek-style top-k expert routing) in `TransformerV2` for when the parametric model is trained at scale.

A frontier model is better at open-ended conversation, long tool-free reasoning, and world knowledge without retrieval. Aletheia does not compete there, and says so. It competes on **trust, cost, privacy, latency, and auditability** — every action leaves evidence, every success becomes training data, and every reward is free in a verifiable domain.

| Question | General chatbot | Aletheia |
| --- | --- | --- |
| "Hi" | generates a greeting | local smalltalk expert |
| "Reverse a list in Python" | generates from weights | computes/retrieves a cited, verified example |
| "Who won X?" | may invent | memory / research / citation / abstain |
| "What's 15% of 200?" | token prediction | real arithmetic evaluator |
| "Analyze this repo" | simulates without tools | emits tool calls or declines honestly |
| "Fix these tests" | suggests a patch | patch + verifier + rollback (`solve`) |
| "Improve with use" | depends on external logs | exports trajectories, retrains, gates, promotes |

---

## Architecture

A chat request flows through a sparse, evidence-first pipeline. The first expert that can answer, answers; nothing falls through to noise.

- **Router** (`internal/router`) — the gating network. A trainable linear classifier over word/char n-grams with deterministic guardrails. Its honest generalization is measured against a held-out split; the guardrails carry correctness, so a weak router is only a hint.
- **Answerers** (`internal/answerer`) — parametric experts that resolve before any retrieval: smalltalk/identity, a **real math evaluator** (percentages, powers, roots, linear equations, parenthesized expressions — a recursive-descent parser, not a lookup table), short translation, and coding.
- **Knowledge & learn-on-demand** (`internal/apiserver/knowledge.go`, `internal/research`) — coding and factual gaps are answered from a citable corpus and from learned, persisted evidence; misses trigger a learning job instead of a fabrication. Shipped/learned Go is type-checked before being presented.
- **Verifiers** (`internal/verifier`) — allowlisted, no-shell checks (`go test`, static parse, vet, fuzz/bench) that gate any code change in `solve`.
- **Cognitive VM, repair, search, skills** (`internal/cognitivevm`, `repair`, `search`, `skills`) — the verifier-first repair loop: deterministic patch candidates, beam/MCTS over actions, verified-trajectory compression into reusable skills.
- **Memory** (`internal/memory`) — SQLite (pure Go, no cgo): episodes, evidence, document chunks, a causal graph, search trajectories, selector/router examples, and learned knowledge.
- **Model** (`internal/model`) — a legacy micro-model plus `TransformerV2` (RoPE, RMSNorm, SwiGLU) with an optional **Mixture-of-Experts** feed-forward (`num_experts`, `top_k_experts`). Forward, gating, top-k routing, and the load-balancing aux loss are implemented; training at scale is the next compute step. Until a checkpoint passes the eval gates, raw generation is gated off and the system abstains honestly rather than emit byte-model noise.
- **Learning loop** (`internal/learning`) — harvests verified labels from real usage, retrains the router/selector, runs eval before/after, and promotes only on improvement.
- **API server** (`internal/apiserver`) — OpenAI-compatible `/v1/chat/completions`, `/v1/completions`, `/v1/models`, plus `/readyz` and `/metrics` (including the per-expert routing distribution). It serves inference only: no server-side shell, no repository mutation.

---

## Research lineage

Aletheia is an engineering synthesis of lessons the literature keeps repeating about small models:

- **Textbooks Are All You Need** (Phi) — data quality beats raw scale; curate, don't scrape.
- **TinyStories** — tiny models are coherent only in controlled domains; give each expert explicit limits.
- **SmolLM2** — competitive small models are data-centric; the dataset and the evals *are* the product.
- **MobileLLM** — sub-billion models win on-device through latency, cost, and tool use, not brute force.
- **Distilling Step-by-Step** — store rationales and trajectories, not just final answers.
- **Retrieval-Augmented Generation** — changing facts belong in memory with a source and a date, not in weights.
- **Toolformer / ReAct / Gorilla** — the model should *request* valid tools, not talk about them.
- **CodeRL / DeepSeek-R1** — the most valuable reward needs no judge model when the domain has trustworthy verifiers; and sparse MoE makes large capacity cheap.

See [`docs/small-models-research.md`](docs/small-models-research.md) for the full technical thesis, [`docs/architecture.md`](docs/architecture.md) for subsystem contracts, and [`docs/decisions.md`](docs/decisions.md) for the milestone log.

---

## What it is — and is not

**It can be superior at:** small/medium repos with tests, internal documentation, private SearXNG-backed research, cited QA, repeatable tasks compressed into skills, and any workflow where privacy, low cost, and "I don't know" matter.

**It does not claim superiority at:** open-ended creative writing, long tool-free reasoning, world knowledge without retrieval, autonomous coding over large repos without context, or general replacement of frontier models.

The neural model is intentionally gated until it is trained and proven by evals. Today the product surface is a fast, honest, self-improving verified agent; the trained generative core is additive, not load-bearing.

---

## Quickstart

```bash
# Initialize local memory
go run ./cmd/aletheia init --db data/memory.sqlite

# Build datasets, tokenizer, and the public artifact (no paid APIs)
go run ./cmd/aletheia dataset build --profile mikros-live-v1 --out datasets/generated/mikros_live_v1.jsonl
go run ./cmd/aletheia tokenizer train --dataset datasets/generated/mikros_live_v1.jsonl --out checkpoints/aletheia-mikros/tokenizer.json

# Train the router with a held-out validation split and feature pruning
go run ./cmd/aletheia train-router --dataset datasets/router_mikros.jsonl --out checkpoints/router-mikros --val-split 0.2

# Verifier-first repair on a buggy repo
go run ./cmd/aletheia solve --task examples/buggy-go/task.json --trace

# Evaluation gates
go run ./cmd/aletheia eval --suite evals/production --json
go run ./cmd/aletheia eval --suite evals/mikros_live --json

# Close the learning loop: harvest real-usage labels, retrain, promote only if better
go run ./cmd/aletheia learn --db data/memory.sqlite --out datasets/generated \
  --train-router-out checkpoints/router-mikros --suite evals/mikros_live
```

### Serve (OpenAI-compatible)

```bash
ALETHEIA_API_KEY=local-dev go run ./cmd/aletheia serve \
  --checkpoints-dir checkpoints \
  --model aletheia-mikros \
  --router-checkpoint checkpoints/router-mikros \
  --knowledge knowledge \
  --addr :8080
```

```python
from openai import OpenAI

client = OpenAI(api_key="local-dev", base_url="http://localhost:8080/v1")
print(client.chat.completions.create(
    model="aletheia-mikros",
    messages=[{"role": "user", "content": "how do I reverse a list in Python?"}],
).choices[0].message.content)
```

`/metrics` exposes the sparse-expert routing distribution (`aletheia_expert_total{expert=...}`); `/readyz` reflects memory and research state. For research and deployment, see [`docs/research.md`](docs/research.md), [`docs/deploy.md`](docs/deploy.md), and [`docs/testing.md`](docs/testing.md).

---

Aletheia is not a small model pretending to be a giant. It is a small **system** that converts verification into learning. That is the game changer.
