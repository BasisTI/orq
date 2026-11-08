// Package hora formata data e hora para quem lê a interface (web, TUI, CLI em
// texto), sempre em America/Sao_Paulo e sem depender do fuso do host. O estado
// guardado e o JSON do `orq snapshot` seguem em RFC 3339 UTC.
package hora

import (
	"time"
	_ "time/tzdata" // o binário carrega o banco de fusos: não depende do do host
)

// Fuso é o fuso das datas exibidas.
var Fuso = carregar()

func carregar() *time.Location {
	l, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		// Com time/tzdata isto não acontece; o fallback é o BRT fixo (sem horário de verão desde 2019).
		return time.FixedZone("BRT", -3*60*60)
	}
	return l
}

const (
	layoutData     = "02/01/2006"
	layoutDataHora = "02/01/2006 15:04"
)

// Data formata t como DD/MM/YYYY em BRT.
func Data(t time.Time) string { return t.In(Fuso).Format(layoutData) }

// DataHora formata t como DD/MM/YYYY HH:MM em BRT.
func DataHora(t time.Time) string { return t.In(Fuso).Format(layoutDataHora) }

// DataHoraRFC converte um instante RFC 3339 em DD/MM/YYYY HH:MM (BRT). Texto
// que não é RFC 3339 volta como veio.
func DataHoraRFC(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return DataHora(t)
}

// DataRFC converte um instante RFC 3339 em DD/MM/YYYY (BRT). Texto que não é
// RFC 3339 volta como veio.
func DataRFC(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return Data(t)
}
