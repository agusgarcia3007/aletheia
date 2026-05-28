# Decisions

## Milestone 0

- Use `module aletheia` to avoid assuming a remote repository path.
- Use only the standard library for CLI parsing.
- Use `modernc.org/sqlite` for SQLite so the project stays pure Go and avoids cgo.
- Keep the first solver deterministic: it patches the known `examples/buggy-go` failure and proves the verifier/evidence loop.
- Resolve relative task repository paths from the process working directory.
- Reject unsupported success commands before execution.
- Keep `ALETHEIA_MU_PROMPT.md` as the source specification.

## Milestone 1

- Add a tiny CPU training path for automated acceptance and keep `seed-10m` as a manual config target.
- Use a deterministic causal next-token model with float32 tensors and manual gradients so overfit checks run quickly on CPU.
- Store checkpoints as `manifest.json` plus little-endian `weights.f32`.
- Keep generated checkpoints ignored by git.
