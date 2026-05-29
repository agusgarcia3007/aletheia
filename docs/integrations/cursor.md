# Cursor Integration

When a Cursor build supports a custom OpenAI-compatible provider, configure:

```text
Base URL: https://api.llmlabs.app/v1
API key:  <your ALETHEIA_API_KEY>
Model:    aletheia-mikros
```

Use `aletheia-mikros` for chat and coding prompts. Mikros routes internally
between chat, coding, tool-call, research, and abstention behavior.

Compatibility caveat: Cursor's own Composer/apply/tab features may use internal
providers that do not honor arbitrary external OpenAI-compatible endpoints. In
that case Aletheia still works through any Cursor surface that sends normal Chat
Completions requests to the configured Base URL.
