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
ALETHEIA_CHECKPOINT=/app/checkpoints/aletheia-chat-basic
ALETHEIA_MODEL=aletheia-chat-basic
```

The Dockerfile is self-contained for the default API model: it builds the Go binary and trains both `aletheia-chat-basic` and `tiny-actions` inside the image. Dokploy serves `aletheia-chat-basic` by default. If you want to expose planner-token inference instead, override `ALETHEIA_CHECKPOINT=/app/checkpoints/tiny-actions` and `ALETHEIA_MODEL=tiny-actions`.

## API Smoke

```bash
curl https://your-domain.example/healthz

curl https://your-domain.example/v1/chat/completions \
  -H "Authorization: Bearer $ALETHEIA_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"aletheia-chat-basic","messages":[{"role":"user","content":"hola como estas?"}],"max_tokens":64}'
```

Python SDK:

```python
from openai import OpenAI

client = OpenAI(
    api_key="your-api-key",
    base_url="https://your-domain.example/v1",
)

response = client.chat.completions.create(
    model="aletheia-chat-basic",
    messages=[{"role": "user", "content": "hola como estas?"}],
    max_tokens=64,
)
print(response.choices[0].message.content)
```

## Security Boundary

The API server only exposes checkpoint inference through `/v1/models`, `/v1/chat/completions`, and `/v1/completions`. It does not expose `solve`, verifiers, shell execution, repository access, or file mutation.
