# JavaScript: clonar un objeto profundo (deep clone)

Para clonar un objeto en profundidad en JavaScript moderno:

```js
const copia = structuredClone(original);
```

`structuredClone` copia objetos anidados sin referencias compartidas. Para casos simples sin funciones tambien sirve `JSON.parse(JSON.stringify(obj))`.
