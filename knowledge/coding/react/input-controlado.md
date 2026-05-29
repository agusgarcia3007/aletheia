# React: input controlado

Un input controlado en React liga su valor al estado:

```tsx
const [nombre, setNombre] = useState("");
return (
  <input value={nombre} onChange={(e) => setNombre(e.target.value)} />
);
```

El valor viene del estado y `onChange` lo actualiza: React es la unica fuente de verdad.
