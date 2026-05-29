# Python: funcion que dice si un numero es primo

Una funcion para chequear si un numero entero es primo en Python:

```python
def es_primo(n: int) -> bool:
    if n < 2:
        return False
    for d in range(2, int(n ** 0.5) + 1):
        if n % d == 0:
            return False
    return True
```

Solo hace falta probar divisores hasta la raiz cuadrada de n.
