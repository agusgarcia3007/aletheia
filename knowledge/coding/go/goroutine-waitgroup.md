# Go: lanzar una goroutine con WaitGroup

Para lanzar una goroutine (goroutines) y esperar a que terminen en Go, se usa un waitgroup:

```go
var wg sync.WaitGroup
for _, item := range items {
    wg.Add(1)
    go func(it string) {
        defer wg.Done()
        process(it)
    }(it)
}
wg.Wait()
```

`wg.Add` antes de lanzar, `defer wg.Done()` dentro, y `wg.Wait()` bloquea hasta que todas terminen.
