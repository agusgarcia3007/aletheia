package research
import "testing"
func TestNavDumpVsRealAnswer(t *testing.T) {
	nav := "| Concepto de derivada - YouTube Acerca de Prensa Derechos de autor Comunicarte con nosotros Creadores Anunciar Desarrolladores Condiciones Privacidad Politicas y seguridad Como funciona YouTube Prueba funciones nuevas Google LLC"
	if !looksLikeStructuredDump(nav) { t.Fatal("nav dump not detected") }
	good := "Un modelo extenso de lenguaje o LLM es un modelo de lenguaje de aprendizaje profundo que consta de una red neuronal con muchos parametros"
	if looksLikeStructuredDump(good) { t.Fatal("real answer wrongly flagged as dump") }
}
