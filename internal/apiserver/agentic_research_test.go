package apiserver

import (
	"reflect"
	"testing"
)

func TestResearchQueriesPlansBroadeningRounds(t *testing.T) {
	got := researchQueries("decime las propiedades del anana en hombres")
	want := []string{
		"decime las propiedades del anana en hombres",
		"decime propiedades anana hombres",
		"decime propiedades",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestResearchQueriesSingleKeywordNoRefinement(t *testing.T) {
	got := researchQueries("entropia")
	if len(got) != 1 || got[0] != "entropia" {
		t.Fatalf("got %#v, want single original query", got)
	}
}
