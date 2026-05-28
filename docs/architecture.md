# Aletheia-mu Architecture

Aletheia-mu starts as a local, verifier-first Go system. Milestone 0 proves the loop before adding a custom model: read a task, run an allowlisted verifier, apply a controlled patch, verify again, and persist evidence locally.

The first loop is intentionally small:

1. `cmd/aletheia` parses commands with the Go standard library.
2. `internal/verifier` runs only allowlisted commands, starting with `go test ./...`.
3. `internal/cognitivevm` coordinates task execution and the toy patch flow.
4. `internal/memory` stores episodes and evidence in SQLite.
5. `internal/tokenizer` provides byte-level tokens plus atomic functional tokens for later model work.

Later milestones can replace the dummy patcher with model-generated action candidates while keeping the verifier and evidence contract.

