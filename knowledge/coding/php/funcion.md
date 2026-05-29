# PHP: ejemplo de funcion

Una funcion simple en PHP:

```php
function suma(int $a, int $b): int {
    return $a + $b;
}

echo suma(2, 3); // 5
```

Desde PHP 7 podes tipar parametros y retorno. `echo` imprime el resultado.
