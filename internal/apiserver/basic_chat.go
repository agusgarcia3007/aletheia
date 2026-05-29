package apiserver

import "strings"

const mikrosModelName = "aletheia-mikros"

func basicMikrosChatReply(modelName string, messages []chatMessage) (string, bool) {
	if modelName != mikrosModelName {
		return "", false
	}
	userText := lastUserMessage(messages)
	normalized := normalizeBasicChat(userText)
	switch {
	case hasAny(normalized, "gracias", "thanks"):
		return "De nada. Puedo ayudarte con pruebas locales, API, deploy o comandos de Aletheia.", true
	case hasAny(normalized, "chau", "adios", "bye"):
		return "Chau. Cuando quieras seguir, puedo ayudarte con Aletheia y sus pruebas locales.", true
	case hasAny(normalized, "hola", "buenas", "buen dia", "hello", "hi"):
		return "Hola. Soy Aletheia Mikros, un modelo local experimental. Estoy listo para ayudar con respuestas basicas sobre Aletheia.", true
	case hasAny(normalized, "como estas", "how are you"):
		return "Estoy funcionando localmente y listo para ayudar con Aletheia, comandos, pruebas y API.", true
	case hasAny(normalized, "quien sos", "quien eres", "que sos", "como te llamas", "tu nombre", "what are you", "who are you"):
		return "Soy Aletheia Mikros, el primer modelo local basico de Aletheia. Soy experimental y tengo capacidades acotadas.", true
	case hasAny(normalized, "que podes hacer", "que puedes hacer", "que sabes hacer", "what can you do", "ayuda", "help"):
		return "Puedo conversar de forma basica y orientarte con comandos como ask, solve, eval, serve y train.", true
	case strings.Contains(normalized, "ask"):
		return "ask responde preguntas usando memoria local indexada y se abstiene cuando no tiene evidencia suficiente.", true
	case strings.Contains(normalized, "solve"):
		return "solve intenta reparar tareas verificables sobre repos. Aplica patches solo despues de pasar verificadores locales.", true
	case strings.Contains(normalized, "eval"):
		return "eval corre la suite bootstrap y reporta metricas verificables como success rate, abstencion y tool calls.", true
	case strings.Contains(normalized, "serve") || strings.Contains(normalized, "api") || strings.Contains(normalized, "openai"):
		return "serve expone una API local compatible con OpenAI SDK. Usa base_url terminado en /v1 y ALETHEIA_API_KEY como bearer token.", true
	case strings.Contains(normalized, "modelo") || strings.Contains(normalized, "model"):
		return "El modelo publico de esta version es aletheia-mikros: pequeno, local y pensado para chat basico sobre Aletheia.", true
	case hasAny(normalized, "limite", "limites", "confiar", "sabes", "internet"):
		return "Tengo limites: no soy un chatbot universal y debo apoyarme en evidencia local o pruebas verificables para hechos importantes.", true
	case strings.TrimSpace(normalized) == "":
		return "Hola. Soy Aletheia Mikros. Puedo ayudarte con comandos basicos y pruebas locales.", true
	default:
		return "", false
	}
}

func policyReply(modelName string, messages []chatMessage) (string, bool) {
	normalized := normalizeBasicChat(lastUserMessage(messages))
	if isRepoRepairRequest(normalized) {
		return "Para reparar codigo necesito un repo y una tarea verificable. Usa `aletheia solve --task task.json --verifier static_go_parse,go_test --trace`; desde chat no aplico patches ni ejecuto comandos.", true
	}
	return "", false
}

func isRepoRepairRequest(normalized string) bool {
	return hasAny(normalized, "arregla este repo", "arreglar este repo", "fix this repo", "go test", "failing test", "tests fallan", "repo falla")
}

func isReactComponentRequest(normalized string) bool {
	return hasAny(normalized, "componente de react", "component de react", "react component", "componente react") &&
		hasAny(normalized, "haz", "hace", "crea", "crear", "genera", "generar", "build", "make")
}

func isCodeGenerationRequest(normalized string) bool {
	return hasAny(normalized, "haz ", "hace ", "crea ", "crear ", "genera ", "generar ", "implementa ", "build ", "make ") &&
		hasAny(normalized, "codigo", "code", "componente", "component", "funcion", "function", "react", "go", "typescript", "javascript")
}

func isProgrammingHelpRequest(normalized string) bool {
	if isRepoRepairRequest(normalized) {
		return false
	}
	if len(detectCodingLanguages(normalized)) > 0 && hasAny(normalized,
		"hablame", "que es", "diferencia", "diferencias", "compar", "entre", "enter", "versus", " vs ",
	) {
		return true
	}
	return hasProgrammingLanguage(normalized) && hasAny(normalized,
		"codigo", "code", "ejemplo", "example", "snippet",
		"como es", "como se escribe", "como hago", "como hacer", "muestrame", "mostrame", "funcion", "function",
	)
}

func hasProgrammingLanguage(normalized string) bool {
	return len(detectCodingLanguages(normalized)) > 0 ||
		hasAny(normalized, "java", "c++", "cpp", "c#", "csharp", "php", "ruby", "swift", "kotlin")
}

func lastUserMessage(messages []chatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

func normalizeBasicChat(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	replacer := strings.NewReplacer(
		"á", "a",
		"é", "e",
		"í", "i",
		"ó", "o",
		"ú", "u",
		"ü", "u",
		"ñ", "n",
		"?", "",
		"¿", "",
		"!", "",
		"¡", "",
		",", " ",
		".", " ",
		"(", " ",
		")", " ",
		"[", " ",
		"]", " ",
		"{", " ",
		"}", " ",
		":", " ",
		";", " ",
	)
	return strings.Join(strings.Fields(replacer.Replace(text)), " ")
}

func hasAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
