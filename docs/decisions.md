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

## Milestone 2

- Route `solve` through an explicit Cognitive VM with functional action tokens and action trace.
- Keep the toy patcher bounded to `examples/buggy-go`, but delay file writes until `<ACT_VERIFY>`.
- Use a heuristic selector that can fall back safely when model candidates are missing or invalid.
- Allow optional model-backed planning from a checkpoint without requiring it for `solve`.

## Milestone 3

- Use a verifier bus so VM actions can run one or more required verifiers and aggregate evidence.
- Execute verifier commands without a shell and only through an exact allowlist.
- Keep `go_test` as the default required verifier; make `static_go_parse`, `go_vet`, and `go_test_race` opt-in.
- Store structured verifier payloads in the existing evidence table instead of adding a migration.
