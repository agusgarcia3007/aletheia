# OpenCode Integration

Aletheia exposes an OpenAI-compatible Chat Completions API. For coding-agent
use, configure OpenCode with the public Mikros model and the real context limit
served by the current checkpoint:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "aletheia": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "Aletheia",
      "options": {
        "baseURL": "https://api.llmlabs.app/v1",
        "apiKey": "{env:ALETHEIA_API_KEY}"
      },
      "models": {
        "aletheia-mikros": {
          "name": "Aletheia Mikros",
          "limit": { "context": 1024, "output": 256 }
        }
      }
    }
  },
  "model": "aletheia/aletheia-mikros"
}
```

Use `aletheia-mikros` for coding tasks. Hidden specialist checkpoints or coding
profiles are implementation details and are not required in client config.

The public Aletheia API may return OpenAI-style `assistant.tool_calls` when the
client sends `tools`, but it never executes tools server-side. The coding agent
client remains responsible for filesystem edits, shell commands, and safety.

Compatibility notes:

- Chat Completions accepts OpenAI client extension fields such as
  `stream_options`.
- `stream:true` is supported with Server-Sent Events and may emit either content
  chunks or `assistant.tool_calls` chunks.
- Aletheia never executes tools server-side. OpenCode executes filesystem and
  shell tools locally.
- Check `/healthz` for the live `context_length`, `max_output_tokens`,
  `supports_tools`, and tokenizer metadata before advertising larger limits.
