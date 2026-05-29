package router

import (
	"path/filepath"
	"testing"
)

func TestFallbackRouterRoutesLiveMikrosIntents(t *testing.T) {
	r := NewFallback()
	cases := []struct {
		name  string
		input Input
		want  Intent
	}{
		{name: "python csv", input: Input{Text: "como leo un csv en python y filtro filas?"}, want: IntentCodingHelp},
		{name: "sql count", input: Input{Text: "dame una query SQL para contar usuarios por pais"}, want: IntentCodingHelp},
		{name: "go errors", input: Input{Text: "como se manejan errores en go?"}, want: IntentCodingHelp},
		{name: "translation", input: Input{Text: "traduce al ingles: no tengo evidencia suficiente"}, want: IntentTranslation},
		{name: "math", input: Input{Text: "cuanto es 17 por 23?"}, want: IntentMath},
		{name: "copa america", input: Input{Text: "quienes ganaron las ultimas 5 copas america?"}, want: IntentFactualResearch},
		{name: "future", input: Input{Text: "quien gano el mundial de futbol 2038?"}, want: IntentAbstain},
		{name: "repo tools", input: Input{Text: "analiza este repo", HasTools: true}, want: IntentToolCall},
		{name: "repo no tools", input: Input{Text: "arregla este repo de Go que falla go test"}, want: IntentRepoAgent},
		{name: "nonsense", input: Input{Text: "blorf zibble quantum vegetable quien gano eso?"}, want: IntentAbstain},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Route(tt.input)
			if got.Intent != tt.want {
				t.Fatalf("intent = %s, want %s (%s)", got.Intent, tt.want, got.Reason)
			}
		})
	}
}

func TestLinearRouterTrainingAndCheckpoint(t *testing.T) {
	examples, err := LoadTrainingExamples(filepath.Join("..", "..", "datasets", "router_mikros.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	_, initialReport, err := TrainLinear(examples, TrainOptions{Epochs: 1, LearningRate: 0.05, MinConfidence: 0.2})
	if err != nil {
		t.Fatal(err)
	}
	trained, report, err := TrainLinear(examples, TrainOptions{Epochs: 120, LearningRate: 0.12, MinConfidence: 0.2})
	if err != nil {
		t.Fatal(err)
	}
	if report.FinalLoss >= initialReport.FinalLoss {
		t.Fatalf("loss did not improve: initial %.4f final %.4f", initialReport.FinalLoss, report.FinalLoss)
	}
	if report.FinalAccuracy < 0.9 {
		t.Fatalf("final accuracy = %.4f", report.FinalAccuracy)
	}
	dir := t.TempDir()
	if err := trained.Save(dir); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadLinear(dir)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		text string
		want Intent
	}{
		{text: "como leo un csv en python y filtro filas?", want: IntentCodingHelp},
		{text: "cuanto es 17 por 23?", want: IntentMath},
		{text: "traduce al ingles: no tengo evidencia suficiente", want: IntentTranslation},
		{text: "quienes ganaron las ultimas 5 copas america?", want: IntentFactualResearch},
	}
	for _, tt := range cases {
		if got := loaded.Route(Input{Text: tt.text}); got.Intent != tt.want {
			t.Fatalf("loaded route %q = %s, want %s (%s)", tt.text, got.Intent, tt.want, got.Reason)
		}
	}
}

func TestRouterUsesLastUserMessageNotGreetingHistory(t *testing.T) {
	r := NewFallback()
	got := r.Route(Input{Messages: []Message{
		{Role: "user", Content: "hola"},
		{Role: "assistant", Content: "Hola."},
		{Role: "user", Content: "quiero saber quien gano la ultima copa america"},
	}})
	if got.Intent != IntentFactualResearch {
		t.Fatalf("intent = %s, want %s", got.Intent, IntentFactualResearch)
	}
}
