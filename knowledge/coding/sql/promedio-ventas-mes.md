# SQL: promedio de ventas por mes

Para sacar el promedio de ventas agrupado por mes en SQL:

```sql
SELECT date_trunc('month', fecha) AS mes, AVG(total) AS promedio
FROM ventas
GROUP BY mes
ORDER BY mes;
```

`AVG` calcula el promedio y `GROUP BY mes` lo agrupa por periodo.
