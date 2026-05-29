# React: useEffect para fetch de datos

Para hacer fetch de datos con useEffect en React:

```tsx
useEffect(() => {
  let activo = true;
  fetch("/api/users")
    .then((r) => r.json())
    .then((data) => { if (activo) setUsers(data); });
  return () => { activo = false; };
}, []);
```

El array vacio ejecuta una sola vez al montar; el cleanup evita setState tras desmontar.
