# OpenCode Integration

Aletheia exposes an OpenAI-compatible Chat Completions API. For coding-agent
use, point OpenCode at the public Mikros model; coding/tool prompts route
internally:

```ts
import { createOpenAICompatible } from "@ai-sdk/openai-compatible"

export const aletheia = createOpenAICompatible({
  name: "aletheia",
  apiKey: process.env.ALETHEIA_API_KEY,
  baseURL: "https://api.llmlabs.app/v1",
})

export const model = aletheia("aletheia-mikros")
```

Use `aletheia-mikros` for coding tasks. Hidden specialist checkpoints or coding
profiles are implementation details and are not required in client config.

The public Aletheia API may return OpenAI-style `assistant.tool_calls` when the
client sends `tools`, but it never executes tools server-side. The coding agent
client remains responsible for filesystem edits, shell commands, and safety.
