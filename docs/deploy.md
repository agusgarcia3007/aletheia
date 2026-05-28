# Deploy

## Dokploy

Aletheia can be deployed as an OpenAI-compatible inference API with Dokploy's Dockerfile build type.

Dokploy application settings:

- Build Type: `Dockerfile`
- Dockerfile Path: `Dockerfile`
- Docker Context Path: `.`
- Target Port: `8080`

Required service environment variables:

```env
ALETHEIA_ADDR=:8080
ALETHEIA_API_KEY=<your-api-key>
ALETHEIA_CHECKPOINT=/app/checkpoints/tiny-actions
ALETHEIA_MODEL=tiny-actions
```

The Dockerfile is self-contained for the default model: it builds the Go binary and trains the tiny bootstrap checkpoint inside the image. This avoids relying on local ignored `checkpoints/` content during deploy.

## API Smoke

```bash
curl https://your-domain.example/healthz

curl https://your-domain.example/v1/chat/completions \
  -H "Authorization: Bearer $ALETHEIA_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"tiny-actions","messages":[{"role":"user","content":"fix failing go test"}],"max_tokens":32}'
```

Python SDK:

```python
from openai import OpenAI

client = OpenAI(
    api_key="your-api-key",
    base_url="https://your-domain.example/v1",
)

response = client.chat.completions.create(
    model="tiny-actions",
    messages=[{"role": "user", "content": "fix failing go test"}],
    max_tokens=32,
)
print(response.choices[0].message.content)
```

## Security Boundary

The API server only exposes checkpoint inference through `/v1/models`, `/v1/chat/completions`, and `/v1/completions`. It does not expose `solve`, verifiers, shell execution, repository access, or file mutation.
