package enriquecer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/BasisTI/orq/internal/central"
)

// RenomeioPane é o resultado de pôr o nome da onda no pane do coordenador. Rotulo e Agente
// dizem o que ficou com o nome; Erros trazem o que o Herdr recusou (a onda não depende disso) e
// Notas o que não é falha (pane sem agente: só o rótulo tem o que renomear).
type RenomeioPane struct {
	Coordenador string   `json:"coordenador"`
	Pane        string   `json:"pane"`
	OndaID      int64    `json:"onda_id"`
	Nome        string   `json:"nome"`
	Rotulo      bool     `json:"rotulo"`
	Agente      bool     `json:"agente"`
	Notas       []string `json:"notas,omitempty"`
	Erros       []string `json:"erros,omitempty"`
}

// Evento é o registro do diário (onda.pane_nome) desse renomeio.
func (r RenomeioPane) Evento() central.EventoExterno {
	d := map[string]any{"pane": r.Pane, "nome": r.Nome, "rotulo": r.Rotulo, "agente": r.Agente}
	if len(r.Notas) > 0 {
		d["notas"] = r.Notas
	}
	if len(r.Erros) > 0 {
		d["erros"] = r.Erros
	}
	return central.EventoExterno{Tipo: "onda.pane_nome", Detalhe: d}
}

// Resumo é a linha de texto do renomeio, para a saída dos comandos e o log do daemon.
func (r RenomeioPane) Resumo() string {
	var ok []string
	if r.Rotulo {
		ok = append(ok, "rótulo")
	}
	if r.Agente {
		ok = append(ok, "agente")
	}
	s := fmt.Sprintf("pane %s do coordenador %s", r.Pane, r.Coordenador)
	if len(ok) > 0 {
		s += ": " + strings.Join(ok, " e ") + " com o nome " + r.Nome
	}
	if len(r.Erros) > 0 {
		s += " (falhou: " + strings.Join(r.Erros, "; ") + ")"
	}
	return s
}

// RenomearPane dá o nome ao rótulo (`herdr pane rename`) e ao agente (`herdr agent rename`) do
// pane. comAgente falso pula o agente: o pane não tem um. Uma falha de um não impede o outro.
// É escrita no Herdr, só sobre o pane do coordenador que o orq registrou.
func (f Ferramentas) RenomearPane(ctx context.Context, pane, nome string, rotulo, agente bool) (r RenomeioPane) {
	r = RenomeioPane{Pane: pane, Nome: nome}
	if rotulo {
		if _, err := f.Rodar(ctx, "herdr", "pane", "rename", pane, nome); err != nil {
			r.Erros = append(r.Erros, "pane rename: "+descreverHerdr(err))
		} else {
			r.Rotulo = true
		}
	}
	if agente {
		if _, err := f.Rodar(ctx, "herdr", "agent", "rename", pane, nome); err != nil {
			if codigoHerdr(err) == "agent_not_found" {
				r.Notas = append(r.Notas, "o pane não tem agente registrado no Herdr: só o rótulo foi renomeado")
			} else {
				r.Erros = append(r.Erros, "agent rename: "+descreverHerdr(err))
			}
		} else {
			r.Agente = true
		}
	}
	return r
}

// SincronizarNomePane põe no pane do coordenador o nome da onda aberta mais recente dele, sem
// olhar o estado atual do Herdr (é o que os comandos que criam, ampliam e encerram ondas fazem).
// Nulo quando não há o que renomear: coordenador sem pane, sem onda aberta ou com a onda sem nome.
// Grava o resultado no diário, e falha do Herdr vai no resultado, nunca no erro.
func (f Ferramentas) SincronizarNomePane(ctx context.Context, st *central.Store, coord string) (*RenomeioPane, error) {
	p, err := st.PaneEsperadoDe(ctx, coord)
	if err != nil || p == nil || p.Nome == "" {
		return nil, err
	}
	r := f.RenomearPane(ctx, p.Pane, p.Nome, true, true)
	r.Coordenador, r.OndaID = p.Coordenador, p.OndaID
	if err := st.RegistrarExternosOnda(ctx, p.OndaID, []central.EventoExterno{r.Evento()}); err != nil {
		return &r, err
	}
	return &r, nil
}

// Nomes é a conferência do daemon: compara o rótulo e o nome do agente do pane de cada
// coordenador com o nome da onda aberta mais recente dele e corrige o que divergiu. Pane que o
// Herdr não mostra mais fica como está. Falha do Herdr vai para o log, uma vez por mudança.
type Nomes struct {
	St  *central.Store
	F   Ferramentas
	Log func(format string, args ...any)

	// falhas guarda, por pane, a última falha registrada: a mesma recusa do Herdr não volta ao
	// diário nem ao log a cada tick.
	falhas map[string]string
}

// Ciclo é uma conferência. Só consulta o Herdr se algum coordenador tem pane e onda aberta.
func (n *Nomes) Ciclo(ctx context.Context) error {
	ps, err := n.St.PanesEsperados(ctx)
	if err != nil {
		return err
	}
	var esperados []central.PaneEsperado
	for _, p := range ps {
		if p.Nome != "" {
			esperados = append(esperados, p)
		}
	}
	if len(esperados) == 0 {
		return nil
	}
	sessao, err := n.F.ConsultarHerdr(ctx)
	if err != nil {
		return err
	}
	for _, p := range esperados {
		vivo, ok := sessao.Panes[p.Pane]
		if !ok {
			continue
		}
		// Agente: só se o pane tem um (Agente é o tipo, claude, codex…).
		precisaRotulo := vivo.Rotulo != p.Nome
		precisaAgente := vivo.Agente != "" && vivo.Nome != p.Nome
		if !precisaRotulo && !precisaAgente {
			continue
		}
		r := n.F.RenomearPane(ctx, p.Pane, p.Nome, precisaRotulo, precisaAgente)
		r.Coordenador, r.OndaID = p.Coordenador, p.OndaID
		if n.falhas == nil {
			n.falhas = map[string]string{}
		}
		if len(r.Erros) > 0 {
			chave := p.Nome + "|" + strings.Join(r.Erros, ";")
			if n.falhas[p.Pane] == chave {
				continue
			}
			n.falhas[p.Pane] = chave
		} else {
			delete(n.falhas, p.Pane)
		}
		if err := n.St.RegistrarExternosOnda(ctx, p.OndaID, []central.EventoExterno{r.Evento()}); err != nil {
			return err
		}
		if n.Log != nil {
			n.Log("nome do pane divergia da onda %d: %s", p.OndaID, r.Resumo())
		}
	}
	return nil
}

// codigoHerdr é o code do JSON de erro que o herdr escreve (no stderr ou no stdout).
func codigoHerdr(err error) string {
	f := ComoFalha(err)
	if f == nil {
		return ""
	}
	for _, b := range [][]byte{f.Erro, f.Saida} {
		var e struct {
			Error *struct{ Code string } `json:"error"`
		}
		if json.Unmarshal(b, &e) == nil && e.Error != nil {
			return e.Error.Code
		}
	}
	return ""
}

func descreverHerdr(err error) string {
	if c := codigoHerdr(err); c != "" {
		return c
	}
	return err.Error()
}
