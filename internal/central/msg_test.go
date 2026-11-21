package central

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
)

func TestMensagensFluxoCompleto(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	t0 := time.Date(2026, 11, 21, 15, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return t0 }

	co, err := st.RegistrarCoord(ctx, Registro{Nome: "coord1", Pane: "wA:p1", Projeto: "orq"})
	if err != nil {
		t.Fatal(err)
	}
	o, err := st.CriarOnda(ctx, co, NovaOnda{Titulo: "Onda teste", Ate: "plano", US: []refs.US{{Projeto: "orq", Ref: 64}}})
	if err != nil {
		t.Fatal(err)
	}

	// Onda sem mensagens tem lista vazia (não nula) no snapshot.
	snap, err := st.Snapshot(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"mensagens":[]`) {
		t.Fatalf("snapshot sem mensagens não trouxe []: %s", raw)
	}

	// Enviar duas mensagens.
	m1, err := st.EnviarMensagem(ctx, o.ID, "Primeira pergunta\r\ncom linha 2", "cedric")
	if err != nil {
		t.Fatalf("EnviarMensagem 1: %v", err)
	}
	if m1.Texto != "Primeira pergunta\ncom linha 2" || m1.EnviadaPor != "cedric" || m1.OndaID != o.ID || m1.Coordenador != "coord1" {
		t.Fatalf("m1 = %+v", m1)
	}
	t0 = t0.Add(time.Minute)
	m2, err := st.EnviarMensagem(ctx, o.ID, "Segunda pergunta", "local")
	if err != nil {
		t.Fatalf("EnviarMensagem 2: %v", err)
	}

	// ListarMensagens devolve em ordem decrescente (m2 antes de m1).
	ms, err := st.ListarMensagens(ctx, FiltroMensagens{OndaID: o.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].ID != m2.ID || ms[1].ID != m1.ID {
		t.Fatalf("ordem decrescente incorreta: %+v", ms)
	}

	// Snapshot também traz Onda.Mensagens em ordem decrescente.
	snap, err = st.Snapshot(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	om := snap.Coordenadores[0].Ondas[0].Mensagens
	if len(om) != 2 || om[0].ID != m2.ID || om[1].ID != m1.ID {
		t.Fatalf("snapshot mensagens = %+v", om)
	}

	// MensagensParaAvisar traz em ordem crescente (m1 antes de m2) com o pane do coordenador.
	avisos, err := st.MensagensParaAvisar(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(avisos) != 2 || avisos[0].Mensagem.ID != m1.ID || avisos[0].Pane == nil || *avisos[0].Pane != "wA:p1" {
		t.Fatalf("avisos = %+v", avisos)
	}

	// Falha de aviso grava uma única vez.
	if err := st.RegistrarFalhaAvisoMensagem(ctx, m1.ID, "agent_not_found"); err != nil {
		t.Fatal(err)
	}
	if err := st.RegistrarFalhaAvisoMensagem(ctx, m1.ID, "agent_not_found"); err != nil {
		t.Fatal(err)
	}
	m1v, err := st.VerMensagem(ctx, m1.ID)
	if err != nil || !m1v.SemCoordenador || m1v.AvisoFalhouEm == nil {
		t.Fatalf("m1 após falha = %+v, %v", m1v, err)
	}

	// Marcar avisada limpa aviso_falhou_em e grava msg.avisada uma única vez.
	if err := st.MarcarMensagemAvisada(ctx, m1.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.MarcarMensagemAvisada(ctx, m1.ID); err != nil {
		t.Fatal(err)
	}
	m1v, err = st.VerMensagem(ctx, m1.ID)
	if err != nil || m1v.AvisadaEm == nil || m1v.AvisoFalhouEm != nil || m1v.SemCoordenador {
		t.Fatalf("m1 após avisada = %+v, %v", m1v, err)
	}

	// Responder mensagem uma vez funciona; segunda vez retorna mensagem_respondida (ExitConflict).
	resp, err := st.ResponderMensagem(ctx, &co, m1.ID, "**Resposta** em markdown")
	if err != nil {
		t.Fatalf("ResponderMensagem: %v", err)
	}
	if resp.Resposta == nil || *resp.Resposta != "**Resposta** em markdown" || resp.RespondidaPor == nil || *resp.RespondidaPor != "coord1" {
		t.Fatalf("resp = %+v", resp)
	}
	_, err = st.ResponderMensagem(ctx, &co, m1.ID, "segunda tentativa")
	if e := output.AsError(err); e.Code != "mensagem_respondida" || e.Exit != output.ExitConflict {
		t.Fatalf("segunda resposta = %+v", e)
	}

	// Filtro Abertas traz apenas m2.
	abertas, err := st.ListarMensagens(ctx, FiltroMensagens{Abertas: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(abertas) != 1 || abertas[0].ID != m2.ID {
		t.Fatalf("abertas = %+v", abertas)
	}

	// EncerramentoDe não é bloqueado por mensagens sem resposta.
	if _, _, err := st.AtualizarStatus(ctx, &co, refs.US{Projeto: "orq", Ref: 64}, MudancaStatus{Status: "done"}); err != nil {
		t.Fatal(err)
	}
	ov, err := st.VerOnda(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if enc := EncerramentoDe(ov); enc == nil || !enc.Pronta {
		t.Fatalf("EncerramentoDe bloqueado por mensagem: %+v", enc)
	}

	// Conferir eventos gravados no diário.
	evs, err := st.Diario(ctx, nil, FiltroDiario{OndaID: o.ID})
	if err != nil {
		t.Fatal(err)
	}
	contagem := map[string]int{}
	for _, ev := range evs {
		contagem[ev.Tipo]++
	}
	if contagem["msg.enviar"] != 2 || contagem["msg.aviso_falhou"] != 1 || contagem["msg.avisada"] != 1 || contagem["msg.responder"] != 1 {
		t.Fatalf("contagem eventos = %+v", contagem)
	}
}

func TestMensagensValidacoesEOndaEncerrada(t *testing.T) {
	ctx := context.Background()
	st, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	co, err := st.RegistrarCoord(ctx, Registro{Nome: "coord1", Pane: "wA:p1", Projeto: "orq"})
	if err != nil {
		t.Fatal(err)
	}
	o, err := st.CriarOnda(ctx, co, NovaOnda{Titulo: "Onda", Ate: "plano", US: []refs.US{{Projeto: "orq", Ref: 1}}})
	if err != nil {
		t.Fatal(err)
	}

	// Onda inexistente.
	if _, err := st.EnviarMensagem(ctx, 999, "oi", "ana"); output.AsError(err).Exit != output.ExitNotFound {
		t.Fatalf("onda inexistente = %v", err)
	}
	// Texto vazio, controle, acima de 8 KiB.
	for _, txt := range []string{"", "   \n\t ", "com\x00nulo", "com\x1besc", strings.Repeat("a", LimiteMensagem+1)} {
		if _, err := st.EnviarMensagem(ctx, o.ID, txt, "ana"); output.AsError(err).Exit != output.ExitUsage {
			t.Errorf("texto inválido %q: err = %v", txt[:min(len(txt), 16)], err)
		}
	}

	// Resposta até 64 KiB aceita; 64 KiB + 1 recusa.
	m, err := st.EnviarMensagem(ctx, o.ID, "pergunta", "ana")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ResponderMensagem(ctx, nil, m.ID, strings.Repeat("r", LimiteRespostaMensagem+1)); output.AsError(err).Exit != output.ExitUsage {
		t.Fatalf("resposta > 64KiB = %v", err)
	}
	if _, err := st.ResponderMensagem(ctx, nil, m.ID, "com\x07sino"); output.AsError(err).Exit != output.ExitUsage {
		t.Fatalf("resposta com controle = %v", err)
	}
	if _, err := st.ResponderMensagem(ctx, nil, m.ID, strings.Repeat("r", LimiteRespostaMensagem)); err != nil {
		t.Fatalf("resposta de 64KiB falhou: %v", err)
	}

	// Encerrar onda impede novo envio (409 onda_encerrada).
	if _, err := st.EncerrarOnda(ctx, "coord1", o.ID); err != nil {
		t.Fatal(err)
	}
	_, err = st.EnviarMensagem(ctx, o.ID, "tarde demais", "ana")
	if e := output.AsError(err); e.Code != "onda_encerrada" || e.Exit != output.ExitConflict {
		t.Fatalf("envio em onda encerrada = %+v", e)
	}
}
