# Rust Basic Snippets

```rust
fn main() {
    let name = "Aletheia";
    println!("Hola, {name}");
}
```

Rust is statically typed. Functions declare parameter and return types:

```rust
fn add(a: i32, b: i32) -> i32 {
    a + b
}
```

The final expression without a semicolon is returned.
