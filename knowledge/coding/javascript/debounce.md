# JavaScript: como hacer un debounce

Un debounce retrasa la ejecucion de una funcion hasta que pasa un tiempo sin llamadas:

```js
function debounce(fn, delay = 300) {
  let timer;
  return (...args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), delay);
  };
}
```

Util para inputs de busqueda o resize: evita ejecutar en cada evento.
