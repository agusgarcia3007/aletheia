# Rust: leer un archivo de texto completo

Para leer un archivo de texto completo a un String en Rust:

```rust
use std::fs;

let contenido = fs::read_to_string("datos.txt")?;
println!("{contenido}");
```

`fs::read_to_string` devuelve un `Result`; el `?` propaga el error si falla.
