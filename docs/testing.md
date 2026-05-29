# Testing

Base checks after every milestone:

```bash
go test -count=1 ./...
go run ./cmd/aletheia eval --suite evals/bootstrap
go run ./cmd/aletheia eval --suite evals/bootstrap --json
go run ./cmd/aletheia eval --suite evals/production --json
go run ./cmd/aletheia config inspect --config configs/micro.yaml
```

Smoke solve:

```bash
tmp=$(mktemp -d)
mkdir -p "$tmp/examples"
cp -R examples/buggy-go "$tmp/examples/buggy-go"

cat > "$tmp/task.json" <<EOF
{"goal":"Fix the Go project so all tests pass.","repo":"$tmp/examples/buggy-go","success":"go test ./..."}
EOF

go run ./cmd/aletheia solve \
  --task "$tmp/task.json" \
  --db "$tmp/memory.sqlite" \
  --verifier static_go_parse,go_test \
  --trace

grep -n "return a + b" "$tmp/examples/buggy-go/calculator.go"
go run ./cmd/aletheia memory inspect --db "$tmp/memory.sqlite"
go run ./cmd/aletheia memory graph --db "$tmp/memory.sqlite" --type patch_candidate
go run ./cmd/aletheia memory skills --db "$tmp/memory.sqlite"
```

MCTS smoke:

```bash
go run ./cmd/aletheia solve \
  --task "$tmp/task.json" \
  --db "$tmp/memory.sqlite" \
  --search mcts \
  --verifier static_go_parse,go_test \
  --trace
```

Learning smoke:

```bash
go run ./cmd/aletheia learn \
  --db "$tmp/memory.sqlite" \
  --suite evals/bootstrap \
  --out "$tmp/generated"
```

Costly verifier smoke, opt-in only:

```bash
go run ./cmd/aletheia solve \
  --task "$tmp/task.json" \
  --db "$tmp/memory.sqlite" \
  --bench \
  --trace
```

Production API smoke:

```bash
curl https://api.llmlabs.app/healthz
curl https://api.llmlabs.app/readyz
curl https://api.llmlabs.app/metrics
curl https://api.llmlabs.app/v1/aletheia/jobs \
  -H "Authorization: Bearer local-dev"
curl https://api.llmlabs.app/v1/chat/completions \
  -H "Authorization: Bearer local-dev" \
  -H "Content-Type: application/json" \
  -d '{"model":"aletheia-mikros","messages":[{"role":"user","content":"que es un MCP?"}],"max_tokens":128}'
curl https://api.llmlabs.app/v1/chat/completions \
  -H "Authorization: Bearer local-dev" \
  -H "Content-Type: application/json" \
  -d '{"model":"aletheia-mikros","messages":[{"role":"user","content":"que fue la guerra de vietnam?"}],"max_tokens":128}'
```
