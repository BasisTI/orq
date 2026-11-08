package hora

import (
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
