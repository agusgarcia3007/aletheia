# SQL: join entre pedidos y clientes

Para combinar pedidos con sus clientes en SQL:

```sql
SELECT p.id, c.nombre, p.total
FROM pedidos p
JOIN clientes c ON c.id = p.cliente_id
ORDER BY p.total DESC;
```

`JOIN ... ON` une por la clave foranea. Usa `LEFT JOIN` si queres incluir pedidos sin cliente.
