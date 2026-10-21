package pendencias

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
)

// Erros que um Avisador devolve para o ciclo decidir o que fazer com o aviso.
var (
	// ErrOcupado: o coordenador existe mas não está parado (working, blocked); tenta no ciclo seguinte.
	ErrOcupado = errors.New("coordenador ocupado")
	// ErrSemCoordenador: não há agente nesse pane (ou o coordenador não tem pane registrado).
	ErrSemCoordenador = errors.New("coordenador não encontrado")
	// ErrSemAvisador: não há meio de avisar; o aviso fica devido, sem evento.
	ErrSemAvisador = errors.New("sem avisador")
)

// Destino é o coordenador que recebe o aviso.
type Destino struct {
	Coordenador string
	Pane        string // pane do Herdr; vazio se o coordenador não tem pane registrado
}

// Avisador entrega um texto de uma linha ao coordenador. O acoplamento com o Herdr fica só aqui:
// o resto do fluxo da pendência não sabe que ele existe.
type Avisador interface {
	Avisar(ctx context.Context, d Destino, texto string) error
}

// Nulo não avisa ninguém. Serve para o daemon sem Herdr e para os testes; nada é marcado como
// avisado, então o aviso sai quando houver um Avisador de verdade.
type Nulo struct{}

func (Nulo) Avisar(context.Context, Destino, string) error { return ErrSemAvisador }

// Herdr avisa pelo `herdr agent prompt`, só com o coordenador parado (idle ou done).
type Herdr struct{ F enriquecer.Ferramentas }

func (h Herdr) Avisar(ctx context.Context, d Destino, texto string) error {
	if d.Pane == "" {
		return ErrSemCoordenador
	}
	out, err := h.F.Rodar(ctx, "herdr", "agent", "get", d.Pane)
	if err != nil {
		return traduzirHerdr(err)
	}
	var r struct {
		Result struct {
			Agent struct {
				Status string `json:"agent_status"`
			} `json:"agent"`
		} `json:"result"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return fmt.Errorf("herdr agent get: resposta inválida: %w", err)
	}
	if r.Error != nil {
		return traduzirCodigo(r.Error.Code, "herdr agent get")
	}
	if s := r.Result.Agent.Status; s != "idle" && s != "done" {
		return ErrOcupado
	}
	if _, err := h.F.Rodar(ctx, "herdr", "agent", "prompt", d.Pane, texto); err != nil {
		return traduzirHerdr(err)
	}
	return nil
}

// traduzirHerdr olha o JSON de erro que o herdr escreve (no stderr, com saída 1).
func traduzirHerdr(err error) error {
	f := enriquecer.ComoFalha(err)
	for _, b := range [][]byte{f.Erro, f.Saida} {
		var e struct {
			Error *struct{ Code string } `json:"error"`
		}
		if json.Unmarshal(b, &e) == nil && e.Error != nil {
			return traduzirCodigo(e.Error.Code, "herdr")
		}
	}
	return err
}

func traduzirCodigo(code, quem string) error {
	switch code {
	case "agent_not_found":
		return ErrSemCoordenador
	case "agent_blocked", "agent_busy":
		return ErrOcupado
	}
	return fmt.Errorf("%s: %s", quem, code)
}

// Servico é o ciclo do daemon para as pendências: importa os arquivos novos e avisa o que falta.
type Servico struct {
	St  *central.Store
	Imp *Importador
	Av  Avisador
	Log func(format string, args ...any)

	ultimoErro string
}

func (s *Servico) logf(f string, a ...any) {
	if s.Log != nil {
		s.Log(f, a...)
	}
}

// Ciclo importa e avisa. Falha de um passo não impede o outro; o primeiro erro volta no fim.
func (s *Servico) Ciclo(ctx context.Context) error {
	_, err1 := s.Imp.Varrer(ctx)
	err2 := s.Avisar(ctx)
	return errors.Join(err1, err2)
}

// Avisar entrega os avisos devidos. Manda no máximo um por coordenador a cada ciclo: depois do
// primeiro, o agente vai a working e o próximo espera a vez.
func (s *Servico) Avisar(ctx context.Context) error {
	avisos, err := s.St.AvisosPendentes(ctx)
	if err != nil || len(avisos) == 0 {
		return err
	}
	atendidos := map[string]bool{}
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
		err := s.Av.Avisar(ctx, Destino{Coordenador: a.Coordenador, Pane: pane}, TextoAviso(a, s.Imp.BaseURL))
		switch {
		case err == nil:
			s.ultimoErro = ""
			atendidos[a.Coordenador] = true
			if err := s.St.MarcarAvisada(ctx, a); err != nil && primeiro == nil {
				primeiro = err
			}
			s.logf("aviso %s de %s enviado a %s", a.Tipo, a.Pendencia.ID, a.Coordenador)
		case errors.Is(err, ErrOcupado), errors.Is(err, ErrSemAvisador):
			atendidos[a.Coordenador] = true
		case errors.Is(err, ErrSemCoordenador):
			if err := s.St.RegistrarFalhaAviso(ctx, a, "agent_not_found"); err != nil && primeiro == nil {
				primeiro = err
			}
		default:
			// O mesmo erro a cada ciclo (herdr fora do ar, por exemplo) entra no log uma vez.
			if msg := err.Error(); msg != s.ultimoErro {
				s.ultimoErro = msg
				s.logf("aviso %s de %s a %s: %v", a.Tipo, a.Pendencia.ID, a.Coordenador, err)
			}
			atendidos[a.Coordenador] = true
		}
	}
	return primeiro
}

// TextoAviso monta a linha que o coordenador recebe. O texto livre da pessoa nunca entra: o
// coordenador o lê com orq pend ver, que o mostra sem interpretar. Título de pendência e de
// opção vêm de outro agente, então saem numa linha só e sem controle.
func TextoAviso(a central.Aviso, base string) string {
	p := a.Pendencia
	us := p.RefUS()
	cab := fmt.Sprintf("[orq] %s %s pendência P%d", us.Projeto, refs.Exibir(us.Ref), p.Numero)
	if a.Tipo == central.AvisoAbertura {
		return fmt.Sprintf("%s aberta: %s — %s", cab, uma(p.Texto, 160), refs.URLPendencia(base, us.Projeto, us.Ref, p.Numero))
	}
	escolha := "sem opção"
	if a.Opcao != nil {
		escolha = fmt.Sprintf("opção %d (%s)", a.Opcao.ID, uma(a.Opcao.Titulo, 120))
	} else if p.RespostaOpcao != nil {
		escolha = fmt.Sprintf("opção %d", *p.RespostaOpcao)
	}
	return fmt.Sprintf("%s respondida: %s — leia com orq pend ver %s", cab, escolha, p.Chave)
}

// uma deixa s numa linha só, sem caractere de controle, com no máximo n runas.
func uma(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if output.UnsafeRune(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n-1]) + "…"
	}
	return s
}
