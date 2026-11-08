package hora

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestFormatoBRTIndependenteDoFusoDoHost(t *testing.T) {
	old := time.Local
	defer func() { time.Local = old }()
	for _, loc := range []*time.Location{time.UTC, time.FixedZone("X", 9*3600)} {
		time.Local = loc
		if got := DataHoraRFC("2026-11-07T01:30:00Z"); got != "06/11/2026 22:30" {
			t.Errorf("local=%v: DataHoraRFC = %q", loc, got)
		}
		if got := DataRFC("2026-11-07T01:30:00Z"); got != "06/11/2026" {
			t.Errorf("DataRFC = %q", got)
		}
	}
}

func TestFusoVemDoBancoEmbutido(t *testing.T) {
	if Fuso.String() != "America/Sao_Paulo" {
		t.Fatalf("Fuso = %v, quer America/Sao_Paulo", Fuso)
	}
}

func TestTextoInvalidoVoltaComoVeio(t *testing.T) {
	if got := DataHoraRFC("ontem"); got != "ontem" {
		t.Errorf("got %q", got)
	}
}

// Sem o import de time/tzdata o binário só tem fuso se o host tiver zoneinfo:
// o pacote precisa carregá-lo.
func TestPacoteEmbuteOBancoDeFusos(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list indisponível: %v", err)
	}
	if !strings.Contains(string(out), "\ntime/tzdata\n") {
		t.Fatal("internal/hora não depende de time/tzdata")
	}
}

func TestAnteriorFormataOInstante(t *testing.T) {
	m := map[string]any{"anterior": map[string]any{"em": "2027-01-01T02:00:00Z", "por": "x"}}
	if !Anterior(m) || m["anterior"].(map[string]any)["em"] != "31/12/2026 23:00" {
		t.Fatalf("m = %v", m)
	}
	if Anterior(map[string]any{"id": 1}) {
		t.Fatal("sem anterior não muda nada")
	}
}
