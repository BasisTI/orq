package central

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/BasisTI/orq/internal/refs"
)

// Link público da spec (US #61): um código que não se adivinha abre a spec da US, só leitura, por um
// socket próprio. O central.db guarda só o SHA-256 do código; ele aparece uma única vez, na saída
// do gerar, e nunca vai para o diário nem para o log.

// Estados de um link.
const (
	LinkAtivo    = "ativo"
	LinkRevogado = "revogado"
	LinkExpirado = "expirado"
)

const (
	// BytesDoCodigo são os bytes aleatórios do código: 256 bits.
	BytesDoCodigo = 32
	// TamanhoDoCodigo é o tamanho do código em base64url sem padding.
	TamanhoDoCodigo = 43
	// LimiteRotuloLink é o maior rótulo, em caracteres.
	LimiteRotuloLink = 80
	// MaxValidadeLink é a maior validade que o --expira aceita.
	MaxValidadeLink = 10 * 365 * 24 * time.Hour
)

// LinkSpec é um link público como a listagem o mostra: sem o código, que o orq não guarda.
type LinkSpec struct {
	ID           int64   `json:"id"`
	Rotulo       string  `json:"rotulo"`
	CriadoEm     string  `json:"criado_em"`
	ExpiraEm     *string `json:"expira_em"`
	RevogadoEm   *string `json:"revogado_em"`
	Estado       string  `json:"estado"`
	UltimoAcesso *string `json:"ultimo_acesso"`
	Acessos      int     `json:"acessos"`
}

// LinkGerado é o resultado do gerar: o link e o código em claro, que só existe aqui.
type LinkGerado struct {
	LinkSpec
	Codigo string `json:"codigo"`
}

// NovoCodigoLink sorteia um código: 32 bytes de crypto/rand em base64url.
func NovoCodigoLink() (string, error) {
	b := make([]byte, BytesDoCodigo)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CodigoLinkValido diz se o texto tem a forma de um código (43 caracteres de base64url). Quem
// atende o socket público descarta antes de consultar o banco o que não tem.
func CodigoLinkValido(c string) bool {
	if len(c) != TamanhoDoCodigo {
		return false
	}
	for i := 0; i < len(c); i++ {
		ch := c[i]
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

// HashCodigoLink é o que o banco guarda do código: o SHA-256 em hexadecimal.
func HashCodigoLink(codigo string) string {
	h := sha256.Sum256([]byte(codigo))
	return hex.EncodeToString(h[:])
}

func estadoLink(revogado, expira *string, agora string) string {
	switch {
	case revogado != nil:
		return LinkRevogado
	case expira != nil && *expira <= agora:
		return LinkExpirado
	}
	return LinkAtivo
}

// GerarLink cria um link para a spec da US. validade zero é sem validade. O evento do diário traz o
// id, o rótulo e a validade, nunca o código nem o hash. Cada chamada cria um código novo: o gerar
// não devolve código já usado, revogado ou não.
func (s *Store) GerarLink(ctx context.Context, coord *Coordenador, ref refs.US, rotulo string, validade time.Duration) (LinkGerado, error) {
	rotulo = limparRotulo(rotulo)
	switch {
	case rotulo == "":
		return LinkGerado{}, uso("--para vazio", `diga para quem é o link, ex. --para "cliente X"`)
	case temControle(rotulo):
		return LinkGerado{}, uso("--para com caractere de controle", "")
	case utf8.RuneCountInString(rotulo) > LimiteRotuloLink:
		return LinkGerado{}, uso("--para passa de 80 caracteres", "use um rótulo curto")
	case validade < 0 || validade > MaxValidadeLink:
		return LinkGerado{}, uso("--expira fora do intervalo aceito", "use de 1m a 3650d, ex. --expira 30d")
	}
	codigo, err := NovoCodigoLink()
	if err != nil {
		return LinkGerado{}, err
	}
	var out LinkGerado
	err = s.escrever(ctx, func(tx *sql.Tx) error {
		alvo, err := resolverUS(ctx, tx, ref, coord)
		if err != nil {
			return err
		}
		if err := exigirSpec(ctx, tx, alvo); err != nil {
			return err
		}
		agora := s.Now().UTC()
		var expira *string
		if validade > 0 {
			e := agora.Add(validade).Format(time.RFC3339)
			expira = &e
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO spec_link (projeto, ref, hash, rotulo, criado_em, expira_em) VALUES (?, ?, ?, ?, ?, ?)`,
			alvo.Projeto, alvo.Ref, HashCodigoLink(codigo), rotulo, agora.Format(time.RFC3339), expira)
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		out = LinkGerado{LinkSpec: LinkSpec{ID: id, Rotulo: rotulo, CriadoEm: agora.Format(time.RFC3339), ExpiraEm: expira, Estado: LinkAtivo}, Codigo: codigo}
		return s.registrar(ctx, tx, evento{coordenador: autor(coord, alvo), ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: "spec.link.gerar",
			detalhe: map[string]any{"id": id, "rotulo": rotulo, "expira_em": expira}})
	})
	return out, err
}

// limparRotulo tira as pontas e junta os espaços internos.
func limparRotulo(r string) string {
	out := make([]rune, 0, len(r))
	espaco := false
	for _, c := range []rune(r) {
		if c == ' ' {
			espaco = true
			continue
		}
		if espaco && len(out) > 0 {
			out = append(out, ' ')
		}
		espaco = false
		out = append(out, c)
	}
	return string(out)
}

// RevogarLink revoga o link da spec da US, de forma definitiva. Link de outra US é "não
// encontrado"; já revogado, conflito.
func (s *Store) RevogarLink(ctx context.Context, coord *Coordenador, ref refs.US, id int64) (LinkSpec, error) {
	var out LinkSpec
	err := s.escrever(ctx, func(tx *sql.Tx) error {
		alvo, err := resolverUS(ctx, tx, ref, coord)
		if err != nil {
			return err
		}
		l, err := lerLink(ctx, tx, alvo.Projeto, alvo.Ref, id, s.agora())
		if err != nil {
			return err
		}
		if l == nil {
			return naoEncontrado(alvo.Chave()+" não tem o link "+strconv.FormatInt(id, 10), "veja os ids com orq spec link listar "+refs.Exibir(alvo.Ref))
		}
		if l.RevogadoEm != nil {
			return conflito("link_ja_revogado", alvo.Chave()+": o link "+strconv.FormatInt(id, 10)+" já está revogado", "gere outro com orq spec link gerar; a revogação é definitiva")
		}
		agora := s.agora()
		if _, err := tx.ExecContext(ctx, `UPDATE spec_link SET revogado_em = ? WHERE id = ?`, agora, id); err != nil {
			return err
		}
		l.RevogadoEm, l.Estado = &agora, LinkRevogado
		out = *l
		return s.registrar(ctx, tx, evento{coordenador: autor(coord, alvo), ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: "spec.link.revogar",
			detalhe: map[string]any{"id": id, "rotulo": l.Rotulo}})
	})
	return out, err
}

// ListarLinks lista os links da spec da US, do mais antigo ao mais novo. naoEncontrado se a US não
// tem spec registrada.
func (s *Store) ListarLinks(ctx context.Context, coord *Coordenador, ref refs.US) ([]LinkSpec, error) {
	out := []LinkSpec{}
	err := s.ler(ctx, func(tx *sql.Tx) error {
		alvo, err := resolverUS(ctx, tx, ref, coord)
		if err != nil {
			return err
		}
		sp, err := lerLinhaSpec(ctx, tx, alvo.Projeto, alvo.Ref)
		if err != nil {
			return err
		}
		if sp == nil {
			return naoEncontrado(alvo.Chave()+" não tem spec registrada", "registre com orq spec registrar "+refs.Exibir(alvo.Ref)+" --caminho <pasta> --mr <url>")
		}
		rows, err := tx.QueryContext(ctx, `SELECT id, rotulo, criado_em, expira_em, revogado_em, ultimo_acesso, acessos FROM spec_link WHERE projeto = ? AND ref = ? ORDER BY id`, alvo.Projeto, alvo.Ref)
		if err != nil {
			return err
		}
		defer rows.Close()
		agora := s.agora()
		for rows.Next() {
			l, err := lerLinhaLink(rows, agora)
			if err != nil {
				return err
			}
			out = append(out, l)
		}
		return rows.Err()
	})
	return out, err
}

func lerLinhaLink(r interface{ Scan(...any) error }, agora string) (LinkSpec, error) {
	var l LinkSpec
	if err := r.Scan(&l.ID, &l.Rotulo, &l.CriadoEm, &l.ExpiraEm, &l.RevogadoEm, &l.UltimoAcesso, &l.Acessos); err != nil {
		return l, err
	}
	l.Estado = estadoLink(l.RevogadoEm, l.ExpiraEm, agora)
	return l, nil
}

func lerLink(ctx context.Context, q querier, projeto string, ref int, id int64, agora string) (*LinkSpec, error) {
	l, err := lerLinhaLink(q.QueryRowContext(ctx, `SELECT id, rotulo, criado_em, expira_em, revogado_em, ultimo_acesso, acessos FROM spec_link WHERE projeto = ? AND ref = ? AND id = ?`, projeto, ref, id), agora)
	if semLinha(err) {
		return nil, nil
	}
	return &l, err
}

// contarLinksAtivos conta os links ativos (nem revogados nem expirados) da spec.
func contarLinksAtivos(ctx context.Context, q querier, projeto string, ref int, agora string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM spec_link WHERE projeto = ? AND ref = ? AND revogado_em IS NULL AND (expira_em IS NULL OR expira_em > ?)`, projeto, ref, agora).Scan(&n)
	return n, err
}

// MarcarAcesso registra um acesso válido ao link: último acesso e contador. Não é estado da
// Central: sem evento no diário. Link inexistente, revogado ou expirado não muda nada.
func (s *Store) MarcarAcesso(ctx context.Context, hash string) error {
	return s.escrever(ctx, func(tx *sql.Tx) error {
		agora := s.agora()
		_, err := tx.ExecContext(ctx, `UPDATE spec_link SET ultimo_acesso = ?, acessos = acessos + 1 WHERE hash = ? AND revogado_em IS NULL AND (expira_em IS NULL OR expira_em > ?)`, agora, hash, agora)
		return err
	})
}

// Estados que a página pública mostra: a faixa simplificada do que a pessoa de fora precisa saber.
const (
	PublicoEmValidacao = "em validação"
	PublicoAprovada    = "aprovada"
	PublicoEntregue    = "entregue"
)

// SpecPublica é tudo o que o socket público sabe de uma spec, e nada mais: sem ref, projeto, SHA,
// MR, pasta registrada, pendência ou pane. Os arquivos trazem o caminho no repositório só para o
// handler resolver os links relativos; ele não o desenha.
type SpecPublica struct {
	// Titulo é o título da US no Taiga; vazio quando o orq ainda não o leu.
	Titulo string
	// Estado é um dos Publico*.
	Estado string
	// Versao é a do registro da spec; SubstituidaEm, de quando é a atual (só com Versao a partir de 2).
	Versao        int
	SubstituidaEm string
	// Arquivos e LidaEm são a versão atual do cache; sem arquivos, o daemon ainda não leu a spec.
	Arquivos []ArquivoSpec
	LidaEm   string
}

// EstadoPublico traduz o estado da spec (e se o código foi entregue) no da faixa pública: "em
// validação" para registrada, em validação e mudança além do decisoes.md; "aprovada" para aprovada
// e para a MR da spec mesclada; "entregue" só com MR do código ligada à US mesclada, que não é o
// done da US nem a onda encerrada. Toda MR que já foi MR de spec da US (a atual e as anteriores a
// um --substituir) fica de fora da conta.
func EstadoPublico(estadoSpec string, codigoEntregue bool) string {
	switch {
	case codigoEntregue:
		return PublicoEntregue
	case estadoSpec == SpecAprovada || estadoSpec == SpecMesclada:
		return PublicoAprovada
	}
	return PublicoEmValidacao
}

// SpecPorHash devolve a spec do link de hash dado. Hash inexistente, link revogado e link expirado
// dão o mesmo erro "não encontrado": quem chama não consegue distingui-los.
func (s *Store) SpecPorHash(ctx context.Context, hash string) (SpecPublica, error) {
	var out SpecPublica
	err := s.ler(ctx, func(tx *sql.Tx) error {
		var projeto string
		var ref int
		var expira, revogado *string
		agora := s.agora()
		err := tx.QueryRowContext(ctx, `SELECT projeto, ref, expira_em, revogado_em FROM spec_link WHERE hash = ?`, hash).Scan(&projeto, &ref, &expira, &revogado)
		if err != nil && !semLinha(err) {
			return err
		}
		if semLinha(err) || estadoLink(revogado, expira, agora) != LinkAtivo {
			return naoEncontrado("link indisponível", "")
		}
		sp, err := carregarSpec(ctx, tx, projeto, ref)
		if err != nil {
			return err
		}
		if sp == nil {
			return naoEncontrado("link indisponível", "")
		}
		var entregue bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM mr WHERE projeto = ? AND ref = ? AND estado = 'mesclada' AND url <> ?
			AND url NOT IN (SELECT json_extract(detalhe, '$.mr_url') FROM evento WHERE tipo = 'spec.registrar' AND projeto = ? AND ref = ? AND json_extract(detalhe, '$.mr_url') IS NOT NULL)
			AND url NOT IN (SELECT json_extract(detalhe, '$.anterior.mr_url') FROM evento WHERE tipo = 'spec.registrar' AND projeto = ? AND ref = ? AND json_extract(detalhe, '$.anterior.mr_url') IS NOT NULL))`,
			projeto, ref, sp.MRURL, projeto, ref, projeto, ref).Scan(&entregue); err != nil {
			return err
		}
		out.Estado, out.Versao = EstadoPublico(sp.Estado, entregue), sp.Versao
		if sp.Versao >= 2 {
			out.SubstituidaEm = sp.AtualizadaEm
		}
		if t, err := carregarTaiga(ctx, tx, refs.US{Projeto: projeto, Ref: ref}.Chave()); err != nil {
			return err
		} else if t.Titulo != nil {
			out.Titulo = *t.Titulo
		}
		l, err := lerLeituraSpec(ctx, tx, projeto, ref)
		if err != nil || l == nil || l.SHA == nil {
			return err
		}
		v, err := lerVersaoSpec(ctx, tx, projeto, ref, *l.SHA)
		if err != nil || v == nil {
			return err
		}
		out.Arquivos, out.LidaEm = v.Arquivos, v.LidaEm
		return nil
	})
	return out, err
}
