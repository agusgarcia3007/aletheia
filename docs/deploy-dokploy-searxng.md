# Dokploy SearXNG Research Deploy

Run two services in the same Dokploy environment:

- `aletheia`
- `searxng`

SearXNG does not need to be public. Aletheia reaches it through the internal service DNS name.

## Aletheia Env

```env
ALETHEIA_API_KEY=<your-api-key>
ALETHEIA_ADDR=:8080
ALETHEIA_RESEARCH_ENABLED=true
ALETHEIA_RESEARCH_AUTO=true
ALETHEIA_SEARXNG_URL=http://searxng:8080
ALETHEIA_RESEARCH_MAX_SOURCES=5
```

If the Dokploy service name is not `searxng`, change `ALETHEIA_SEARXNG_URL` to the actual internal service URL.

## Smoke

```bash
curl https://llmlabs.app/v1/aletheia/research \
  -H "Authorization: Bearer $ALETHEIA_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"query":"what is MCP in agents?","mode":"background","max_sources":5}'
```

Then check:

```bash
curl https://llmlabs.app/v1/aletheia/jobs \
  -H "Authorization: Bearer $ALETHEIA_API_KEY"
```

## Security

- Keep SearXNG private/internal.
- Keep Aletheia behind bearer auth.
- Fetching is bounded by domain policy, timeout, max bytes, redirects, and content type.
- Research does not run shell commands or repository tools.
