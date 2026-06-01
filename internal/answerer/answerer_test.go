package answerer

import (
	"context"
	"strings"
	"testing"

	"aletheia/internal/router"
)

func TestDefaultAnswerersHandleLiveMikrosBasics(t *testing.T) {
	answerers := Default()
	cases := []struct {
		name   string
		req    Request
		want   []string
		forbid []string
	}{
		{
			name: "python csv",
			req:  Request{Query: "como leo un csv en python y filtro filas?", Intent: router.IntentCodingHelp},
			want: []string{"Python", "csv.DictReader", "status"},
		},
		{
			name: "sql count",
			req:  Request{Query: "dame una query SQL para contar usuarios por pais", Intent: router.IntentCodingHelp},
			want: []string{"SELECT pais", "COUNT(*)", "GROUP BY pais"},
		},
		{
			name: "go errors",
			req:  Request{Query: "como se manejan errores en go?", Intent: router.IntentCodingHelp},
			want: []string{"Go", "err != nil", "%w"},
		},
		{
			name: "rust map filter",
			req:  Request{Query: "explicame map y filter en rust con un ejemplo corto", Intent: router.IntentCodingHelp},
			want: []string{"Rust", "filter", "map", "collect"},
		},
		{
			name: "react product",
			req:  Request{Query: "haz un componente de react para una tarjeta de producto con precio y boton", Intent: router.IntentCodeGeneration},
			want: []string{"ProductCard", "price", "onAdd", "Agregar"},
		},
		{
			name: "math",
			req:  Request{Query: "cuanto es 17 por 23?", Intent: router.IntentMath},
			want: []string{"17 por 23 = 391"},
		},
		{
			name: "translation",
			req:  Request{Query: "traduce al ingles: no tengo evidencia suficiente", Intent: router.IntentTranslation},
			want: []string{"I do not have enough evidence."},
		},
		{
			name:   "abstain",
			req:    Request{Query: "blorf zibble quantum vegetable quien gano eso?", Intent: router.IntentAbstain},
			want:   []string{"No tengo evidencia suficiente"},
			forbid: []string{"Fuentes:", "chunk="},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resp, ok, err := answerers.Answer(context.Background(), tt.req)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				t.Fatalf("no answer for %+v", tt.req)
			}
			for _, want := range tt.want {
				if !strings.Contains(resp.Content, want) {
					t.Fatalf("content missing %q:\n%s", want, resp.Content)
				}
			}
			for _, forbid := range tt.forbid {
				if strings.Contains(resp.Content, forbid) {
					t.Fatalf("content contains %q:\n%s", forbid, resp.Content)
				}
			}
		})
	}
}

func TestCodingAnswererAsksForConstraintsWhenLanguageMissing(t *testing.T) {
	resp, err := (CodingAnswerer{}).Answer(context.Background(), Request{
		Query:  "haceme una funcion",
		Intent: router.IntentCodeGeneration,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Content, "lenguaje") || !strings.Contains(resp.Content, "objetivo") {
		t.Fatalf("content = %s", resp.Content)
	}
}

// A basic "function in <language>" request must be answered directly with the
// minimal example for that exact language — never routed to web retrieval (which
// used to surface a Rust snippet for a Go or Python question).
func TestCodingAnswererAnswersBasicFunctionPerLanguage(t *testing.T) {
	cases := []struct {
		query string
		want  string
	}{
		{"mostrame el codigo de una funcion en rust", "fn add"},
		{"como hago una funcion en go", "func Add"},
		{"dame una funcion en python", "def add"},
	}
	for _, tc := range cases {
		resp, err := (CodingAnswerer{}).Answer(context.Background(), Request{Query: tc.query, Intent: router.IntentCodingHelp})
		if err != nil {
			t.Fatalf("%q: %v", tc.query, err)
		}
		if !strings.Contains(resp.Content, tc.want) {
			t.Fatalf("%q: content = %q, want substring %q", tc.query, resp.Content, tc.want)
		}
		if strings.Contains(resp.Content, "Volvé a preguntar") || strings.Contains(resp.Content, "Fuentes:") {
			t.Fatalf("%q routed to research instead of answering: %q", tc.query, resp.Content)
		}
	}
}
