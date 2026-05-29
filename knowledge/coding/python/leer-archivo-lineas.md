# Python: abrir un archivo y leer sus lineas

Para abrir un archivo de texto y leer sus lineas en Python:

```python
with open("datos.txt", encoding="utf-8") as f:
    for linea in f:
        print(linea.rstrip())
```

`with` cierra el archivo automaticamente. Iterar sobre `f` lee linea por linea sin cargar todo en memoria.
