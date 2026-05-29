# OpenCode Integration

Aletheia exposes an OpenAI-compatible Chat Completions API. For coding-agent
use, point OpenCode at the coding specialist model:

```ts
import { createOpenAICompatible } from "@ai-sdk/openai-compatible"

export const aletheia = createOpenAICompatible({
  name: "aletheia",
  apiKey: process.env.ALETHEIA_API_KEY,
  baseURL: "https://api.llmlabs.app/v1",
})

export const model = aletheia("aletheia-hephaestus")
```

Use `aletheia-hephaestus` for coding tasks. `aletheia-mikros` remains the public
router model and may auto-route coding prompts to Hephaestus in the hosted chat.

The public Aletheia API may return OpenAI-style `assistant.tool_calls` when the
client sends `tools`, but it never executes tools server-side. The coding agent
client remains responsible for filesystem edits, shell commands, and safety.
