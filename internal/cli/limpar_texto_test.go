package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/limpeza"
)

func TestTextoLimpezaNadaAFazerSoSemRecusa(t *testing.T) {
	var sem bytes.Buffer
	if err := textoLimpeza(&sem, limpeza.Resultado{US: "orq/1"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sem.String(), "nada a fazer") {
		t.Errorf("sem itens e sem recusa deveria dizer nada a fazer:\n%s", sem.String())
	}
	var rec bytes.Buffer
	if err := textoLimpeza(&rec, limpeza.Resultado{US: "orq/1", Recusa: "MR reaberta"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.String(), "recusada") || strings.Contains(rec.String(), "nada a fazer") {
		t.Errorf("recusa não deve terminar em nada a fazer:\n%s", rec.String())
	}
}
