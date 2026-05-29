# React Basic Snippets

```tsx
type GreetingCardProps = {
  name: string
}

export function GreetingCard({ name }: GreetingCardProps) {
  return <section><h2>Hola, {name}</h2></section>
}
```

Keep components small, typed, and explicit about props. For repo changes,
inspect the existing component style before editing.
