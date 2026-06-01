package apiserver

import "strings"

// batteryCases returns the out-of-distribution prompt battery. None of these are
// verbatim eval cases or verbatim answerer map keys; they are paraphrases,
// unseen languages, unseen tasks, and adversarial variants.
func batteryCases() []batteryCase {
	var cs []batteryCase

	add := func(cat string, check func(string, bool, string) (string, bool), prompts ...string) {
		for _, p := range prompts {
			cs = append(cs, batteryCase{category: cat, prompt: p, check: check})
		}
	}

	add("smalltalk", mustContainAny("aletheia", "mikros", "puedo", "ayudar", "codigo", "hola", "evidencia"),
		"buenas, todo bien?",
		"hey que onda",
		"que tal andas hoy",
		"holaa",
		"sos un robot o una persona?",
		"contame quien sos",
		"en que me podes ayudar exactamente",
		"que cosas sabes hacer vos",
		"necesito ayuda con algo",
		"buen dia",
		"hello there",
		"hi, what can you do?",
		"who built you?",
		"te puedo hacer preguntas?",
		"gracias capo",
		"muchas gracias por la ayuda",
		"nos vemos, chau",
		"adios",
	)

	add("coding_known_lang_novel_task", mustContainAny("```", "def ", "function", "fn ", "func ", "select", "const ", "return"),
		"en python, como invierto una lista?",
		"en python, como abro un archivo y leo lineas?",
		"en python escribi una funcion que diga si un numero es primo",
		"en javascript como hago un debounce?",
		"en javascript como clono un objeto profundo?",
		"en typescript como defino un tipo generico para un Result?",
		"en go como lanzo una goroutine con waitgroup?",
		"en go como parseo json a un struct?",
		"en rust como manejo un Option con match?",
		"en rust como leo un archivo de texto completo?",
		"en sql como hago un join entre pedidos y clientes?",
		"en sql como saco el promedio de ventas por mes?",
		"en react como uso useEffect para fetch de datos?",
		"en react como hago un input controlado?",
	)

	add("coding_unsupported_lang", mustContainAny("```", "fun ", "func ", "def ", "<?php", "println", "fn "),
		"como hago un loop en kotlin?",
		"dame un ejemplo de funcion en php",
		"como imprimo en consola en java?",
		"escribi un hola mundo en c++",
		"como hago un for en ruby?",
		"ejemplo de funcion en swift",
		"como declaro una variable en haskell?",
		"un ejemplo simple en c#",
	)

	add("math_arithmetic", mustContainAny("=", " es ", " da "),
		"cuanto es 123 por 456?",
		"cuanto es 1000 menos 333?",
		"sumame 48 mas 52",
		"cuanto da 144 dividido 12?",
		"calcula 99 por 99",
	)
	add("math_beyond_arithmetic", mustContainAny("=", "raiz", "%", "porciento", "elevado", "potencia", "12", "144"),
		"cuanto es 15% de 200?",
		"cual es la raiz cuadrada de 144?",
		"cuanto es 2 elevado a 10?",
		"cuanto es el 20 por ciento de 50?",
		"resolve la ecuacion 2x + 4 = 10",
	)

	add("translation_arbitrary", mustContainAny("the", "cat", "i ", "you", "house", "dog", "good"),
		"traduce al ingles: el gato come pescado",
		"traduce al ingles: necesito ayuda con mi codigo",
		"como se dice 'buenos dias' en ingles?",
		"traduci al ingles: la casa es grande",
		"traduce: el perro corre rapido",
	)

	add("factual_no_research", abstains(),
		"quien es el presidente de francia?",
		"cual es la capital de australia?",
		"en que año cayo el muro de berlin?",
		"quien escribio cien años de soledad?",
		"cuantos planetas tiene el sistema solar?",
		"que es la fotosintesis?",
		"quien gano el balon de oro 2021?",
		"cual es la moneda de japon?",
		"que altura tiene el everest?",
		"quien pinto la mona lisa?",
		"cuando se fundo roma?",
		"que es un agujero negro?",
		"cual es el rio mas largo del mundo?",
		"quien invento el telefono?",
		"que paso en la revolucion francesa?",
	)

	add("future_outcome_abstain", abstains(),
		"quien va a ganar el mundial 2030?",
		"quien sera presidente de argentina en 2035?",
		"que equipo ganara la champions 2040?",
		"cual sera el precio del bitcoin en 2050?",
		"quien ganara el balon de oro 2099?",
	)

	add("repo_agent_no_tools", mustContainAny("solve", "herramientas", "tools", "no ejecuto", "cliente"),
		"arregla este repo de go que falla en go test",
		"los tests fallan, podes corregir el codigo?",
		"aplica un patch para arreglar el bug",
		"el repo falla al compilar, arreglalo",
	)

	// 9) TOOL USE with tools provided — must emit tool_calls.
	const repoTools = `[{"type":"function","function":{"name":"list_files","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}},{"type":"function","function":{"name":"read","parameters":{"type":"object","properties":{"filePath":{"type":"string"}},"required":["filePath"]}}}]`
	for _, p := range []string{
		"analiza este repositorio",
		"revisa el codigo del proyecto",
		"explorá los archivos del repo",
		"que hay en este repo?",
	} {
		cs = append(cs, batteryCase{category: "tool_use_with_tools", prompt: p, tools: repoTools, check: isToolCallCheck()})
	}

	add("nonsense_abstain", abstains(),
		"blorf zibble quantum vegetable",
		"asdf asdf asdf",
		"zibble blorf",
		"lorem ipsum dolor",
	)

	add("ambiguous", abstains(),
		"y entonces?",
		"y eso?",
		"continua",
		"dale",
	)

	add("open_ended_creative", func(content string, _ bool, _ string) (string, bool) {

		if strings.TrimSpace(content) == "" {
			return "empty", false
		}
		if len([]rune(content)) < 15 {
			return "too short: " + content, false
		}
		return "", true
	},
		"escribime un poema corto sobre el mar",
		"contame un chiste",
		"dame ideas para el cumpleaños de mi hermana",
		"recomendame un libro de ciencia ficcion",
		"explicame la teoria de la relatividad como si tuviera 5 años",
		"redacta un email para pedir vacaciones",
		"hace un resumen de la segunda guerra mundial",
		"dame consejos para dormir mejor",
	)

	return cs
}
