package central

import (
	"context"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/refs"
)

func TestEsperaRegistraTrocaELimpaComEvento(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	a := coord(t, s, "a", "w1:p1", "proj")
	o := onda(t, s, a, NovaOnda{Titulo: "o", US: []refs.US{us(t, "1")}})
	m1, m2 := "  Espera o eSocial publicar o arquivo  ", "Espera a carga corrigida"

	u, alterada, err := s.DefinirEspera(ctx, &a, us(t, "1"), &m1)
	if err != nil || !alterada || u.EsperaMotivo == nil || *u.EsperaMotivo != "Espera o eSocial publicar o arquivo" || u.Status != "wait" {
		t.Fatalf("registrar = %+v, %v, %v", u, alterada, err)
	}
	// Repetir o mesmo texto (aparado) não grava nem gera evento.
	if _, alterada, err = s.DefinirEspera(ctx, &a, us(t, "1"), &m1); err != nil || alterada {
		t.Fatalf("repetir = %v, %v", alterada, err)
	}
	if u, alterada, err = s.DefinirEspera(ctx, &a, us(t, "1"), &m2); err != nil || !alterada || *u.EsperaMotivo != m2 {
		t.Fatalf("trocar = %+v, %v, %v", u, alterada, err)
	}
	if u, alterada, err = s.DefinirEspera(ctx, &a, us(t, "1"), nil); err != nil || !alterada || u.EsperaMotivo != nil {
		t.Fatalf("limpar = %+v, %v, %v", u, alterada, err)
	}
	// Limpar o que já está limpo: sem mudança.
	if _, alterada, err = s.DefinirEspera(ctx, &a, us(t, "1"), nil); err != nil || alterada {
		t.Fatalf("limpar de novo = %v, %v", alterada, err)
	}
	es, err := s.Diario(ctx, nil, FiltroDiario{OndaID: o.ID})
	if err != nil {
		t.Fatal(err)
	}
	var ds []string
	for _, e := range es {
		if e.Tipo == "us.espera" {
			ds = append(ds, string(e.Detalhe))
		}
	}
	if len(ds) != 3 {
		t.Fatalf("%d eventos us.espera, queria 3 (registrar, trocar, limpar):\n%v", len(ds), ds)
	}
	juntos := strings.Join(ds, "\n")
	for _, quer := range []string{`"motivo":"Espera o eSocial publicar o arquivo"`, `"motivo_antes":"Espera o eSocial publicar o arquivo"`, `"motivo":"Espera a carga corrigida"`, `"limpou":true`, `"motivo_antes":"Espera a carga corrigida"`} {
		if !strings.Contains(juntos, quer) {
			t.Errorf("diário sem %s:\n%s", quer, juntos)
		}
	}
}

func TestEsperaNoSnapshotENaMudancaDeStatus(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	a := coord(t, s, "a", "w1:p1", "proj")
	onda(t, s, a, NovaOnda{Titulo: "o", US: []refs.US{us(t, "1"), us(t, "2")}})
	m := "Espera o colaboradados"
	if _, _, err := s.DefinirEspera(ctx, &a, us(t, "1"), &m); err != nil {
		t.Fatal(err)
	}
	// Mudar o status não apaga o motivo: quem limpa é o coordenador.
	if _, _, err := s.AtualizarStatus(ctx, &a, us(t, "1"), MudancaStatus{Status: "exec"}); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	us := snap.Coordenadores[0].Ondas[0].US
	if us[0].EsperaMotivo == nil || *us[0].EsperaMotivo != m || us[1].EsperaMotivo != nil {
		t.Fatalf("snapshot = %v / %v", us[0].EsperaMotivo, us[1].EsperaMotivo)
	}
}

func TestEsperaRecusaTextoInvalido(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	a := coord(t, s, "a", "w1:p1", "proj")
	onda(t, s, a, NovaOnda{Titulo: "o", US: []refs.US{us(t, "1")}})
	for nome, txt := range map[string]string{"vazio": "  ", "controle": "a\x1b[31mb", "quebra de linha": "a\nb", "longo": strings.Repeat("é", LimiteEspera+1)} {
		if _, _, err := s.DefinirEspera(ctx, &a, us(t, "1"), &txt); err == nil {
			t.Errorf("%s: aceitou %q", nome, txt)
		}
	}
	ok := strings.Repeat("é", LimiteEspera)
	if _, _, err := s.DefinirEspera(ctx, &a, us(t, "1"), &ok); err != nil {
		t.Errorf("no limite: %v", err)
	}
}
