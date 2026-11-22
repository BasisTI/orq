package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// limiteTituloAviso corta o título que vai na notificação: ela é um aviso, não a pendência.
const limiteTituloAviso = 120

// Aviso é um item que o navegador pode notificar. O JSON de /avisos leva só isto: o id estável
// para deduplicar, a chave, o título, o projeto e o link. O texto livre da pessoa (a resposta
// dela a uma pendência, a pergunta ao coordenador) e o corpo da resposta do coordenador nunca
// entram aqui, e por isso não chegam à notificação.
type Aviso struct {
	// ID é estável entre consultas e entre abas: "p:<chave>@<aberta_em>" ou "m:<id da mensagem>".
	ID string `json:"id"`
	// Tipo é "pendencia" ou "resposta".
	Tipo    string `json:"tipo"`
	Chave   string `json:"chave"`
	Titulo  string `json:"titulo"`
	Projeto string `json:"projeto,omitempty"`
	// Href é um caminho da própria Central (começa em "/").
	Href string `json:"href"`
}

// Avisos é o corpo de GET /avisos.
type Avisos struct {
	Itens []Aviso `json:"itens"`
}

// montarAvisos seleciona do snapshot o que a Central avisa: as pendências abertas que esperam a
// pessoa (as já respondidas por ela não) e as mensagens das ondas abertas que o coordenador já
// respondeu. Não lê relógio nem banco. A ordem é estável: pendências na ordem do snapshot, depois
// as respostas por onda e por id.
func montarAvisos(snap central.Snapshot) Avisos {
	out := Avisos{Itens: []Aviso{}}
	for _, p := range snap.PendenciasAbertas {
		if p.Estado != central.EstadoAberta {
			continue
		}
		u := p.RefUS()
		out.Itens = append(out.Itens, Aviso{
			ID: "p:" + p.Chave + "@" + p.AbertaEm, Tipo: "pendencia", Chave: p.Chave,
			Titulo: tituloAviso(p.Texto), Projeto: u.Projeto,
			Href: refs.RotaPendencia(u.Projeto, u.Ref, p.Numero),
		})
	}
	for _, c := range snap.Coordenadores {
		for _, o := range c.Ondas {
			for _, m := range o.Mensagens {
				if m.Resposta == nil || m.RespondidaEm == nil {
					continue
				}
				id := strconv.FormatInt(m.ID, 10)
				out.Itens = append(out.Itens, Aviso{
					ID: "m:" + id, Tipo: "resposta", Chave: "onda #" + strconv.FormatInt(o.ID, 10) + " mensagem #" + id,
					Titulo: tituloAviso(o.Titulo), Href: "/#msg-" + id,
				})
			}
		}
	}
	return out
}

// tituloAviso tira controles e espaços repetidos e corta no limite.
func tituloAviso(s string) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if r := []rune(s); len(r) > limiteTituloAviso {
		s = string(r[:limiteTituloAviso-1]) + "…"
	}
	return s
}

// avisos atende GET /avisos: o JSON que o app.js consulta de tempos em tempos para decidir o que
// notificar. Sem corpo de pendência, sem texto de pessoa; erro de leitura é 503 e o próximo ciclo
// tenta de novo.
func (s *Servidor) avisos(w http.ResponseWriter, r *http.Request) {
	snap, err := s.fonte().Snapshot(r.Context(), false)
	if err != nil {
		if s.Log != nil {
			s.Log("web: avisos: %v", err)
		}
		http.Error(w, "indisponível", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(montarAvisos(snap))
}
