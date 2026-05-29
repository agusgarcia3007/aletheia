package answerer

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"aletheia/internal/router"
)

type Reason string

type Request struct {
	Query    string
	Messages []router.Message
	Intent   router.Intent
	HasTools bool
}

type Response struct {
	Content string
	Intent  router.Intent
	Reason  Reason
}

type Answerer interface {
	CanAnswer(ctx context.Context, req Request) (float64, Reason)
	Answer(ctx context.Context, req Request) (Response, error)
}

type Composite struct {
	Answerers []Answerer
}

func Default() Composite {
	return Composite{Answerers: []Answerer{
		ToolAgentAnswerer{},
		SmalltalkAnswerer{},
		MathAnswerer{},
		TranslationAnswerer{},
		CodingAnswerer{},
		AbstainAnswerer{},
	}}
}

func (c Composite) Answer(ctx context.Context, req Request) (Response, bool, error) {
	var best Answerer
	var bestScore float64
	var bestReason Reason
	for _, answerer := range c.Answerers {
		score, reason := answerer.CanAnswer(ctx, req)
		if score > bestScore {
			best = answerer
			bestScore = score
			bestReason = reason
		}
	}
	if best == nil || bestScore < 0.5 {
		return Response{}, false, nil
	}
	resp, err := best.Answer(ctx, req)
	if resp.Reason == "" {
		resp.Reason = bestReason
	}
	return resp, true, err
}

type SmalltalkAnswerer struct{}

func (SmalltalkAnswerer) CanAnswer(_ context.Context, req Request) (float64, Reason) {
	if req.Intent == router.IntentSmalltalk {
		return 0.95, "smalltalk intent"
	}
	return 0, ""
}

func (SmalltalkAnswerer) Answer(_ context.Context, req Request) (Response, error) {
	text := normalize(req.Query)
	content := "Hola. Soy Aletheia Mikros: un agente local pequeño, verificable y experimental. Puedo ayudar con codigo, math simple, traducciones cortas, tools tipo OpenCode y preguntas con evidencia."
	switch {
	case hasAny(text, "gracias", "thanks"):
		content = "De nada. Puedo seguir con codigo, pruebas, research con evidencia o comandos de Aletheia."
	case hasAny(text, "chau", "adios", "bye"):
		content = "Chau. Cuando vuelvas, puedo ayudarte con tareas verificables y respuestas con evidencia."
	case hasAny(text, "que puedes hacer", "que podes hacer", "que sabes hacer", "help", "ayuda"):
		content = "Puedo conversar de forma básica y hacer cinco cosas bien: responder codigo pequeño, usar tools de agente, calcular operaciones simples, traducir frases cortas y contestar hechos sólo con evidencia o research."
	case hasAny(text, "quien sos", "quien eres", "what are you", "who are you"):
		content = "Soy Aletheia Mikros, la interfaz pública de Aletheia: pequeña, local y orientada a verificación antes que a inventar."
	}
	return Response{Content: content, Intent: router.IntentSmalltalk, Reason: "smalltalk answerer"}, nil
}

type ToolAgentAnswerer struct{}

func (ToolAgentAnswerer) CanAnswer(_ context.Context, req Request) (float64, Reason) {
	if req.Intent == router.IntentRepoAgent && !req.HasTools {
		return 0.9, "repo agent without client tools"
	}
	return 0, ""
}

func (ToolAgentAnswerer) Answer(_ context.Context, req Request) (Response, error) {
	return Response{
		Content: "Para analizar o reparar un repo necesito herramientas locales del cliente o `aletheia solve`. Desde chat público no ejecuto comandos ni edito archivos en el servidor.",
		Intent:  router.IntentRepoAgent,
		Reason:  "repo agent boundary",
	}, nil
}

type MathAnswerer struct{}

func (MathAnswerer) CanAnswer(_ context.Context, req Request) (float64, Reason) {
	if req.Intent == router.IntentMath && parseArithmetic(req.Query).ok {
		return 0.95, "simple arithmetic"
	}
	return 0, ""
}

func (MathAnswerer) Answer(_ context.Context, req Request) (Response, error) {
	op := parseArithmetic(req.Query)
	if !op.ok {
		return Response{Content: "Puedo calcular operaciones simples si me das una expresión concreta, por ejemplo `17 por 23`.", Intent: router.IntentMath, Reason: "math needs expression"}, nil
	}
	var result int
	switch op.op {
	case "*":
		result = op.a * op.b
	case "+":
		result = op.a + op.b
	case "-":
		result = op.a - op.b
	case "/":
		if op.b == 0 {
			return Response{Content: "No puedo dividir por cero.", Intent: router.IntentMath, Reason: "division by zero"}, nil
		}
		return Response{Content: fmt.Sprintf("%d dividido %d es %.4g.", op.a, op.b, float64(op.a)/float64(op.b)), Intent: router.IntentMath, Reason: "simple division"}, nil
	}
	return Response{Content: fmt.Sprintf("%d %s %d = %d.", op.a, op.symbol, op.b, result), Intent: router.IntentMath, Reason: "simple arithmetic"}, nil
}

type arithmetic struct {
	a, b   int
	op     string
	symbol string
	ok     bool
}

func parseArithmetic(text string) arithmetic {
	normalized := normalize(text)
	re := regexp.MustCompile(`(-?\d+)\s*(por|x|\*|\+|mas|más|menos|-|dividido|\/)\s*(-?\d+)`)
	matches := re.FindStringSubmatch(normalized)
	if len(matches) != 4 {
		return arithmetic{}
	}
	a, _ := strconv.Atoi(matches[1])
	b, _ := strconv.Atoi(matches[3])
	opWord := matches[2]
	switch opWord {
	case "por", "x", "*":
		return arithmetic{a: a, b: b, op: "*", symbol: "por", ok: true}
	case "+", "mas", "más":
		return arithmetic{a: a, b: b, op: "+", symbol: "más", ok: true}
	case "-", "menos":
		return arithmetic{a: a, b: b, op: "-", symbol: "menos", ok: true}
	case "/", "dividido":
		return arithmetic{a: a, b: b, op: "/", symbol: "dividido por", ok: true}
	default:
		return arithmetic{}
	}
}

type TranslationAnswerer struct{}

func (TranslationAnswerer) CanAnswer(_ context.Context, req Request) (float64, Reason) {
	if req.Intent == router.IntentTranslation {
		return 0.9, "translation intent"
	}
	return 0, ""
}

func (TranslationAnswerer) Answer(_ context.Context, req Request) (Response, error) {
	text := strings.TrimSpace(req.Query)
	payload := text
	if idx := strings.Index(text, ":"); idx >= 0 {
		payload = strings.TrimSpace(text[idx+1:])
	}
	normalized := normalize(payload)
	translations := map[string]string{
		"no tengo evidencia suficiente": "I do not have enough evidence.",
		"hola como estas":               "Hello, how are you?",
		"gracias":                       "Thank you.",
	}
	if value, ok := translations[normalized]; ok {
		return Response{Content: value, Intent: router.IntentTranslation, Reason: "phrase translation"}, nil
	}
	if len([]rune(payload)) <= 120 {
		return Response{Content: "Puedo traducir frases cortas comunes, pero no quiero inventar una traducción si no la tengo clara. Pasame una frase simple en español o inglés.", Intent: router.IntentTranslation, Reason: "translation abstain"}, nil
	}
	return Response{Content: "La frase es demasiado larga para mi traductor local básico. Dividila en una oración corta.", Intent: router.IntentTranslation, Reason: "translation too long"}, nil
}

type CodingAnswerer struct{}

func (CodingAnswerer) CanAnswer(_ context.Context, req Request) (float64, Reason) {
	if req.Intent == router.IntentCodingHelp || req.Intent == router.IntentCodeGeneration {
		return 0.92, "coding intent"
	}
	return 0, ""
}

func (CodingAnswerer) Answer(_ context.Context, req Request) (Response, error) {
	query := normalize(req.Query)
	slots := detectCodingSlots(query)
	if slots.Language == "" {
		return Response{Content: "Puedo ayudar con codigo, pero necesito el lenguaje y el objetivo. Ejemplo: `en Python, lee un CSV y filtra filas por estado`.", Intent: router.IntentCodingHelp, Reason: "missing coding language"}, nil
	}
	content := codingResponse(slots)
	if content == "" {
		content = genericCodingResponse(slots)
	}
	return Response{Content: content, Intent: req.Intent, Reason: "parametric coding answer"}, nil
}

type codingSlots struct {
	Language string
	Task     string
	Subject  string
}

func detectCodingSlots(query string) codingSlots {
	s := codingSlots{}
	if hasAny(query, "diferencia", "diferencias", "compar", " vs ", "entre", "enter") &&
		hasAny(query, "python") &&
		hasAny(query, "javascript", " js ", "javacript") {
		return codingSlots{Language: "python_javascript", Task: "compare"}
	}
	switch {
	case hasAny(query, "python"):
		s.Language = "python"
	case hasAny(query, "sql", "query"):
		s.Language = "sql"
	case hasAny(query, "react", "componente", "component"):
		s.Language = "react"
	case hasAny(query, "typescript", " ts "):
		s.Language = "typescript"
	case hasAny(query, "javascript", "javacript", " js ", "java script"):
		s.Language = "javascript"
	case hasAny(query, "rust"):
		s.Language = "rust"
	case hasAny(query, "golang", " go "):
		s.Language = "go"
	}
	switch {
	case hasAny(query, "csv"):
		s.Task = "read_csv_filter"
	case hasAny(query, "count", "contar", "por pais", "group"):
		s.Task = "count_group"
	case hasAny(query, "error", "errores", "errors"):
		s.Task = "errors"
	case hasAny(query, "map", "filter", "filtrar"):
		s.Task = "map_filter"
	case hasAny(query, "producto", "precio", "boton", "button"):
		s.Task = "product_card"
	case hasAny(query, "funcion", "function", "sume", "sumar", "add"):
		s.Task = "function"
	case hasAny(query, "diferencia", "compar", " vs ", "entre"):
		s.Task = "compare"
	default:
		s.Task = "intro"
	}
	return s
}

func codingResponse(s codingSlots) string {
	switch {
	case s.Language == "python" && s.Task == "read_csv_filter":
		return "En Python podés leer un CSV con la librería estándar y filtrar filas así:\n\n```python\nimport csv\n\nwith open(\"users.csv\", newline=\"\", encoding=\"utf-8\") as f:\n    rows = csv.DictReader(f)\n    active = [row for row in rows if row.get(\"status\") == \"active\"]\n\nprint(active)\n```\n\nSi el archivo es grande, conviene iterar fila por fila en vez de guardar todo en memoria."
	case s.Language == "sql" && s.Task == "count_group":
		return "Para contar usuarios por país en SQL:\n\n```sql\nSELECT pais, COUNT(*) AS total\nFROM usuarios\nGROUP BY pais\nORDER BY total DESC;\n```\n\nCambiá `usuarios` y `pais` por los nombres reales de tu tabla y columna."
	case s.Language == "go" && s.Task == "errors":
		return "En Go los errores se devuelven como valores y se chequean explícitamente:\n\n```go\nvalue, err := doWork()\nif err != nil {\n    return fmt.Errorf(\"do work: %w\", err)\n}\n```\n\nUsá `%w` para envolver errores cuando querés conservar la causa original."
	case s.Language == "rust" && s.Task == "map_filter":
		return "En Rust `filter` deja pasar elementos y `map` transforma cada uno:\n\n```rust\nlet nums = vec![1, 2, 3, 4, 5];\nlet doubled_even: Vec<i32> = nums\n    .into_iter()\n    .filter(|n| n % 2 == 0)\n    .map(|n| n * 2)\n    .collect();\n```\n\n`filter` recibe una condición; `map` produce el nuevo valor."
	case s.Language == "react" && s.Task == "product_card":
		return "Un componente React para una tarjeta de producto puede ser:\n\n```tsx\ntype ProductCardProps = {\n  name: string\n  price: number\n  onAdd: () => void\n}\n\nexport function ProductCard({ name, price, onAdd }: ProductCardProps) {\n  return (\n    <article className=\"product-card\">\n      <h3>{name}</h3>\n      <p>${price.toFixed(2)}</p>\n      <button onClick={onAdd}>Agregar</button>\n    </article>\n  )\n}\n```\n\nSi usás Tailwind o shadcn, te lo adapto a ese estilo."
	case s.Language == "python_javascript" && s.Task == "compare":
		return "Python y JavaScript sirven para cosas distintas:\n\n- Python: scripts, datos, automatización, backend e IA.\n- JavaScript: navegador, UI web y backend con Node/Bun/Deno.\n\nSi tu prioridad es UI web, elegí JavaScript o TypeScript. Si querés automatizar o trabajar con datos, Python suele ser más directo."
	case s.Language == "javascript" && s.Task == "function":
		return "Una función simple en JavaScript:\n\n```js\nfunction add(a, b) {\n  return a + b;\n}\n```\n\nJavaScript no exige tipos; validá entradas si vienen de usuarios o APIs."
	case s.Language == "typescript" && s.Task == "function":
		return "Una función simple en TypeScript:\n\n```ts\nfunction add(a: number, b: number): number {\n  return a + b;\n}\n```\n\nLos tipos ayudan a detectar errores antes de ejecutar."
	}
	return ""
}

func genericCodingResponse(s codingSlots) string {
	descriptions := map[string]string{
		"python":     "Python es práctico para scripts, datos, automatización y backend.",
		"javascript": "JavaScript corre en el navegador y en runtimes como Node, Bun o Deno.",
		"typescript": "TypeScript es JavaScript con tipos estáticos.",
		"go":         "Go es simple, compilado y muy usado para APIs y herramientas.",
		"rust":       "Rust apunta a rendimiento y seguridad de memoria sin garbage collector.",
		"react":      "React permite construir UI con componentes declarativos.",
		"sql":        "SQL sirve para consultar y transformar datos relacionales.",
	}
	examples := map[string]string{
		"python":     "```python\ndef add(a: int, b: int) -> int:\n    return a + b\n```",
		"javascript": "```js\nfunction add(a, b) {\n  return a + b;\n}\n```",
		"typescript": "```ts\nfunction add(a: number, b: number): number {\n  return a + b;\n}\n```",
		"go":         "```go\nfunc Add(a, b int) int {\n    return a + b\n}\n```",
		"rust":       "```rust\nfn add(a: i32, b: i32) -> i32 {\n    a + b\n}\n```",
		"react":      "```tsx\ntype GreetingCardProps = { name: string }\n\nexport function GreetingCard({ name }: GreetingCardProps) {\n  return <section><h2>Hola, {name}</h2></section>\n}\n```",
		"sql":        "```sql\nSELECT * FROM usuarios LIMIT 10;\n```",
	}
	desc := descriptions[s.Language]
	if desc == "" {
		desc = "Puedo ayudar con ese lenguaje si me das el objetivo concreto."
	}
	example := examples[s.Language]
	if example == "" {
		return desc
	}
	return desc + "\n\nEjemplo mínimo:\n\n" + example
}

type AbstainAnswerer struct{}

func (AbstainAnswerer) CanAnswer(_ context.Context, req Request) (float64, Reason) {
	if req.Intent == router.IntentAbstain {
		return 0.9, "abstain intent"
	}
	return 0, ""
}

func (AbstainAnswerer) Answer(_ context.Context, _ Request) (Response, error) {
	return Response{Content: "No tengo evidencia suficiente para afirmarlo de forma confiable. Prefiero abstenerme o buscar fuentes verificables si research está habilitado.", Intent: router.IntentAbstain, Reason: "safe abstention"}, nil
}

func normalize(text string) string {
	return router.Normalize(text)
}

func hasAny(text string, needles ...string) bool {
	padded := " " + text + " "
	for _, needle := range needles {
		if strings.Contains(padded, needle) || strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
