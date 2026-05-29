# Aletheia-mu

Local verifier-first cognitive architecture in Go. The default path is deterministic and local: plan actions, build patch candidates, verify them with allowlisted tools, and persist evidence to SQLite.

Core commands:

```bash
go run ./cmd/aletheia init --db data/memory.sqlite
go run ./cmd/aletheia dataset build --profile mikros-v1 --out datasets/generated/mikros_v1.jsonl
go run ./cmd/aletheia dataset build --profile mikros-curriculum-v1 --out datasets/generated/mikros_curriculum_v1.jsonl
go run ./cmd/aletheia tokenizer train --dataset datasets/generated/mikros_v1.jsonl --out checkpoints/aletheia-mikros/tokenizer.json
go run ./cmd/aletheia train --config configs/aletheia-mikros-v1.yaml --dataset datasets/generated/mikros_v1.jsonl --out checkpoints/aletheia-mikros
go run ./cmd/aletheia train-selector --dataset datasets/selector_bootstrap.jsonl --out checkpoints/selector-bootstrap
go run ./cmd/aletheia solve --task examples/buggy-go/task.json --trace
go run ./cmd/aletheia eval --suite evals/bootstrap --json
go run ./cmd/aletheia eval --suite evals/production --json
go run ./cmd/aletheia eval --suite evals/mikros_functional --json
go run ./cmd/aletheia eval --suite evals/mikros_artifact --json
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
- `serve` for an OpenAI-compatible inference API around a local checkpoint.
- SearXNG-backed research can fill chat knowledge gaps; `research --query ...` remains available for manual ops.

OpenAI-compatible local API:

```bash
ALETHEIA_API_KEY=local-dev go run ./cmd/aletheia serve \
  --checkpoints-dir checkpoints \
  --model aletheia-mikros \
  --addr :8080
```

Python SDK:

```python
from openai import OpenAI

client = OpenAI(api_key="local-dev", base_url="http://localhost:8080/v1")
response = client.chat.completions.create(
    model="aletheia-mikros",
    messages=[{"role": "user", "content": "hola como estas?"}],
    max_tokens=64,
)
print(response.choices[0].message.content)
```

Node SDK:

```ts
import OpenAI from "openai";

const client = new OpenAI({
  apiKey: "local-dev",
  baseURL: "http://localhost:8080/v1",
});

const response = await client.chat.completions.create({
  model: "aletheia-mikros",
  messages: [{ role: "user", content: "hola como estas?" }],
  max_tokens: 64,
});
console.log(response.choices[0].message.content);
```

`aletheia-mikros` is the public model name. Internally it routes between chat,
coding, tool-call, research, and abstention behavior, but `/v1/models` presents
one product surface. The target is a verified small agent: local memory, SearXNG
research, coding knowledge, repair, and verifiers must beat guessing. `solve`
keeps its verifier-first flow and does not require serving a separate planner
checkpoint.

Mikros must answer in natural language first. Factual/current questions never
fall through to free generation: they use verified local/research evidence,
start research when enabled, or abstain. Coding prompts stay local and do not
reuse stale web chunks.

See [docs/testing.md](docs/testing.md) for the smoke suite, [docs/deploy.md](docs/deploy.md) for Dokploy deploy, and [docs/architecture.md](docs/architecture.md) for subsystem contracts.

For SearXNG-backed research setup, see [docs/research.md](docs/research.md) and [docs/deploy-dokploy-searxng.md](docs/deploy-dokploy-searxng.md).
