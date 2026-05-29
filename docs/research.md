# Research

Aletheia research is an opt-in evidence pipeline. It uses local memory first, then internal SearXNG only when the user explicitly requests research or chat detects a knowledge gap and research auto mode is enabled.

Flow:

1. Route the chat/query intent.
2. Prefer a completed research answer when query terms overlap and confidence is high enough.
3. Search local memory.
4. If local evidence is sufficient, answer with citations.
5. If evidence is missing and research is enabled, queue a research job.
6. Search SearXNG, fetch sources, extract clean text, rank sources, extract claims, and store evidence.
7. Future `ask` or chat requests can answer from stored answers/chunks.

Research stores evidence, not just answers. Completed jobs keep a canonical answer, fetched pages are persisted into `documents`/`chunks`, source metadata goes into `web_sources`, claims go into `web_claims`, and graph nodes/edges record `web_page`, `web_source`, and `web_claim` relationships.

## Config

```yaml
research:
  enabled: true
  auto_on_knowledge_gap: true
  background_jobs_enabled: true
  provider: searxng
  searxng_url: "http://searxng:8080"
  max_sources: 5
  max_fetch_bytes: 1048576
  fetch_timeout_seconds: 10
  job_timeout_seconds: 120
  min_sources_for_verified: 2
  min_trust_score: 0.35
  store_raw_html: false
  user_agent: "AletheiaResearchBot/0.1"
```

Environment overrides:

```env
ALETHEIA_RESEARCH_ENABLED=true
ALETHEIA_RESEARCH_AUTO=true
ALETHEIA_SEARXNG_URL=http://searxng:8080
ALETHEIA_RESEARCH_MAX_SOURCES=5
```

## CLI

```bash
go run ./cmd/aletheia research \
  --query "what is MCP in agents?" \
  --background \
  --db data/memory.sqlite

go run ./cmd/aletheia jobs --db data/memory.sqlite
go run ./cmd/aletheia research-status --job <job_id> --db data/memory.sqlite
go run ./cmd/aletheia ask --query "what is MCP in agents?" --db data/memory.sqlite
```

## HTTP

```bash
curl https://llmlabs.app/v1/aletheia/research \
  -H "Authorization: Bearer $ALETHEIA_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"query":"what is MCP in agents?","mode":"background","max_sources":5}'
```

Chat accepts an optional extension:

```json
{
  "model": "aletheia-mikros",
  "messages": [{"role": "user", "content": "what is MCP in agents?"}],
  "aletheia": {"research": "background"}
}
```

## Policy

- Greetings and command help never trigger web search.
- Coding/repo repair tasks never web search by default.
- Single-source evidence is not treated as verified truth.
- Weak or conflicting evidence must produce uncertainty.
- Raw HTML is not stored unless explicitly enabled.
- Research never executes shell commands.
