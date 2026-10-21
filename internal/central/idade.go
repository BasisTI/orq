package central

import (
	"fmt"
	"time"
)

// PrazoVencido é a idade a partir da qual o valor em cache de uma fonte deixa de valer como
// atual: três vezes o TTL do enriquecedor (enriquecer.TTLPadrao). O TUI e a web mostram a idade
// pela mesma Idade; a web usa Vencido para marcar o que passou do prazo.
var PrazoVencido = map[string]time.Duration{
	FonteHerdr: 15 * time.Second,
	FonteMR:    3 * time.Minute,
	FonteTaiga: 15 * time.Minute,
}

// Idade escreve há quanto tempo foi ts (RFC 3339): agora, há 12 min, há 3 h, há 2 d.
func Idade(ts string, agora time.Time) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return "?"
	}
	d := agora.Sub(t)
	switch {
	case d < time.Minute:
		return "agora"
	case d < time.Hour:
		return fmt.Sprintf("há %d min", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("há %d h", int(d/time.Hour))
	default:
		return fmt.Sprintf("há %d d", int(d/(24*time.Hour)))
	}
}

// Vencido diz se o valor da fonte, atualizado em ts, passou do prazo. Sem ts (nunca houve
// valor) ou com ts ilegível não há o que vencer: devolve falso.
func Vencido(fonte string, ts *string, agora time.Time) bool {
	if ts == nil {
		return false
	}
	t, err := time.Parse(time.RFC3339, *ts)
	if err != nil {
		return false
	}
	p, ok := PrazoVencido[fonte]
	return ok && agora.Sub(t) > p
}
