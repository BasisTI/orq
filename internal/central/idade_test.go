package central

import (
	"testing"
	"time"
)

func TestIdade(t *testing.T) {
	agora := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	for d, quer := range map[time.Duration]string{30 * time.Second: "agora", 12 * time.Minute: "há 12 min", 3 * time.Hour: "há 3 h", 72 * time.Hour: "há 3 d"} {
		if s := Idade(agora.Add(-d).Format(time.RFC3339), agora); s != quer {
			t.Errorf("%v = %q, quer %q", d, s, quer)
		}
	}
	if s := Idade("lixo", agora); s != "?" {
		t.Errorf("lixo = %q", s)
	}
}

func TestVencido(t *testing.T) {
	agora := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	em := func(d time.Duration) *string { s := agora.Add(-d).Format(time.RFC3339); return &s }
	casos := []struct {
		fonte string
		ts    *string
		quer  bool
	}{
		{FonteHerdr, em(10 * time.Second), false},
		{FonteHerdr, em(16 * time.Second), true},
		{FonteMR, em(3 * time.Minute), false},
		{FonteMR, em(4 * time.Minute), true},
		{FonteTaiga, em(10 * time.Minute), false},
		{FonteTaiga, em(16 * time.Minute), true},
		{FonteTaiga, nil, false},
		{"outra", em(time.Hour), false},
	}
	for _, c := range casos {
		if got := Vencido(c.fonte, c.ts, agora); got != c.quer {
			t.Errorf("%s %v = %v", c.fonte, c.ts, got)
		}
	}
	if Vencido(FonteMR, new("lixo"), agora) {
		t.Error("data ilegível não vence")
	}
}
