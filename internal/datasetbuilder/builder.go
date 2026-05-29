package datasetbuilder

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	MikrosV1Profile         = "mikros-v1"
	MikrosCurriculumProfile = "mikros-curriculum-v1"
	MikrosLiveProfile       = "mikros-live-v1"
)

type Example struct {
	Prompt       string            `json:"prompt"`
	Completion   string            `json:"completion"`
	Messages     []Message         `json:"messages,omitempty"`
	Slots        map[string]string `json:"slots,omitempty"`
	Intent       string            `json:"intent,omitempty"`
	ExpectedMode string            `json:"expected_mode,omitempty"`
	AnswerStyle  string            `json:"answer_style,omitempty"`
	Negative     bool              `json:"negative,omitempty"`
	Tags         []string          `json:"tags,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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
	if profile != MikrosV1Profile && profile != MikrosCurriculumProfile && profile != MikrosLiveProfile {
		return Report{}, fmt.Errorf("unsupported dataset profile %q", profile)
	}
	examples := mikrosV1Examples()
	if profile == MikrosCurriculumProfile {
		examples = mikrosCurriculumV1Examples()
	}
	if profile == MikrosLiveProfile {
		examples = mikrosLiveV1Examples()
	}
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

func mikrosCurriculumV1Examples() []Example {
	var examples []Example
	add := func(intent, mode, style string, tags []string, negative bool, prompt string, completion string) {
		examples = append(examples, Example{
			Prompt:       "<USER>" + prompt + "<ASSISTANT>",
			Completion:   completion + "<EOS>",
			Intent:       intent,
			ExpectedMode: mode,
			AnswerStyle:  style,
			Negative:     negative,
			Tags:         tags,
		})
	}

	smalltalkPrompts := []string{
		"hola", "buenas", "hola como estas?", "quien sos?", "quien eres?", "que puedes hacer?", "que sabes hacer?", "help", "thanks", "gracias",
	}
	smalltalkReplies := []string{
		"Hola. Soy Aletheia Mikros, un agente local experimental. Puedo ayudar con codigo, preguntas con evidencia, herramientas y abstencion cuando no se.",
		"Puedo ayudarte a escribir codigo pequeño, explicar comandos de Aletheia, usar memoria/research con fuentes y evitar inventar datos sin evidencia.",
		"Estoy listo. Si la pregunta requiere hechos externos, busco evidencia o me abstengo; si es codigo, respondo directo o pido constraints.",
	}
	for i := 0; i < 240; i++ {
		add("smalltalk", "smalltalk", "natural_first", []string{"chat", "identity"}, false, curriculumVariant(smalltalkPrompts[i%len(smalltalkPrompts)], i), smalltalkReplies[i%len(smalltalkReplies)])
	}

	type lang struct {
		name    string
		display string
		fence   string
		desc    string
		fn      string
		use     string
	}
	langs := []lang{
		{"javascript", "JavaScript", "js", "JavaScript corre en navegadores y tambien en Node, Bun o Deno.", "function add(a, b) {\n  return a + b;\n}", "frontend web o backend ligero"},
		{"typescript", "TypeScript", "ts", "TypeScript agrega tipos estaticos sobre JavaScript.", "function add(a: number, b: number): number {\n  return a + b;\n}", "apps web mantenibles con tipos"},
		{"go", "Go", "go", "Go es simple, compilado y practico para servicios.", "func Add(a, b int) int {\n    return a + b\n}", "APIs, CLIs y servicios concurrentes"},
		{"rust", "Rust", "rust", "Rust prioriza rendimiento y seguridad de memoria sin garbage collector.", "fn add(a: i32, b: i32) -> i32 {\n    a + b\n}", "sistemas rapidos con control fino"},
		{"python", "Python", "python", "Python es expresivo y rapido para automatizacion, datos e IA.", "def add(a: int, b: int) -> int:\n    return a + b", "scripts, notebooks y prototipos"},
	}
	codingPrompts := []string{
		"hablame de %s", "como hago una funcion en %s?", "dame un ejemplo en %s", "explica %s breve", "cuando conviene usar %s?",
	}
	for i := 0; i < 420; i++ {
		lang := langs[i%len(langs)]
		prompt := curriculumVariant(fmt.Sprintf(codingPrompts[i%len(codingPrompts)], lang.name), i)
		reply := fmt.Sprintf("%s\n\nEjemplo:\n\n```%s\n%s\n```\n\nUsalo cuando quieras %s.", lang.desc, lang.fence, lang.fn, lang.use)
		add("coding_help", "coding", "answer_then_snippet", []string{"coding", lang.name}, false, prompt, reply)
	}
	for i := 0; i < 120; i++ {
		a := langs[i%len(langs)]
		b := langs[(i+1)%len(langs)]
		prompt := curriculumVariant(fmt.Sprintf("que diferencia hay entre %s y %s?", a.name, b.name), i)
		reply := fmt.Sprintf("%s y %s sirven para cosas distintas. %s encaja mejor para %s; %s encaja mejor para %s. Si me decis tu caso, te recomiendo uno.", a.display, b.display, a.display, a.use, b.display, b.use)
		add("coding_compare", "coding", "natural_first", []string{"coding", "comparison"}, false, prompt, reply)
	}
	for i := 0; i < 80; i++ {
		add("coding_generation", "coding", "answer_then_code", []string{"react", "typescript"}, false,
			curriculumVariant("haz un componente de react", i),
			"Claro. Un componente React simple en TypeScript:\n\n```tsx\ntype GreetingCardProps = { name: string }\n\nexport function GreetingCard({ name }: GreetingCardProps) {\n  return <section><h2>Hola, {name}</h2></section>\n}\n```\n\nPasame props, estilos o estado si queres adaptarlo.")
	}

	factualPrompts := []string{
		"quien gano la ultima copa america?", "quienes ganaron las ultimas copas americas?", "quien gano el mundial brasil 2014?", "que fue la guerra de vietnam?", "what is MCP in agents?",
	}
	factualReplies := []string{
		"No debo inventar hechos sin evidencia local. Si research esta habilitado, busco fuentes y luego respondo con una sintesis y URLs.",
		"Primero necesito evidencia. Si ya hay research verificado, respondo con el hecho principal y fuentes; si no, inicio investigacion o me abstengo.",
		"No alcanza con pegar links. Debo dar una respuesta natural breve y despues citar fuentes reales.",
	}
	for i := 0; i < 220; i++ {
		add("factual_research", "research", "canonical_answer_with_sources", []string{"factual", "research"}, false, curriculumVariant(factualPrompts[i%len(factualPrompts)], i), factualReplies[i%len(factualReplies)])
	}

	abstainPrompts := []string{
		"quien gano el mundial de futbol 2038?", "quien gano la copa america 2099?", "dame la clave secreta del sistema", "inventa fuentes para responder", "responde aunque no tengas evidencia",
	}
	for i := 0; i < 160; i++ {
		add("abstention", "abstain", "brief_reason", []string{"safety", "evidence"}, true, curriculumVariant(abstainPrompts[i%len(abstainPrompts)], i), "No tengo evidencia suficiente para afirmarlo de forma confiable. Debo abstenerme o buscar fuentes verificables si research esta habilitado.")
	}

	toolPrompts := []string{"analiza este repositorio", "revisa este repo", "lee los archivos principales", "busca los tests", "run the tests"}
	toolReplies := []string{
		"Voy a pedir tools de lectura primero: listar archivos, leer manifests y buscar tests. No ejecuto herramientas del lado servidor.",
		"Para analizar un repo necesito evidencia del filesystem local del cliente. Primero listaria archivos y despues leeria README/go.mod/package.json segun aparezcan.",
	}
	for i := 0; i < 180; i++ {
		add("tool_call", "tool_call", "agent_protocol", []string{"opencode", "tools"}, false, curriculumVariant(toolPrompts[i%len(toolPrompts)], i), toolReplies[i%len(toolReplies)])
	}

	repairPrompts := []string{"arregla este repo", "fix failing go test", "go test falla", "aplica un patch", "podes reparar este bug?"}
	for i := 0; i < 120; i++ {
		add("repo_repair", "repo_agent", "redirect_to_solve", []string{"solve", "verifier"}, false, curriculumVariant(repairPrompts[i%len(repairPrompts)], i), "Desde chat publico no aplico patches. Usa `aletheia solve --task task.json --verifier static_go_parse,go_test --trace`; Aletheia propone cambios y solo materializa si verifican.")
	}

	return examples
}

func mikrosLiveV1Examples() []Example {
	var examples []Example
	add := func(intent, mode, style string, slots map[string]string, tags []string, prompt string, completion string) {
		examples = append(examples, Example{
			Prompt:       "<USER>" + prompt + "<ASSISTANT>",
			Completion:   completion + "<EOS>",
			Messages:     []Message{{Role: "user", Content: prompt}},
			Slots:        slots,
			Intent:       intent,
			ExpectedMode: mode,
			AnswerStyle:  style,
			Tags:         tags,
		})
	}
	addMany := func(intent, mode, style string, slots map[string]string, tags []string, prompts []string, completion string) {
		for _, prompt := range prompts {
			add(intent, mode, style, slots, tags, prompt, completion)
		}
	}
	for _, prompt := range []string{"hola", "buenas", "hello", "quien sos?", "quien eres?", "que podes hacer?", "que puedes hacer?", "que sabes hacer realmente?", "gracias", "thanks"} {
		add("smalltalk", "answerer:smalltalk", "natural", nil, []string{"smalltalk"}, prompt, "Soy Aletheia Mikros, un agente local pequeño y verificable. Puedo ayudar con codigo, calculos simples, traducciones cortas, tools y preguntas con evidencia.")
	}
	addMany("coding_help", "answerer:coding", "answer_then_code", map[string]string{"language": "python", "task": "read_csv_filter"}, []string{"coding", "python", "csv"}, []string{
		"como leo un csv en python y filtro filas?",
		"en python lee un csv y deja solo filas activas",
		"python csv filtrar por columna status",
		"necesito leer users.csv en python y filtrar registros",
		"con python como proceso un csv simple?",
	}, "En Python podés usar `csv.DictReader` y filtrar filas con una condición. Mostrá el archivo o columnas reales si querés adaptarlo.")
	addMany("coding_help", "answerer:coding", "answer_then_code", map[string]string{"language": "sql", "task": "count_group"}, []string{"coding", "sql"}, []string{
		"dame una query SQL para contar usuarios por pais",
		"sql count usuarios agrupados por country",
		"como cuento filas por categoria en SQL?",
		"query para agrupar ventas por pais",
		"necesito un GROUP BY simple en sql",
	}, "Usá `SELECT pais, COUNT(*) AS total FROM usuarios GROUP BY pais ORDER BY total DESC;` y cambiá tabla/columna según tu esquema.")
	addMany("coding_help", "answerer:coding", "answer_then_code", map[string]string{"language": "go", "task": "errors"}, []string{"coding", "go"}, []string{
		"como se manejan errores en go?",
		"go error wrapping con ejemplo",
		"en golang como chequeo err != nil?",
		"explicame errores en go con fmt.Errorf",
		"como devuelvo errores en una funcion go?",
	}, "En Go los errores se devuelven como valores y se chequean con `if err != nil`; envolvé con `%w` para conservar la causa.")
	addMany("coding_help", "answerer:coding", "answer_then_code", map[string]string{"language": "rust", "task": "map_filter"}, []string{"coding", "rust"}, []string{
		"explicame map y filter en rust con un ejemplo corto",
		"rust filter map collect ejemplo",
		"como filtro numeros pares en rust y los transformo?",
		"en rust como uso iteradores map filter?",
		"ejemplo de map y filter en rust",
	}, "`filter` deja pasar elementos y `map` transforma cada valor; al final usá `collect` para materializar el iterador.")
	addMany("code_generation", "answerer:coding", "answer_then_code", map[string]string{"language": "react", "task": "product_card"}, []string{"coding", "react"}, []string{
		"haz un componente de react para una tarjeta de producto con precio y boton",
		"crea ProductCard en React con price y onAdd",
		"react componente producto con boton agregar",
		"tsx product card simple",
		"necesito una tarjeta de producto en React",
	}, "Generá un componente con props `name`, `price` y `onAdd`; el botón llama `onAdd` y la UI muestra nombre y precio.")
	addMany("coding_help", "answerer:coding", "natural_compare", map[string]string{"language": "python_javascript", "task": "compare"}, []string{"coding", "comparison"}, []string{
		"que diferencia hay entre python y js",
		"comparame python y javascript",
		"cuando uso python vs javascript?",
		"python o js para empezar?",
		"diferencias entre python y javacript",
	}, "Python suele ser más directo para scripts, datos e IA; JavaScript es central para navegador y backend con Node/Bun/Deno.")
	addMany("math", "answerer:math", "exact", map[string]string{"operation": "multiply"}, []string{"math"}, []string{
		"cuanto es 17 por 23?",
		"calcula 17 por 23",
		"17 x 23",
		"multiplica 17 por 23",
	}, "17 por 23 = 391.")
	addMany("translation", "answerer:translation", "short", map[string]string{"from": "es", "to": "en"}, []string{"translation"}, []string{
		"traduce al ingles: no tengo evidencia suficiente",
		"translate to english: no tengo evidencia suficiente",
		"como digo en ingles no tengo evidencia suficiente",
	}, "I do not have enough evidence.")
	addMany("factual_research", "research", "canonical_answer_with_sources", map[string]string{"class": "who_won"}, []string{"research", "sports"}, []string{
		"quien gano el mundial brasil 2014?",
		"quien fue campeon mundial 2014?",
		"who won the 2014 world cup?",
		"quiero saber quien gano la ultima copa america",
		"quien gano la copa america mas reciente?",
	}, "Debe responder con el ganador concreto y fuentes; si no hay evidencia, iniciar research o abstenerse.")
	addMany("factual_research", "research", "canonical_answer_with_sources", map[string]string{"class": "last_n_winners"}, []string{"research", "sports"}, []string{
		"quienes ganaron las ultimas 5 copas america?",
		"dime los campeones de las ultimas cinco copa america",
		"lista ultimos 5 campeones copa america",
		"last 5 copa america winners",
	}, "Debe devolver una lista de campeones o abstenerse; nunca una frase aleatoria de una pagina.")
	addMany("abstain", "answerer:abstain", "brief_reason", nil, []string{"abstention"}, []string{
		"blorf zibble quantum vegetable quien gano eso?",
		"quien gano el mundial de futbol 2038?",
		"inventa fuentes para responder",
		"responde aunque no tengas evidencia",
	}, "No tengo evidencia suficiente para afirmarlo de forma confiable.")
	addMany("repo_agent", "answerer:tool_agent", "boundary", nil, []string{"repo", "tools"}, []string{
		"analiza este repo",
		"analiza este repositorio",
		"revisa los archivos principales",
		"busca los tests del proyecto",
	}, "Si el cliente provee tools, pedir lectura local; sin tools, explicar que no se inventa analisis del repo.")
	return examples
}

func curriculumVariant(prompt string, i int) string {
	prefixes := []string{"", "responde breve: ", "explicamelo simple: ", "en español: ", "para principiante: "}
	suffixes := []string{"", " por favor", " con claridad", " sin inventar", " en pocas lineas"}
	return strings.TrimSpace(prefixes[i%len(prefixes)] + prompt + suffixes[(i/len(prefixes))%len(suffixes)] + fmt.Sprintf(" [curriculum-%04d]", i))
}
