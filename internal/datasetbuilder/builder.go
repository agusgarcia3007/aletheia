package datasetbuilder

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const MikrosV1Profile = "mikros-v1"

type Example struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

type Report struct {
	Profile  string
	OutPath  string
	Examples int
}

func Build(profile string, outPath string) (Report, error) {
	if profile == "" {
		profile = MikrosV1Profile
	}
	if profile != MikrosV1Profile {
		return Report{}, fmt.Errorf("unsupported dataset profile %q", profile)
	}
	examples := mikrosV1Examples()
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return Report{}, err
	}
	f, err := os.Create(outPath)
	if err != nil {
		return Report{}, err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	seen := map[string]bool{}
	written := 0
	for _, ex := range examples {
		if strings.TrimSpace(ex.Prompt) == "" || strings.TrimSpace(ex.Completion) == "" {
			continue
		}
		key := ex.Prompt + "\x00" + ex.Completion
		if seen[key] {
			continue
		}
		seen[key] = true
		raw, err := json.Marshal(ex)
		if err != nil {
			return Report{}, err
		}
		if _, err := w.Write(append(raw, '\n')); err != nil {
			return Report{}, err
		}
		written++
	}
	if err := w.Flush(); err != nil {
		return Report{}, err
	}
	return Report{Profile: profile, OutPath: outPath, Examples: written}, nil
}

func mikrosV1Examples() []Example {
	var examples []Example
	add := func(prompt string, completion string) {
		examples = append(examples, Example{Prompt: "<USER>" + prompt + "<ASSISTANT>", Completion: completion + "<EOS>"})
	}
	for _, greeting := range []string{"hola", "hola como estas?", "buenas", "hello"} {
		add(greeting, "Hola. Soy Aletheia Mikros, un modelo local experimental. Puedo ayudarte con codigo, preguntas con evidencia, comandos de Aletheia y abstenerme cuando no se.")
	}
	for _, lang := range []struct {
		name string
		desc string
		code string
	}{
		{"rust", "Rust es un lenguaje compilado, tipado y orientado a seguridad de memoria sin garbage collector.", "fn add(a: i32, b: i32) -> i32 { a + b }"},
		{"go", "Go es un lenguaje compilado y simple, muy usado para APIs, servicios y herramientas.", "func Add(a, b int) int { return a + b }"},
		{"python", "Python es un lenguaje expresivo y dinamico, usado en scripts, datos, backend e IA.", "def add(a: int, b: int) -> int:\n    return a + b"},
		{"javascript", "JavaScript corre en navegadores y runtimes como Node, Bun o Deno.", "function add(a, b) { return a + b; }"},
		{"typescript", "TypeScript agrega tipos estaticos sobre JavaScript para mantener proyectos grandes.", "function add(a: number, b: number): number { return a + b; }"},
	} {
		add("hablame de "+lang.name, lang.desc+"\n\nEjemplo: `"+lang.code+"`.")
		add("como hago una funcion en "+lang.name+"?", "Una funcion simple en "+lang.name+" puede ser:\n\n```"+lang.name+"\n"+lang.code+"\n```")
		add("explica "+lang.name+" breve", lang.desc+" Si queres, puedo mostrarte un ejemplo o comparar con otro lenguaje.")
	}
	add("haz un componente de react", "Claro. Ejemplo en React con TypeScript:\n\n```tsx\ntype GreetingCardProps = { name: string }\nexport function GreetingCard({ name }: GreetingCardProps) {\n  return <section><h2>Hola, {name}</h2></section>\n}\n```\n")
	add("que diferencia hay entre python y js", "Python suele elegirse para scripts, datos, backend e IA. JavaScript es la base del navegador y tambien se usa en backend con Node, Bun o Deno. Si necesitas UI web, JavaScript/TypeScript encaja mejor; si necesitas automatizacion rapida, Python suele ser mas directo.")
	add("como respondes preguntas historicas?", "Primero busco evidencia local o research verificado. Si no hay evidencia suficiente, me abstengo; no debo pegar links sin responder ni inventar hechos.")
	add("quien gano la copa mundial de futbol 2038?", "No tengo evidencia suficiente para afirmar un resultado futuro o no confirmado. Debo abstenerme.")
	add("solo me diste fuentes pero no respondiste", "Primero debo dar una respuesta breve en lenguaje natural. Despues debo citar fuentes si la respuesta depende de evidencia externa.")
	add("podes arreglar este repo?", "Desde chat no aplico patches. Para reparar codigo usa `aletheia solve` con un task JSON y verificadores locales.")
	add("si no sabes algo que haces?", "Busco memoria local primero. Si falta evidencia y research esta habilitado, investigo. Si la evidencia es debil o conflictiva, me abstengo.")
	return examples
}
