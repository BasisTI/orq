package pendencias

import (
	"context"
	"errors"
	"fmt"

	"github.com/BasisTI/orq/internal/central"
)

// TextoAvisoMensagem monta o prompt de uma linha que o daemon envia ao coordenador quando há uma
// mensagem nova da pessoa. O texto livre da pessoa nunca entra no prompt.
func TextoAvisoMensagem(m central.Mensagem) string {
	return fmt.Sprintf("[orq] onda #%d mensagem #%d — leia com orq msg ver %d e responda com orq msg responder %d", m.OndaID, m.ID, m.ID, m.ID)
}

// avisarMensagens entrega os avisos de mensagens ainda não entregues ao coordenador, compartilhando
// o mapa atendidos com os avisos de pendências para manter no máximo 1 aviso por coordenador por ciclo.
func (s *Servico) avisarMensagens(ctx context.Context, atendidos map[string]bool) error {
	avisos, err := s.St.MensagensParaAvisar(ctx)
	if err != nil || len(avisos) == 0 {
		return err
	}
	var primeiro error
	for _, a := range avisos {
		if ctx.Err() != nil {
			break
		}
		if atendidos[a.Coordenador] {
			continue
		}
		pane := ""
		if a.Pane != nil {
			pane = *a.Pane
		}
		err := s.Av.Avisar(ctx, Destino{Coordenador: a.Coordenador, Pane: pane}, TextoAvisoMensagem(a.Mensagem))
		switch {
		case err == nil:
			s.ultimoErro = ""
			atendidos[a.Coordenador] = true
			if err := s.St.MarcarMensagemAvisada(ctx, a.Mensagem.ID); err != nil && primeiro == nil {
				primeiro = err
			}
			s.logf("aviso de mensagem #%d (onda #%d) enviado a %s", a.Mensagem.ID, a.Mensagem.OndaID, a.Coordenador)
		case errors.Is(err, ErrOcupado), errors.Is(err, ErrSemAvisador):
			atendidos[a.Coordenador] = true
		case errors.Is(err, ErrSemCoordenador):
			if err := s.St.RegistrarFalhaAvisoMensagem(ctx, a.Mensagem.ID, "agent_not_found"); err != nil && primeiro == nil {
				primeiro = err
			}
		default:
			if msg := err.Error(); msg != s.ultimoErro {
				s.ultimoErro = msg
				s.logf("aviso de mensagem #%d (onda #%d) a %s: %v", a.Mensagem.ID, a.Mensagem.OndaID, a.Coordenador, err)
			}
			atendidos[a.Coordenador] = true
		}
	}
	return primeiro
}
