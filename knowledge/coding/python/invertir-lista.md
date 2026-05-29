# Python: invertir una lista (revertir / dar vuelta una lista)

Para invertir una lista en Python tenés tres formas comunes:

```python
nums = [1, 2, 3, 4]
invertida = nums[::-1]        # copia invertida
invertida = list(reversed(nums))  # iterador invertido
nums.reverse()                # invierte in-place
```

`[::-1]` y `reversed()` devuelven una nueva lista; `.reverse()` modifica la original.
