package cli

import (
	"bytes"
	"testing"

	"github.com/BasisTI/orq/internal/central"
)

func TestTextoCoordCitaOPanePeloNome(t *testing.T) {
	pane := "w12:p4"
	c := central.Coordenador{Nome: "orq-comunicacao", PaneID: &pane}
	var b bytes.Buffer
	if err := textoCoord(&b, c, "colaboradados/ci-templates-v1231 (w12:p4)"); err != nil {
		t.Fatal(err)
	}
	if got, want := b.String(), "orq-comunicacao  pane colaboradados/ci-templates-v1231 (w12:p4)  workspace -  projeto -\n"; got != want {
		t.Fatalf("saída = %q, quer %q", got, want)
	}
}

func TestRefCentralNomeiaWorkspaceEPane(t *testing.T) {
	got := refCentral("Central", PaneCentral{Coordenador: "orq-comunicacao", PaneID: "w3:p2"})
	if want := "Central/orq · orq-comunicacao (w3:p2)"; got != want {
		t.Fatalf("refCentral = %q, quer %q", got, want)
	}
}
