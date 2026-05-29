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
ALETHEIA_CHECKPOINTS_DIR=/app/checkpoints
ALETHEIA_MODEL=aletheia-mikros
```

The Dockerfile is self-contained for the default API model: it builds the Go
binary, generates the Mikros V1 bootstrap dataset, and writes a public
`aletheia-mikros` checkpoint plus any hidden internal specialist checkpoints
needed by the router. By default `ALETHEIA_TRAIN_STEPS=0`, so deploys create
bootstrap artifacts without long retraining. For a trained image, set the Docker
build arg `ALETHEIA_TRAIN_STEPS` or train locally and deploy/mount the resulting
`checkpoints/` directory. `/v1/models` should show `aletheia-mikros`; coding
prompts route internally.

## API Smoke

```bash
curl https://your-domain.example/healthz

curl https://your-domain.example/v1/chat/completions \
  -H "Authorization: Bearer $ALETHEIA_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"aletheia-mikros","messages":[{"role":"user","content":"hola como estas?"}],"max_tokens":64}'
```

Python SDK:

```python
from openai import OpenAI

client = OpenAI(
    api_key="your-api-key",
    base_url="https://your-domain.example/v1",
)

response = client.chat.completions.create(
    model="aletheia-mikros",
    messages=[{"role": "user", "content": "hola como estas?"}],
    max_tokens=64,
)
print(response.choices[0].message.content)
```

## Security Boundary

The API server only exposes checkpoint inference through `/v1/models`, `/v1/chat/completions`, and `/v1/completions`. It does not expose `solve`, verifiers, shell execution, repository access, or file mutation.

For internal SearXNG-backed research, see [deploy-dokploy-searxng.md](deploy-dokploy-searxng.md).
