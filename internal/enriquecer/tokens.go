package enriquecer

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/output"
)

// Tokens de sidebar do orq no Herdr.
const (
	FonteTokens     = "orq"     // --source dos report-metadata
	TokenResumo     = "orq"     // no pane do coordenador
	TokenMRsProntas = "orq_mrs" // no workspace do coordenador
	LimiteToken     = 80        // o Herdr corta o valor em 80 caracteres
)

// ResultadoTokens diz o que a publicação fez.
type ResultadoTokens struct {
	Publicados []central.Token `json:"publicados"`
	Limpos     []central.Token `json:"limpos"`
	Erros      []string        `json:"erros"`
	Avisos     []string        `json:"avisos"` // tokens com valor de outro produtor, que o orq não toca
	Pulado     *string         `json:"pulado"` // motivo, quando nada foi conferido
}

func novoResultadoTokens() ResultadoTokens {
	return ResultadoTokens{Publicados: []central.Token{}, Limpos: []central.Token{}, Erros: []string{}, Avisos: []string{}}
}

var rotulos = []struct{ status, rotulo string }{
	{"wait", "Wait"}, {"plan", "Plan"}, {"exec", "Exec"}, {"rev", "Rev"}, {"done", "Done"},
}

// Resumo é o token orq do coordenador: contagem das US das ondas abertas por status e das
// pendências abertas, ex. `2 Exec·1 Rev·1 pend`, com ⚠ na frente quando algum pane de US está
// blocked. Sem onda aberta, vazio (o token é limpo).
func Resumo(c central.CoordSnapshot) string {
	n := map[string]int{}
	pend, bloqueado, total := 0, false, 0
	for _, o := range c.Ondas {
		for _, u := range o.US {
			total++
			n[u.Status]++
			if u.Herdr != nil && u.Herdr.Status != nil && *u.Herdr.Status == "blocked" {
				bloqueado = true
			}
			for _, p := range u.Pendencias {
				if p.Aberta {
					pend++
				}
			}
		}
	}
	if total == 0 {
		return ""
	}
	var partes []string
	for _, r := range rotulos {
		if n[r.status] > 0 {
			partes = append(partes, strconv.Itoa(n[r.status])+" "+r.rotulo)
		}
	}
	if pend > 0 {
		partes = append(partes, strconv.Itoa(pend)+" pend")
	}
	s := strings.Join(partes, "·")
	if bloqueado {
		s = "⚠ " + s
	}
	return cortar(s, LimiteToken)
}

// RefsProntas devolve as refs curtas das MRs pronta das ondas abertas do coordenador: `!32`
// no GitLab e `repo#29` no GitHub.
func RefsProntas(c central.CoordSnapshot) []string {
	var rs []string
	for _, o := range c.Ondas {
		for _, u := range o.US {
			for _, m := range u.MRs {
				if m.Estado == nil || *m.Estado != "pronta" {
					continue
				}
				rs = append(rs, m.RefCurta())
			}
		}
	}
	return rs
}

// JuntarRefs junta as refs sem repetir, cabendo em 80 caracteres: o que não cabe vira `+N`.
func JuntarRefs(rs []string) string {
	var u []string
	visto := map[string]bool{}
	for _, r := range rs {
		if !visto[r] {
			visto[r] = true
			u = append(u, r)
		}
	}
	s := strings.Join(u, " ")
	if utf8.RuneCountInString(s) <= LimiteToken {
		return s
	}
	for i := len(u) - 1; i > 0; i-- {
		s = strings.Join(u[:i], " ") + " +" + strconv.Itoa(len(u)-i)
		if utf8.RuneCountInString(s) <= LimiteToken {
			return s
		}
	}
	return cortar(u[0], LimiteToken)
}

// cortar deixa s com até n runas, sem controles (o Herdr faz o mesmo ao gravar; se o orq não
// fizesse, o valor gravado nunca bateria com o publicado e o token seria republicado a cada tick).
func cortar(s string, n int) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if output.UnsafeRune(r) {
			return -1
		}
		return r
	}, s))
	if r := []rune(s); len(r) > n {
		return strings.TrimSpace(string(r[:n]))
	}
	return s
}

type chaveToken struct{ alvo, id, nome string }

func (k chaveToken) token(valor string) central.Token {
	return central.Token{Alvo: k.alvo, AlvoID: k.id, Nome: k.nome, Valor: valor}
}

// PublicarTokens confere os tokens do orq contra a última sessão do Herdr. No Herdr o token é
// por nome, não por fonte, então a propriedade vem do registro do orq (token_publicado): um
// token é do orq só enquanto o Herdr mostra o último valor que o orq publicou com sucesso.
// Publica o que está ausente (inclusive o que sumiu porque o servidor do Herdr reiniciou) ou
// ainda tem o valor antigo do orq; valor de outro produtor não é adotado, sobrescrito nem
// limpo, e vira aviso. Só toca panes e workspaces que existem na sessão.
func (e *Enriquecedor) PublicarTokens(ctx context.Context) (ResultadoTokens, error) {
	res := novoResultadoTokens()
	sessao, erro, err := e.St.LerSessaoHerdr(ctx)
	if err != nil {
		return res, err
	}
	if sessao == nil || erro != nil {
		motivo := "sem sessão do Herdr em cache"
		if erro != nil {
			motivo = "a última consulta ao Herdr falhou (" + erro.Codigo + ")"
		}
		res.Pulado = &motivo
		return res, nil
	}
	snap, err := e.St.Snapshot(ctx, false)
	if err != nil {
		return res, err
	}
	desejados := map[chaveToken]string{}
	refsPorWS := map[string][]string{}
	for _, c := range snap.Coordenadores {
		ws := ""
		if c.PaneID != nil {
			if p, ok := sessao.Panes[*c.PaneID]; ok {
				if r := Resumo(c); r != "" {
					desejados[chaveToken{"pane", *c.PaneID, TokenResumo}] = r
				}
				ws = p.WorkspaceID
			}
		}
		if c.WorkspaceID != nil {
			ws = *c.WorkspaceID
		}
		if _, ok := sessao.Workspaces[ws]; ok && ws != "" {
			refsPorWS[ws] = append(refsPorWS[ws], RefsProntas(c)...)
		}
	}
	for ws, rs := range refsPorWS {
		if v := JuntarRefs(rs); v != "" {
			desejados[chaveToken{"workspace", ws, TokenMRsProntas}] = v
		}
	}
	atual := sessaoTokens{sessao}.token
	pubs, err := e.St.TokensPublicados(ctx)
	if err != nil {
		return res, err
	}
	publicado := map[chaveToken]string{}
	for _, t := range pubs {
		publicado[chaveToken{t.Alvo, t.AlvoID, t.Nome}] = t.Valor
	}
	for _, k := range ordenar(desejados) {
		v := desejados[k]
		a, _ := atual(k)
		p, nosso := publicado[k]
		switch {
		case nosso && a == p && a == v:
			// Em dia.
		case a == "" || (nosso && a == p):
			// Ausente (o Herdr reiniciou, ou nunca foi publicado) ou é o valor antigo do orq.
			if err := e.reportar(ctx, k, &v); err != nil {
				res.Erros = append(res.Erros, err.Error())
				continue
			}
			res.Publicados = append(res.Publicados, k.token(v))
			if err := e.St.MarcarToken(ctx, k.token(v)); err != nil {
				return res, err
			}
		default:
			// Outro produtor escreveu esse nome (com o mesmo valor ou não): o token não é do
			// orq, que não o adota nem sobrescreve.
			res.Avisos = append(res.Avisos, alheio(k, a))
			if nosso {
				if err := e.St.DesmarcarToken(ctx, k.token("")); err != nil {
					return res, err
				}
			}
		}
	}
	for _, k := range ordenar(publicado) {
		if _, ok := desejados[k]; ok {
			continue
		}
		if err := e.limparSeNosso(ctx, k, publicado[k], atual, &res); err != nil {
			return res, err
		}
	}
	return res, nil
}

// limparSeNosso limpa o token só se o Herdr ainda mostra o último valor que o orq publicou;
// valor de outro produtor fica, e o registro é esquecido. Falha ao limpar mantém o registro
// para a próxima tentativa.
func (e *Enriquecedor) limparSeNosso(ctx context.Context, k chaveToken, publicado string, atual func(chaveToken) (string, bool), res *ResultadoTokens) error {
	a, existe := atual(k)
	if existe && a != "" {
		if a != publicado {
			res.Avisos = append(res.Avisos, alheio(k, a))
		} else {
			if err := e.reportar(ctx, k, nil); err != nil {
				res.Erros = append(res.Erros, err.Error())
				return nil
			}
			res.Limpos = append(res.Limpos, k.token(""))
		}
	}
	return e.St.DesmarcarToken(ctx, k.token(""))
}

func alheio(k chaveToken, valor string) string {
	return fmt.Sprintf("%s %s: o token %s tem %q, que o orq não publicou; fica como está", k.alvo, k.id, k.nome, valor)
}

// LimparTokens limpa o que o orq publicou e o Herdr ainda mostra com o valor do orq (o daemon
// chama ao sair, depois de reler o Herdr). Sem sessão confiável não limpa nada: não dá para
// saber se o token ainda é do orq.
func (e *Enriquecedor) LimparTokens(ctx context.Context) (ResultadoTokens, error) {
	res := novoResultadoTokens()
	sessao, erro, err := e.St.LerSessaoHerdr(ctx)
	if err != nil {
		return res, err
	}
	if sessao == nil || erro != nil {
		motivo := "sem sessão confiável do Herdr: tokens não conferidos nem limpos"
		res.Pulado = &motivo
		return res, nil
	}
	pubs, err := e.St.TokensPublicados(ctx)
	if err != nil {
		return res, err
	}
	for _, t := range pubs {
		if err := e.limparSeNosso(ctx, chaveToken{t.Alvo, t.AlvoID, t.Nome}, t.Valor, sessaoTokens{sessao}.token, &res); err != nil {
			return res, err
		}
	}
	return res, nil
}

// reportar publica (valor não nil) ou limpa o token pelo report-metadata do Herdr.
func (e *Enriquecedor) reportar(ctx context.Context, k chaveToken, valor *string) error {
	args := []string{k.alvo, "report-metadata", k.id, "--source", FonteTokens}
	if valor != nil {
		args = append(args, "--token", k.nome+"="+*valor)
	} else {
		args = append(args, "--clear-token", k.nome)
	}
	_, err := e.F.Rodar(ctx, "herdr", args...)
	return err
}

// sessaoTokens lê o valor atual de um token na sessão do Herdr e se o alvo existe.
type sessaoTokens struct{ *central.SessaoHerdr }

func (s sessaoTokens) token(k chaveToken) (string, bool) {
	if k.alvo == "pane" {
		p, ok := s.Panes[k.id]
		return p.Tokens[k.nome], ok
	}
	w, ok := s.Workspaces[k.id]
	return w.Tokens[k.nome], ok
}

func ordenar[V any](m map[chaveToken]V) []chaveToken {
	ks := make([]chaveToken, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool {
		if ks[i].alvo != ks[j].alvo {
			return ks[i].alvo < ks[j].alvo
		}
		if ks[i].id != ks[j].id {
			return ks[i].id < ks[j].id
		}
		return ks[i].nome < ks[j].nome
	})
	return ks
}
