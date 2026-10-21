package enriquecer

import (
	"testing"

	"github.com/BasisTI/orq/internal/central"
)

// O prazo de "vencido" da web (central.PrazoVencido) é três vezes o TTL de cada fonte.
func TestPrazoVencidoAcompanhaOTTL(t *testing.T) {
	for _, f := range central.Fontes {
		if central.PrazoVencido[f] != 3*TTLPadrao[f] {
			t.Errorf("%s: prazo %v, TTL %v", f, central.PrazoVencido[f], TTLPadrao[f])
		}
	}
}
