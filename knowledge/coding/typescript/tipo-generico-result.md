# TypeScript: tipo generico Result

Un tipo generico Result para representar exito o error en TypeScript:

```ts
type Result<T, E = Error> =
  | { ok: true; value: T }
  | { ok: false; error: E };

function ok<T>(value: T): Result<T> { return { ok: true, value }; }
function err<E>(error: E): Result<never, E> { return { ok: false, error }; }
```

El discriminante `ok` permite a TypeScript estrechar el tipo en cada rama.
