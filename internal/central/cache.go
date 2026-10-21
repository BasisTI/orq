package central

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// Fontes dos enriquecedores, na ordem em que aparecem no snapshot.
const (
	FonteHerdr = "herdr"
	FonteMR    = "mr"
	FonteTaiga = "taiga"
)

// Fontes lista as fontes conhecidas.
var Fontes = []string{FonteHerdr, FonteMR, FonteTaiga}

// Erro é a última falha de uma consulta: código estável, mensagem da ferramenta e quando foi.
type Erro struct {
	Codigo   string `json:"codigo"`
	Mensagem string `json:"mensagem"`
	Em       string `json:"em"`
}

// Cache é uma linha do cache de uma fonte: o último valor bom (Valor nil se nunca houve) e,
// quando a última consulta falhou, o erro dela.
type Cache struct {
	Chave        string
	Valor        json.RawMessage
	ValorEm      *string
	ConsultadoEm string
	Erro         *Erro
}

// SessaoHerdr é o valor em cache do Herdr: os panes e workspaces que existiam na consulta.
type SessaoHerdr struct {
	Panes      map[string]PaneHerdr      `json:"panes"`
	Workspaces map[string]WorkspaceHerdr `json:"workspaces"`
}

// PaneHerdr é o que o orq guarda de um pane. Status é o agent_status do Herdr (unknown num
// pane sem agente).
type PaneHerdr struct {
	Status      string            `json:"status"`
	Agente      string            `json:"agente,omitempty"`
	Nome        string            `json:"nome,omitempty"`
	WorkspaceID string            `json:"workspace_id"`
	Tokens      map[string]string `json:"tokens,omitempty"`
}

// WorkspaceHerdr é o que o orq guarda de um workspace.
type WorkspaceHerdr struct {
	Tokens map[string]string `json:"tokens,omitempty"`
}

// ValorMR é o valor em cache de uma MR.
type ValorMR struct {
	Estado string `json:"estado"`
	WebURL string `json:"web_url,omitempty"`
	// Pipeline (sucesso|falhou|rodando|pendente|sem) e Aprovacao (aprovada|pendente|nao_exigida)
	// ficam vazios quando a forja não os deu: o snapshot os traz como nulos (desconhecidos).
	Pipeline  string `json:"pipeline,omitempty"`
	Aprovacao string `json:"aprovacao,omitempty"`
	// SituacaoEm é quando Pipeline e Aprovacao foram lidos da forja pela última vez.
	// SituacaoErro é a falha da consulta extra (GitHub) que não conseguiu renová-los: o estado da
	// MR foi gravado, os dois campos são os de SituacaoEm. GravarMR preenche os dois.
	SituacaoEm   string `json:"situacao_em,omitempty"`
	SituacaoErro *Erro  `json:"situacao_erro,omitempty"`
}

// ValorTaiga é o valor em cache de uma US no Taiga.
type ValorTaiga struct {
	Titulo string `json:"titulo"`
	Status string `json:"status"`
	URL    string `json:"url"`
}

// LerCache devolve as linhas de cache da fonte, por chave.
func (s *Store) LerCache(ctx context.Context, fonte string) (map[string]Cache, error) {
	out := map[string]Cache{}
	err := s.ler(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = lerCache(ctx, tx, fonte)
		return err
	})
	return out, err
}

func lerCache(ctx context.Context, q querier, fonte string) (map[string]Cache, error) {
	rows, err := q.QueryContext(ctx, `SELECT chave, valor, valor_em, consultado_em, erro_codigo, erro_mensagem, erro_em FROM cache WHERE fonte = ?`, fonte)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Cache{}
	for rows.Next() {
		var c Cache
		var valor, codigo, mensagem, em *string
		if err := rows.Scan(&c.Chave, &valor, &c.ValorEm, &c.ConsultadoEm, &codigo, &mensagem, &em); err != nil {
			return nil, err
		}
		if valor != nil {
			c.Valor = json.RawMessage(*valor)
		}
		c.Erro = erroDe(codigo, mensagem, em)
		out[c.Chave] = c
	}
	return out, rows.Err()
}

func erroDe(codigo, mensagem, em *string) *Erro {
	if codigo == nil {
		return nil
	}
	e := &Erro{Codigo: *codigo}
	if mensagem != nil {
		e.Mensagem = *mensagem
	}
	if em != nil {
		e.Em = *em
	}
	return e
}

// GravarCache registra uma consulta. Com erro, o valor anterior fica (é o último valor bom);
// sem erro, valor substitui o anterior e o erro some. Cache não é estado da Central e não gera
// evento.
func (s *Store) GravarCache(ctx context.Context, fonte, chave string, valor any, erro *Erro) error {
	return s.escrever(ctx, func(tx *sql.Tx) error { return s.gravarCache(ctx, tx, fonte, chave, valor, erro) })
}

func (s *Store) gravarCache(ctx context.Context, tx *sql.Tx, fonte, chave string, valor any, erro *Erro) error {
	agora := s.agora()
	if erro != nil {
		_, err := tx.ExecContext(ctx, `INSERT INTO cache (fonte, chave, consultado_em, erro_codigo, erro_mensagem, erro_em) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (fonte, chave) DO UPDATE SET consultado_em = excluded.consultado_em,
			  erro_codigo = excluded.erro_codigo, erro_mensagem = excluded.erro_mensagem, erro_em = excluded.erro_em`,
			fonte, chave, agora, erro.Codigo, limitar(erro.Mensagem, 500), agora)
		return err
	}
	b, err := json.Marshal(valor)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO cache (fonte, chave, valor, valor_em, consultado_em) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (fonte, chave) DO UPDATE SET valor = excluded.valor, valor_em = excluded.valor_em,
		  consultado_em = excluded.consultado_em, erro_codigo = NULL, erro_mensagem = NULL, erro_em = NULL`,
		fonte, chave, string(b), agora, agora)
	return err
}

// GravarMR registra a consulta de uma MR e, sem erro, passa o estado para as linhas da MR. A
// mudança de estado é da Central: cada US com a MR ganha um evento mr.estado.
func (s *Store) GravarMR(ctx context.Context, url string, valor ValorMR, erro *Erro) error {
	return s.escrever(ctx, func(tx *sql.Tx) error {
		if erro != nil {
			return s.gravarCache(ctx, tx, FonteMR, url, nil, erro)
		}
		if valor.SituacaoErro != nil {
			// A consulta extra falhou: o estado é novo, pipeline e aprovação são os do último
			// valor bom (se houve), com a falha ao lado.
			valor.SituacaoErro.Em = s.agora()
			var ant *string
			err := tx.QueryRowContext(ctx, `SELECT valor FROM cache WHERE fonte = ? AND chave = ?`, FonteMR, url).Scan(&ant)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			var v ValorMR
			if ant != nil && json.Unmarshal([]byte(*ant), &v) == nil {
				valor.Pipeline, valor.Aprovacao, valor.SituacaoEm = v.Pipeline, v.Aprovacao, v.SituacaoEm
			}
		} else if valor.Pipeline != "" || valor.Aprovacao != "" {
			valor.SituacaoEm = s.agora()
		}
		if err := s.gravarCache(ctx, tx, FonteMR, url, valor, nil); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT mr.id, mr.projeto, mr.ref, mr.estado, onda.id, onda.coordenador FROM mr
			JOIN us ON us.projeto = mr.projeto AND us.ref = mr.ref JOIN onda ON onda.id = us.onda_id
			WHERE mr.url = ? AND (mr.estado IS NULL OR mr.estado <> ?)`, url, valor.Estado)
		if err != nil {
			return err
		}
		type mudanca struct {
			id      int64
			projeto string
			ref     int
			de      *string
			onda    int64
			coord   string
		}
		var ms []mudanca
		for rows.Next() {
			var m mudanca
			if err := rows.Scan(&m.id, &m.projeto, &m.ref, &m.de, &m.onda, &m.coord); err != nil {
				rows.Close()
				return err
			}
			ms = append(ms, m)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, m := range ms {
			if _, err := tx.ExecContext(ctx, `UPDATE mr SET estado = ? WHERE id = ?`, valor.Estado, m.id); err != nil {
				return err
			}
			if err := s.registrar(ctx, tx, evento{coordenador: m.coord, ondaID: m.onda, projeto: m.projeto, ref: m.ref, tipo: "mr.estado",
				detalhe: map[string]any{"url": url, "de": m.de, "para": valor.Estado}}); err != nil {
				return err
			}
		}
		return nil
	})
}

// Suspender para as chamadas a uma ferramenta depois de erro de autenticação.
func (s *Store) Suspender(ctx context.Context, ferramenta string, e Erro) error {
	return s.escrever(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO suspensao (ferramenta, codigo, mensagem, desde) VALUES (?, ?, ?, ?)
			ON CONFLICT (ferramenta) DO UPDATE SET codigo = excluded.codigo, mensagem = excluded.mensagem, desde = excluded.desde`,
			ferramenta, e.Codigo, limitar(e.Mensagem, 500), s.agora())
		return err
	})
}

// Retomar tira a suspensão da ferramenta.
func (s *Store) Retomar(ctx context.Context, ferramenta string) error {
	return s.escrever(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM suspensao WHERE ferramenta = ?`, ferramenta)
		return err
	})
}

// Suspensoes devolve as ferramentas suspensas; Erro.Em é o início da suspensão.
func (s *Store) Suspensoes(ctx context.Context) (map[string]Erro, error) {
	var out map[string]Erro
	err := s.ler(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = suspensoes(ctx, tx)
		return err
	})
	return out, err
}

func suspensoes(ctx context.Context, q querier) (map[string]Erro, error) {
	rows, err := q.QueryContext(ctx, `SELECT ferramenta, codigo, mensagem, desde FROM suspensao ORDER BY ferramenta`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Erro{}
	for rows.Next() {
		var f string
		var e Erro
		if err := rows.Scan(&f, &e.Codigo, &e.Mensagem, &e.Em); err != nil {
			return nil, err
		}
		out[f] = e
	}
	return out, rows.Err()
}

// Token é um token de sidebar que o orq publicou num pane ou workspace do Herdr.
type Token struct {
	Alvo   string `json:"alvo"` // pane ou workspace
	AlvoID string `json:"alvo_id"`
	Nome   string `json:"nome"`
	Valor  string `json:"valor"`
}

// TokensPublicados devolve os tokens que o orq publicou e ainda não limpou.
func (s *Store) TokensPublicados(ctx context.Context) ([]Token, error) {
	ts := []Token{}
	err := s.ler(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT alvo, alvo_id, nome, valor FROM token_publicado ORDER BY alvo, alvo_id, nome`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t Token
			if err := rows.Scan(&t.Alvo, &t.AlvoID, &t.Nome, &t.Valor); err != nil {
				return err
			}
			ts = append(ts, t)
		}
		return rows.Err()
	})
	return ts, err
}

// MarcarToken registra que o token foi publicado com esse valor.
func (s *Store) MarcarToken(ctx context.Context, t Token) error {
	return s.escrever(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO token_publicado (alvo, alvo_id, nome, valor, publicado_em) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (alvo, alvo_id, nome) DO UPDATE SET valor = excluded.valor, publicado_em = excluded.publicado_em`,
			t.Alvo, t.AlvoID, t.Nome, t.Valor, s.agora())
		return err
	})
}

// DesmarcarToken esquece um token limpo (ou cujo alvo sumiu).
func (s *Store) DesmarcarToken(ctx context.Context, t Token) error {
	return s.escrever(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM token_publicado WHERE alvo = ? AND alvo_id = ? AND nome = ?`, t.Alvo, t.AlvoID, t.Nome)
		return err
	})
}

// Herdr é o estado vivo de um pane no snapshot.
type Herdr struct {
	Status       *string `json:"status"`
	Agente       *string `json:"agente"`
	Nome         *string `json:"nome"`
	WorkspaceID  *string `json:"workspace_id"`
	AtualizadoEm *string `json:"atualizado_em"`
	Erro         *Erro   `json:"erro"`
}

// Taiga é o que o Taiga diz de uma US no snapshot.
type Taiga struct {
	Titulo       *string `json:"titulo"`
	Status       *string `json:"status"`
	URL          *string `json:"url"`
	AtualizadoEm *string `json:"atualizado_em"`
	Erro         *Erro   `json:"erro"`
}

// Fonte resume uma fonte no snapshot.
type Fonte struct {
	ConsultadoEm *string `json:"consultado_em"`
	AtualizadoEm *string `json:"atualizado_em"`
	Erro         *Erro   `json:"erro"`
}

// herdrCache é a sessão do Herdr em cache, lida uma vez por leitura.
type herdrCache struct {
	sessao *SessaoHerdr
	linha  *Cache
}

func lerHerdr(ctx context.Context, q querier) (herdrCache, error) {
	cs, err := lerCache(ctx, q, FonteHerdr)
	if err != nil {
		return herdrCache{}, err
	}
	c, ok := cs[""]
	if !ok {
		return herdrCache{}, nil
	}
	h := herdrCache{linha: &c}
	if c.Valor != nil {
		var s SessaoHerdr
		if json.Unmarshal(c.Valor, &s) == nil {
			h.sessao = &s
		}
	}
	return h, nil
}

// pane devolve o estado vivo de um pane: nulo sem pane, ausente quando o Herdr respondeu e o
// pane não estava lá.
func (h herdrCache) pane(id *string) *Herdr {
	if id == nil {
		return nil
	}
	out := &Herdr{}
	if h.linha == nil {
		return out
	}
	out.AtualizadoEm, out.Erro = h.linha.ValorEm, h.linha.Erro
	if h.sessao == nil {
		return out
	}
	p, ok := h.sessao.Panes[*id]
	if !ok {
		out.Status = ptr("ausente")
		return out
	}
	out.Status, out.WorkspaceID = ptr(p.Status), ptr(p.WorkspaceID)
	if p.Agente != "" {
		out.Agente = ptr(p.Agente)
	}
	if p.Nome != "" {
		out.Nome = ptr(p.Nome)
	}
	return out
}

func carregarTaiga(ctx context.Context, q querier, chave string) (Taiga, error) {
	var t Taiga
	var valor, codigo, mensagem, em *string
	err := q.QueryRowContext(ctx, `SELECT valor, valor_em, erro_codigo, erro_mensagem, erro_em FROM cache WHERE fonte = ? AND chave = ?`, FonteTaiga, chave).
		Scan(&valor, &t.AtualizadoEm, &codigo, &mensagem, &em)
	if semLinha(err) {
		return t, nil
	}
	if err != nil {
		return t, err
	}
	t.Erro = erroDe(codigo, mensagem, em)
	if valor != nil {
		var v ValorTaiga
		if json.Unmarshal([]byte(*valor), &v) == nil {
			t.Titulo, t.Status, t.URL = ptr(v.Titulo), ptr(v.Status), ptr(v.URL)
		}
	}
	return t, nil
}

// carregarFontes resume cada fonte: a consulta mais recente, o valor bom mais recente e o erro
// que vale para a fonte inteira (o do Herdr, ou a suspensão de uma ferramenta).
func carregarFontes(ctx context.Context, q querier) (map[string]Fonte, error) {
	out := map[string]Fonte{}
	for _, f := range Fontes {
		var consultado, atualizado *string
		if err := q.QueryRowContext(ctx, `SELECT max(consultado_em), max(valor_em) FROM cache WHERE fonte = ?`, f).Scan(&consultado, &atualizado); err != nil {
			return nil, err
		}
		out[f] = Fonte{ConsultadoEm: consultado, AtualizadoEm: atualizado}
	}
	h, err := lerHerdr(ctx, q)
	if err != nil {
		return nil, err
	}
	if h.linha != nil && h.linha.Erro != nil {
		f := out[FonteHerdr]
		f.Erro = h.linha.Erro
		out[FonteHerdr] = f
	}
	sus, err := suspensoes(ctx, q)
	if err != nil {
		return nil, err
	}
	nomes := make([]string, 0, len(sus))
	for n := range sus {
		nomes = append(nomes, n)
	}
	sort.Strings(nomes)
	for _, ferramenta := range nomes {
		e := sus[ferramenta]
		fonte := FonteDaFerramenta(ferramenta)
		if f := out[fonte]; f.Erro == nil {
			e.Mensagem = ferramenta + " suspenso: " + e.Mensagem
			f.Erro = &e
			out[fonte] = f
		}
	}
	return out, nil
}

// FonteDaFerramenta diz a que fonte uma ferramenta serve.
func FonteDaFerramenta(ferramenta string) string {
	switch ferramenta {
	case "glab", "gh":
		return FonteMR
	case "taiga":
		return FonteTaiga
	}
	return FonteHerdr
}

func ptr(s string) *string { return &s }

// limitar corta s em n bytes sem partir runa.
func limitar(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	for n > 0 && (s[n]&0xC0) == 0x80 {
		n--
	}
	return s[:n]
}

// LerSessaoHerdr devolve a última sessão boa do Herdr (nil se nunca houve) e o erro da última
// consulta, quando ela falhou.
func (s *Store) LerSessaoHerdr(ctx context.Context) (*SessaoHerdr, *Erro, error) {
	var h herdrCache
	err := s.ler(ctx, func(tx *sql.Tx) error {
		var err error
		h, err = lerHerdr(ctx, tx)
		return err
	})
	if err != nil || h.linha == nil {
		return nil, nil, err
	}
	return h.sessao, h.linha.Erro, nil
}
