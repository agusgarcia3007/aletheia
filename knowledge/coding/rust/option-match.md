# Rust: manejar un Option con match

Para manejar un valor Option con match en Rust:

```rust
let maybe: Option<i32> = Some(5);
match maybe {
    Some(n) => println!("valor: {n}"),
    None => println!("sin valor"),
}
```

`match` obliga a cubrir `Some` y `None`. Alternativas: `if let Some(n) = maybe` o `maybe.unwrap_or(0)`.
