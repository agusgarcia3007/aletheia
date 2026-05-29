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
	case hasAny(normalized, "quien sos", "que sos", "como te llamas", "what are you", "who are you"):
		return "Soy Aletheia Mikros, el primer modelo local basico de Aletheia. Soy experimental y tengo capacidades acotadas.", true
	case hasAny(normalized, "que podes hacer", "que puedes hacer", "what can you do", "ayuda", "help"):
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
		return "No tengo evidencia suficiente para responder con precision. Puedo ayudar con saludos, limites y comandos de Aletheia como ask, solve, eval y serve.", true
	}
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
