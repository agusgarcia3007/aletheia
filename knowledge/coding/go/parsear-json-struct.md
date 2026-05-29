# Go: parsear JSON a un struct

Para parsear (deserializar) JSON dentro de un struct en Go:

```go
type User struct {
    Name string `json:"name"`
    Age  int    `json:"age"`
}

var u User
if err := json.Unmarshal(data, &u); err != nil {
    return err
}
```

Las etiquetas `json:"..."` mapean campos; `Unmarshal` recibe un puntero al struct.
