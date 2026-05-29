package apiserver

import (
	"fmt"
	"strings"
)

type codingLanguage struct {
	Name        string
	Display     string
	CodeFence   string
	Description string
	Function    string
	Notes       string
}

var codingLanguages = []codingLanguage{
	{
		Name:        "rust",
		Display:     "Rust",
		CodeFence:   "rust",
		Description: "Rust es un lenguaje compilado y tipado estaticamente, pensado para seguridad de memoria y rendimiento sin garbage collector.",
		Function:    "fn add(a: i32, b: i32) -> i32 {\n    a + b\n}",
		Notes:       "La ultima expresion sin punto y coma puede ser el valor retornado; ownership y borrowing controlan memoria y mutabilidad.",
	},
	{
		Name:        "go",
		Display:     "Go",
		CodeFence:   "go",
		Description: "Go es un lenguaje compilado, simple y orientado a servicios, con concurrencia basada en goroutines y una toolchain muy directa.",
		Function:    "func Add(a, b int) int {\n    return a + b\n}",
		Notes:       "Los tipos van en parametros y retorno; en Go se usa `return` explicito.",
	},
	{
		Name:        "javascript",
		Display:     "JavaScript",
		CodeFence:   "js",
		Description: "JavaScript es el lenguaje principal del navegador y tambien corre en servidores con runtimes como Node, Bun o Deno.",
		Function:    "function add(a, b) {\n  return a + b;\n}",
		Notes:       "No necesita tipos en tiempo de compilacion; si queres tipos estaticos, usa TypeScript.",
	},
	{
		Name:        "typescript",
		Display:     "TypeScript",
		CodeFence:   "ts",
		Description: "TypeScript agrega tipos estaticos sobre JavaScript para detectar errores antes de ejecutar y mejorar autocompletado.",
		Function:    "function add(a: number, b: number): number {\n  return a + b;\n}",
		Notes:       "El codigo se compila a JavaScript; los tipos no existen en runtime.",
	},
	{
		Name:        "python",
		Display:     "Python",
		CodeFence:   "python",
		Description: "Python es un lenguaje interpretado y expresivo, muy usado en scripts, automatizacion, datos, backend e IA.",
		Function:    "def add(a: int, b: int) -> int:\n    return a + b",
		Notes:       "Los type hints ayudan a leer y chequear, pero Python sigue siendo dinamico en runtime.",
	},
	{
		Name:        "react",
		Display:     "React",
		CodeFence:   "tsx",
		Description: "React es una libreria para construir interfaces con componentes declarativos y estado.",
		Function:    "type GreetingCardProps = { name: string }\n\nexport function GreetingCard({ name }: GreetingCardProps) {\n  return <section><h2>Hola, {name}</h2></section>\n}",
		Notes:       "Un componente es una funcion que recibe props y devuelve JSX.",
	},
}

func codingKnowledgeReply(messages []chatMessage) (string, bool) {
	normalized := normalizeBasicChat(lastUserMessage(messages))
	if normalized == "" || isRepoRepairRequest(normalized) {
		return "", false
	}
	languages := detectCodingLanguages(normalized)
	if len(languages) == 0 {
		return "", false
	}
	if wantsComparison(normalized) && len(languages) >= 2 {
		return compareLanguages(languages[0], languages[1]), true
	}
	if wantsCodeExplanation(normalized) {
		return explainCodeSnippet(normalized, languages[0]), true
	}
	if isReactComponentRequest(normalized) {
		react := languageByName("react")
		return fmt.Sprintf("Claro. Un componente React basico en TypeScript puede verse asi:\n\n```tsx\n%s\n```\n\n%s", react.Function, react.Notes), true
	}
	if wantsFunction(normalized) {
		lang := languages[0]
		return fmt.Sprintf("Una funcion simple en %s se escribe asi:\n\n```%s\n%s\n```\n\n%s", lang.Display, lang.CodeFence, lang.Function, lang.Notes), true
	}
	if wantsIntro(normalized) || isProgrammingHelpRequest(normalized) {
		lang := languages[0]
		return fmt.Sprintf("%s\n\nEjemplo corto:\n\n```%s\n%s\n```\n\n%s", lang.Description, lang.CodeFence, lang.Function, lang.Notes), true
	}
	return "", false
}

func detectCodingLanguages(normalized string) []codingLanguage {
	var out []codingLanguage
	for _, lang := range codingLanguages {
		switch lang.Name {
		case "go":
			if hasWord(normalized, "go") || strings.Contains(normalized, "golang") {
				out = append(out, lang)
			}
		case "javascript":
			if strings.Contains(normalized, "javascript") || hasWord(normalized, "js") || strings.Contains(normalized, "javacript") || strings.Contains(normalized, "java script") {
				out = append(out, lang)
			}
		case "typescript":
			if strings.Contains(normalized, "typescript") || hasWord(normalized, "ts") {
				out = append(out, lang)
			}
		default:
			if strings.Contains(normalized, lang.Name) {
				out = append(out, lang)
			}
		}
	}
	return dedupeLanguages(out)
}

func dedupeLanguages(in []codingLanguage) []codingLanguage {
	seen := map[string]bool{}
	var out []codingLanguage
	for _, lang := range in {
		if !seen[lang.Name] {
			out = append(out, lang)
			seen[lang.Name] = true
		}
	}
	return out
}

func languageByName(name string) codingLanguage {
	for _, lang := range codingLanguages {
		if lang.Name == name {
			return lang
		}
	}
	return codingLanguage{}
}

func wantsComparison(normalized string) bool {
	return hasAny(normalized, "diferencia", "diferencias", "compar", "versus", " vs ", "entre", "enter")
}

func wantsFunction(normalized string) bool {
	return hasAny(normalized, "funcion", "function", "metodo", "method", "como hago", "como hacer")
}

func wantsIntro(normalized string) bool {
	return hasAny(normalized, "hablame", "explicame", "explica", "que es", "como es", "aprender", "intro", "introduccion")
}

func wantsCodeExplanation(normalized string) bool {
	return hasAny(normalized, "explica este codigo", "explicame este codigo", "que hace este codigo", "explain this code") ||
		strings.Contains(normalized, "->") ||
		strings.Contains(normalized, "return") ||
		strings.Contains(normalized, "func ") ||
		strings.Contains(normalized, "fn ")
}

func compareLanguages(a, b codingLanguage) string {
	return fmt.Sprintf("%s y %s sirven para cosas distintas:\n\n- %s: %s\n- %s: %s\n\nEn practica, elegi %s si queres %s; elegi %s si queres %s.",
		a.Display,
		b.Display,
		a.Display,
		shortDescription(a),
		b.Display,
		shortDescription(b),
		a.Display,
		shortUseCase(a),
		b.Display,
		shortUseCase(b),
	)
}

func shortDescription(lang codingLanguage) string {
	switch lang.Name {
	case "python":
		return "rapido de escribir, ideal para scripts, datos e IA."
	case "javascript":
		return "base del navegador y muy usado en frontend/backend web."
	case "typescript":
		return "JavaScript con tipos para proyectos mas grandes."
	case "rust":
		return "rendimiento y seguridad de memoria con control fino."
	case "go":
		return "simplicidad, servicios y concurrencia."
	case "react":
		return "interfaces basadas en componentes."
	default:
		return lang.Description
	}
}

func shortUseCase(lang codingLanguage) string {
	switch lang.Name {
	case "python":
		return "automatizacion, notebooks o prototipos rapidos"
	case "javascript":
		return "codigo que corre en browser o Node/Bun"
	case "typescript":
		return "apps web mantenibles con tipos"
	case "rust":
		return "sistemas rapidos y seguros"
	case "go":
		return "APIs y servicios simples de operar"
	case "react":
		return "UI web interactiva"
	default:
		return "ese ecosistema"
	}
}

func explainCodeSnippet(normalized string, lang codingLanguage) string {
	switch lang.Name {
	case "rust":
		return "Ese codigo es Rust. Si ves algo como `fn add(a: i32, b: i32) -> i32 { a + b }`, define una funcion `add`, recibe dos enteros `i32` y devuelve otro `i32`; la expresion final `a + b` es el retorno."
	case "go":
		return "Ese codigo es Go. Una funcion `func Add(a, b int) int { return a + b }` recibe dos enteros, devuelve un entero y usa `return` explicito."
	case "javascript":
		return "Ese codigo es JavaScript. Las funciones pueden declararse con `function` o como arrow functions; normalmente reciben valores dinamicos y devuelven el resultado con `return`."
	case "typescript":
		return "Ese codigo es TypeScript. Es JavaScript con anotaciones de tipo, por ejemplo `a: number`, para detectar errores antes de ejecutar."
	case "react":
		return "Ese codigo es React. Un componente recibe props y devuelve JSX; React se encarga de renderizarlo en la UI."
	default:
		return fmt.Sprintf("Ese codigo parece %s. Puedo explicarlo linea por linea si me pasas el snippet completo.", lang.Display)
	}
}

func hasWord(text string, word string) bool {
	for _, token := range strings.Fields(text) {
		if token == word {
			return true
		}
	}
	return false
}
