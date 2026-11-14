package central

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/BasisTI/orq/internal/refs"
)

func TestStatusGravaOModeloENoDiario(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	a := coord(t, s, "a", "w1:p1", "proj")
	o := onda(t, s, a, NovaOnda{Titulo: "o", US: []refs.US{us(t, "1")}})
	pane, m1, e1, m2 := "w1:p2", "claude-sonnet-5-5", "medium", "claude-fable-5-2"

	u, alterada, err := s.AtualizarStatus(ctx, &a, us(t, "1"), MudancaStatus{Status: "exec", Pane: &pane, Modelo: &m1, Esforco: &e1})
	if err != nil || !alterada || u.Modelo == nil || *u.Modelo != m1 || u.Esforco == nil || *u.Esforco != e1 {
		t.Fatalf("grava = %+v, %v, %v", u, alterada, err)
	}
	// Mesmo modelo de novo: nada a mudar, sem evento.
	if _, alterada, err = s.AtualizarStatus(ctx, &a, us(t, "1"), MudancaStatus{Status: "exec", Modelo: &m1, Esforco: &e1}); err != nil || alterada {
		t.Fatalf("repetir = %v, %v", alterada, err)
	}
	// Troca de modelo na mesma rodada: o diário guarda o de antes e o novo.
	if u, alterada, err = s.AtualizarStatus(ctx, &a, us(t, "1"), MudancaStatus{Status: "exec", Modelo: &m2}); err != nil || !alterada || *u.Modelo != m2 || *u.Esforco != e1 {
		t.Fatalf("trocar = %+v, %v, %v", u, alterada, err)
	}
	// Sem --modelo, o valor gravado fica.
	if u, _, err = s.AtualizarStatus(ctx, &a, us(t, "1"), MudancaStatus{Status: "rev"}); err != nil || *u.Modelo != m2 {
		t.Fatalf("manter = %+v, %v", u, err)
	}
	es, err := s.Diario(ctx, nil, FiltroDiario{OndaID: o.ID})
	if err != nil {
		t.Fatal(err)
	}
	var ds []string
	for _, e := range es {
		if e.Tipo == "us.status" {
			ds = append(ds, string(e.Detalhe))
		}
	}
	juntos := strings.Join(ds, "\n")
	for _, quer := range []string{`"modelo":"claude-sonnet-5-5"`, `"esforco":"medium"`, `"modelo":"claude-fable-5-2"`, `"modelo_antes":"claude-sonnet-5-5"`} {
		if !strings.Contains(juntos, quer) {
			t.Errorf("diário sem %s:\n%s", quer, juntos)
		}
	}
	if strings.Count(juntos, `"modelo":`) != 2 {
		t.Errorf("a repetição e a mudança de status não deviam gravar modelo:\n%s", juntos)
	}
	// O snapshot traz os campos.
	snap, _ := s.Snapshot(ctx, false)
	got := snap.Coordenadores[0].Ondas[0].US[0]
	if got.Modelo == nil || *got.Modelo != m2 || got.Esforco == nil || *got.Esforco != e1 {
		t.Fatalf("snapshot = %v %v", got.Modelo, got.Esforco)
	}
}

func TestStatusRecusaModeloInvalido(t *testing.T) {
	s := novoStore(t)
	ctx := context.Background()
	a := coord(t, s, "a", "w1:p1", "proj")
	onda(t, s, a, NovaOnda{Titulo: "o", US: []refs.US{us(t, "1")}})
	for _, v := range []string{"", "  ", "com espaço", "tab\tx", "ctl\x1b[31m", strings.Repeat("x", LimiteModelo+1)} {
		v := v
		for nome, m := range map[string]MudancaStatus{"modelo": {Status: "exec", Modelo: &v}, "esforco": {Status: "exec", Esforco: &v}} {
			if _, _, err := s.AtualizarStatus(ctx, &a, us(t, "1"), m); codigo(err) != "uso" {
				t.Errorf("%s %q: err = %v", nome, v, err)
			}
		}
	}
	// Recusar não grava nada.
	snap, _ := s.Snapshot(ctx, false)
	u := snap.Coordenadores[0].Ondas[0].US[0]
	if u.Status != "wait" || u.Modelo != nil {
		t.Fatalf("US mudou: %+v", u)
	}
}

// Banco já migrado pela 0007 e lido pelo binário anterior (Schema 6): ele abre sem migrar, lê e
// ainda escreve, e a escrita dele não apaga o modelo gravado.
func TestBancoComModeloAbreNoBinarioAnterior(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	novo, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := novo.RegistrarCoord(ctx, Registro{Nome: "a", Pane: "w1:p1", Projeto: "proj"})
	if err != nil {
		t.Fatal(err)
	}
	onda(t, novo, a, NovaOnda{Titulo: "o", US: []refs.US{us(t, "1")}})
	m := "claude-fable-5-2"
	if _, _, err := novo.AtualizarStatus(ctx, &a, us(t, "1"), MudancaStatus{Status: "exec", Modelo: &m}); err != nil {
		t.Fatal(err)
	}
	if e, err := novo.Versoes(ctx); err != nil || e.LeitorMinimo != 4 || e.EscritorMinimo != 5 || e.Banco != Schema() {
		t.Fatalf("versões = %+v, %v", e, err)
	}
	_ = novo.Close()

	// Binário 6: só as migrations 0001 a 0006 embutidas.
	anterior := fstest.MapFS{}
	es, _ := fs.ReadDir(migrationsFS, "migrations")
	for _, e := range es {
		if strings.HasPrefix(e.Name(), "0007") {
			continue
		}
		b, _ := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		anterior["migrations/"+e.Name()] = &fstest.MapFile{Data: b}
	}
	chamou := false
	velho, err := OpenCom(ctx, dir, Opcoes{Migracoes: anterior, AntesDeMigrar: func(Migracao) { chamou = true }})
	if err != nil {
		t.Fatalf("binário anterior não abre: %v", err)
	}
	defer velho.Close()
	if chamou {
		t.Fatal("o binário anterior não migra")
	}
	// As consultas do binário anterior (sem as colunas novas) seguem válidas.
	var status string
	if err := velho.db.QueryRowContext(ctx, `SELECT status FROM us WHERE projeto = 'proj' AND ref = 1`).Scan(&status); err != nil || status != "exec" {
		t.Fatalf("leitura antiga = %q, %v", status, err)
	}
	if _, err := velho.db.ExecContext(ctx, `UPDATE us SET status = ?, pane_id = COALESCE(?, pane_id), papel = COALESCE(?, papel), rodada = COALESCE(?, rodada), atualizado_em = ? WHERE projeto = 'proj' AND ref = 1`, "rev", nil, nil, nil, "2026-11-14T10:00:00Z"); err != nil {
		t.Fatalf("escrita antiga: %v", err)
	}
	snap, err := velho.Snapshot(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	u := snap.Coordenadores[0].Ondas[0].US[0]
	if u.Status != "rev" || u.Modelo == nil || *u.Modelo != m {
		t.Fatalf("US = %+v", u)
	}
}
