package central

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/BasisTI/orq/internal/refs"
)

// Tipos de pendência.
var TiposPendencia = []string{"duvida", "autorizacao", "decisao"}

// Pendencia é uma dúvida, autorização ou decisão que espera resposta. O número é sequencial
// por US e nunca é reaproveitado; o id exibido é TG-312#P2.
type Pendencia struct {
	ID          string  `json:"id"`
	US          string  `json:"us"` // projeto/ref
	Numero      int     `json:"numero"`
	Tipo        string  `json:"tipo"`
	Texto       string  `json:"texto"`
	PaneOrigem  *string `json:"pane_origem"`
	Aberta      bool    `json:"aberta"`
	Resposta    *string `json:"resposta"`
	AbertaEm    string  `json:"aberta_em"`
	ResolvidaEm *string `json:"resolvida_em"`
	OndaID      int64   `json:"onda_id"`
	Coordenador string  `json:"coordenador"`

	// Chave é projeto/ref#P<n>, o que orq pend ver aceita sem ambiguidade.
	Chave string `json:"chave"`
	// Estado deriva das colunas: aberta, respondida (aberta com resposta da pessoa) ou resolvida.
	Estado string `json:"estado"`
	// TemDetalhe é falso nas pendências antigas, que têm só o título.
	TemDetalhe bool `json:"tem_detalhe"`
	// Detalhe é o markdown inteiro; só orq pend ver e a página da pendência o carregam.
	Detalhe       *string `json:"detalhe,omitempty"`
	Opcoes        []Opcao `json:"opcoes"`
	Slug          *string `json:"slug"`
	Arquivo       *string `json:"arquivo"`
	Autor         *string `json:"autor"`
	RespostaOpcao *int    `json:"resposta_opcao"`
	RespondidaEm  *string `json:"respondida_em"`
	RespondidaPor *string `json:"respondida_por"`
	AvisadaEm     *string `json:"avisada_em"`
	// RespostaVersao sobe a cada troca de resposta; o aviso confirma a versão que entregou.
	RespostaVersao int `json:"resposta_versao"`
	// ArquivoAlterado: o arquivo mudou depois do registro; a pendência não muda com ele.
	ArquivoAlterado bool `json:"arquivo_alterado"`
	// SemCoordenador: respondida e o aviso ao coordenador falhou porque ele não existe mais.
	SemCoordenador bool `json:"sem_coordenador"`
}

// RefUS é a US da pendência, com o projeto.
func (p Pendencia) RefUS() refs.US {
	projeto, n, _ := strings.Cut(p.US, "/")
	ref, _ := strconv.Atoi(n)
	return refs.US{Projeto: projeto, Ref: ref}
}

// Opcao é uma resposta possível de uma pendência.
type Opcao struct {
	ID          int    `json:"id"`
	Titulo      string `json:"titulo"`
	Descricao   string `json:"descricao"`
	Recomendada bool   `json:"recomendada"`
}

// Estados de uma pendência, derivados de aberta e respondida_em.
const (
	EstadoAberta     = "aberta"
	EstadoRespondida = "respondida"
	EstadoResolvida  = "resolvida"
)

// AbrirPendencia grava a pendência com o próximo número da US.
func (s *Store) AbrirPendencia(ctx context.Context, coord *Coordenador, ref refs.US, tipo, texto, pane string) (Pendencia, error) {
	if !contem(TiposPendencia, tipo) {
		return Pendencia{}, uso(fmt.Sprintf("tipo %q inválido", tipo), "use "+strings.Join(TiposPendencia, ", "))
	}
	if strings.TrimSpace(texto) == "" {
		return Pendencia{}, uso("pendência sem texto", `passe o texto entre aspas: orq pend add TG-312 --tipo duvida "…"`)
	}
	if temControle(pane) {
		return Pendencia{}, uso("pane com caractere de controle", "")
	}
	var alvo usAlvo
	var numero int
	err := s.escrever(ctx, func(tx *sql.Tx) error {
		var err error
		if alvo, err = resolverUS(ctx, tx, ref, coord); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `UPDATE us SET proxima_pendencia = proxima_pendencia + 1 WHERE projeto = ? AND ref = ? RETURNING proxima_pendencia - 1`, alvo.Projeto, alvo.Ref).Scan(&numero); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO pendencia (projeto, ref, numero, tipo, texto, pane_origem, aberta, aberta_em) VALUES (?, ?, ?, ?, ?, ?, 1, ?)`,
			alvo.Projeto, alvo.Ref, numero, tipo, texto, nulo(pane), s.agora()); err != nil {
			return err
		}
		return s.registrar(ctx, tx, evento{coordenador: autor(coord, alvo), ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: "pend.add",
			detalhe: map[string]any{"id": refs.PendID(alvo.Ref, numero), "tipo": tipo, "texto": texto, "pane_origem": nulo(pane)}})
	})
	if err != nil {
		return Pendencia{}, err
	}
	return s.verPendencia(ctx, alvo.US, numero)
}

// ResolverPendencia fecha a pendência. Sem resposta, vale a que a pessoa registrou (a web ou
// orq pend responder): o texto dela ou, se escolheu só uma opção, "opção N (título)".
func (s *Store) ResolverPendencia(ctx context.Context, coord *Coordenador, p refs.Pend, resposta string) (Pendencia, error) {
	var alvo usAlvo
	err := s.escrever(ctx, func(tx *sql.Tx) error {
		var err error
		if alvo, err = resolverUS(ctx, tx, p.US, coord); err != nil {
			return err
		}
		id := refs.PendID(alvo.Ref, p.Numero)
		var aberta bool
		var resolvida, atual, opcoesJSON *string
		var opcao *int
		err = tx.QueryRowContext(ctx, `SELECT aberta, resolvida_em, resposta, resposta_opcao, opcoes FROM pendencia WHERE projeto = ? AND ref = ? AND numero = ?`, alvo.Projeto, alvo.Ref, p.Numero).
			Scan(&aberta, &resolvida, &atual, &opcao, &opcoesJSON)
		if semLinha(err) {
			return naoEncontrado(fmt.Sprintf("%s não existe em %s", id, alvo.Chave()), "veja as pendências com orq pend list")
		}
		if err != nil {
			return err
		}
		if !aberta {
			quando := ""
			if resolvida != nil {
				quando = " em " + *resolvida
			}
			return conflito("pendencia_resolvida", id+" já foi resolvida"+quando, "")
		}
		if resposta == "" {
			resposta = respostaRegistrada(atual, opcao, opcoesJSON)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE pendencia SET aberta = 0, resposta = ?, resolvida_em = ? WHERE projeto = ? AND ref = ? AND numero = ?`,
			nulo(resposta), s.agora(), alvo.Projeto, alvo.Ref, p.Numero); err != nil {
			return err
		}
		return s.registrar(ctx, tx, evento{coordenador: autor(coord, alvo), ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: "pend.resolver",
			detalhe: map[string]any{"id": id, "resposta": nulo(resposta)}})
	})
	if err != nil {
		return Pendencia{}, err
	}
	return s.verPendencia(ctx, alvo.US, p.Numero)
}

// respostaRegistrada é o texto da resposta da pessoa, ou a opção que ela escolheu.
func respostaRegistrada(texto *string, opcao *int, opcoesJSON *string) string {
	if texto != nil && *texto != "" {
		return *texto
	}
	if opcao == nil {
		return ""
	}
	if o, ok := achar(decodificarOpcoes(opcoesJSON), *opcao); ok {
		return fmt.Sprintf("opção %d (%s)", o.ID, o.Titulo)
	}
	return fmt.Sprintf("opção %d", *opcao)
}

func achar(os []Opcao, id int) (Opcao, bool) {
	for _, o := range os {
		if o.ID == id {
			return o, true
		}
	}
	return Opcao{}, false
}

func decodificarOpcoes(j *string) []Opcao {
	os := []Opcao{}
	if j != nil && *j != "" {
		_ = json.Unmarshal([]byte(*j), &os)
	}
	return os
}

// FiltroPendencias restringe orq pend list e as páginas da web.
type FiltroPendencias struct {
	Abertas     bool // aberta = 1: inclui as respondidas, que ainda não foram resolvidas
	Respondidas bool // só as respondidas e ainda não resolvidas
	Resolvidas  bool
	Coord       string
	OndaID      int64
	Projeto     string
	// Limite e Deslocamento paginam (Limite 0 não limita); a ordem com Resolvidas é a mais
	// recente primeiro.
	Limite, Deslocamento int
}

func (f FiltroPendencias) onde() (string, []any) {
	var where []string
	var args []any
	switch {
	case f.Resolvidas:
		where = append(where, "p.aberta = 0")
	case f.Respondidas:
		where = append(where, "p.aberta = 1 AND p.respondida_em IS NOT NULL")
	case f.Abertas:
		where = append(where, "p.aberta = 1")
	}
	if f.Coord != "" {
		where, args = append(where, "onda.coordenador = ?"), append(args, f.Coord)
	}
	if f.OndaID != 0 {
		where, args = append(where, "us.onda_id = ?"), append(args, f.OndaID)
	}
	if f.Projeto != "" {
		where, args = append(where, "p.projeto = ?"), append(args, f.Projeto)
	}
	if len(where) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(where, " AND "), args
}

// ListarPendencias devolve as pendências por data de abertura (as resolvidas, pela resolução
// mais recente), sem o detalhe.
func (s *Store) ListarPendencias(ctx context.Context, f FiltroPendencias) ([]Pendencia, error) {
	clause, args := f.onde()
	if f.Resolvidas {
		clause += " ORDER BY p.resolvida_em DESC, p.id DESC"
	} else {
		clause += " ORDER BY p.aberta_em, p.id"
	}
	if f.Limite > 0 {
		clause += " LIMIT ? OFFSET ?"
		args = append(args, f.Limite, f.Deslocamento)
	}
	var ps []Pendencia
	err := s.ler(ctx, func(tx *sql.Tx) error {
		var err error
		ps, err = carregarPendenciasSQL(ctx, tx, false, clause, args...)
		return err
	})
	return ps, err
}

// ContarPendencias conta as que ListarPendencias devolveria sem a paginação.
func (s *Store) ContarPendencias(ctx context.Context, f FiltroPendencias) (int, error) {
	clause, args := f.onde()
	var n int
	err := s.ler(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pendencia p JOIN us ON us.projeto = p.projeto AND us.ref = p.ref JOIN onda ON onda.id = us.onda_id `+clause, args...).Scan(&n)
	})
	return n, err
}

// VerPendencia devolve a pendência com o detalhe completo; coord, se houver, desfaz a ambiguidade
// de um TG-312 sem projeto.
func (s *Store) VerPendencia(ctx context.Context, coord *Coordenador, p refs.Pend) (Pendencia, error) {
	var ps []Pendencia
	err := s.ler(ctx, func(tx *sql.Tx) error {
		alvo, err := resolverUS(ctx, tx, p.US, coord)
		if err != nil {
			return err
		}
		ps, err = carregarPendenciasSQL(ctx, tx, true, `WHERE p.projeto = ? AND p.ref = ? AND p.numero = ?`, alvo.Projeto, alvo.Ref, p.Numero)
		return err
	})
	if err != nil {
		return Pendencia{}, err
	}
	if len(ps) == 0 {
		return Pendencia{}, naoEncontrado(refs.PendID(p.US.Ref, p.Numero)+" não existe", "veja as pendências com orq pend list")
	}
	return ps[0], nil
}

func (s *Store) verPendencia(ctx context.Context, u refs.US, numero int) (Pendencia, error) {
	var ps []Pendencia
	err := s.ler(ctx, func(tx *sql.Tx) error {
		var err error
		ps, err = carregarPendenciasSQL(ctx, tx, false, `WHERE p.projeto = ? AND p.ref = ? AND p.numero = ?`, u.Projeto, u.Ref, numero)
		return err
	})
	if err != nil {
		return Pendencia{}, err
	}
	if len(ps) == 0 {
		return Pendencia{}, naoEncontrado(refs.PendID(u.Ref, numero)+" não existe", "")
	}
	return ps[0], nil
}

func carregarPendencias(ctx context.Context, q querier, clause string, args ...any) ([]Pendencia, error) {
	return carregarPendenciasSQL(ctx, q, false, clause+" ORDER BY p.aberta_em, p.id", args...)
}

// carregarPendenciasSQL lê as pendências do WHERE (e do que vier depois dele) em clause. O
// detalhe, que chega a 64 KiB por pendência, só vem quando pedido.
func carregarPendenciasSQL(ctx context.Context, q querier, comDetalhe bool, clause string, args ...any) ([]Pendencia, error) {
	detalhe := "NULL"
	if comDetalhe {
		detalhe = "p.detalhe"
	}
	rows, err := q.QueryContext(ctx, `SELECT p.projeto, p.ref, p.numero, p.tipo, p.texto, p.pane_origem, p.aberta, p.resposta, p.aberta_em, p.resolvida_em, us.onda_id, onda.coordenador,
		p.detalhe IS NOT NULL, `+detalhe+`, p.opcoes, p.slug, p.arquivo, p.autor, p.resposta_opcao, p.respondida_em, p.respondida_por, p.avisada_em, p.resposta_versao, p.arquivo_alterado IS NOT NULL, p.aviso_falhou_em IS NOT NULL
		FROM pendencia p JOIN us ON us.projeto = p.projeto AND us.ref = p.ref JOIN onda ON onda.id = us.onda_id `+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ps := []Pendencia{}
	for rows.Next() {
		var p Pendencia
		var u refs.US
		var opcoes *string
		var falhou bool
		if err := rows.Scan(&u.Projeto, &u.Ref, &p.Numero, &p.Tipo, &p.Texto, &p.PaneOrigem, &p.Aberta, &p.Resposta, &p.AbertaEm, &p.ResolvidaEm, &p.OndaID, &p.Coordenador,
			&p.TemDetalhe, &p.Detalhe, &opcoes, &p.Slug, &p.Arquivo, &p.Autor, &p.RespostaOpcao, &p.RespondidaEm, &p.RespondidaPor, &p.AvisadaEm, &p.RespostaVersao, &p.ArquivoAlterado, &falhou); err != nil {
			return nil, err
		}
		p.ID, p.US = refs.PendID(u.Ref, p.Numero), u.Chave()
		p.Chave = p.US + "#P" + strconv.Itoa(p.Numero)
		p.Opcoes = decodificarOpcoes(opcoes)
		switch {
		case !p.Aberta:
			p.Estado = EstadoResolvida
		case p.RespondidaEm != nil:
			p.Estado = EstadoRespondida
			p.SemCoordenador = falhou && p.AvisadaEm == nil
		default:
			p.Estado = EstadoAberta
		}
		ps = append(ps, p)
	}
	return ps, rows.Err()
}
