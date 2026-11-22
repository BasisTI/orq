package central

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/BasisTI/orq/internal/refs"
)

// Estados da spec de uma US, do registro à MR mesclada.
const (
	SpecRegistrada  = "registrada"
	SpecEmValidacao = "em_validacao"
	SpecAprovada    = "aprovada"
	// SpecMudou: depois da aprovação, a pasta da spec mudou além do decisoes.md.
	SpecMudou    = "mudou"
	SpecMesclada = "mesclada"
)

// Opções fixas da pendência de validação: o id 1 aprova, o 2 pede ajustes. Nenhuma é recomendada.
const (
	OpcaoAprovar      = 1
	OpcaoPedirAjustes = 2
	// ArquivoDecisoesSpec é o único arquivo da pasta da spec que pode mudar depois da aprovação.
	ArquivoDecisoesSpec = "decisoes.md"
)

// TituloValidacao é o título que o orq dá à pendência de validação, seja qual for o do arquivo.
func TituloValidacao(ref int) string {
	return fmt.Sprintf("Spec da %s pronta para validar", refs.Exibir(ref))
}

// OpcoesValidacao são as opções da pendência de validação.
func OpcoesValidacao() []Opcao {
	return []Opcao{{ID: OpcaoAprovar, Titulo: "Aprovar"}, {ID: OpcaoPedirAjustes, Titulo: "Pedir ajustes"}}
}

// ConferenciaSpec é o resultado de comparar a pasta da spec entre o SHA aprovado e a cabeça da MR.
// PodeMesclar vale quando nada da pasta mudou além do decisoes.md. ForaDaPasta é outro sinal: a MR
// também mexe em arquivos de fora da pasta (a MR não é só da spec, ou a base entrou na branch), e
// não reprova a spec.
type ConferenciaSpec struct {
	SHAAprovado     string   `json:"sha_aprovado"`
	SHACabeca       string   `json:"sha_cabeca"`
	PodeMesclar     bool     `json:"pode_mesclar"`
	MudouNaPasta    []string `json:"mudou_na_pasta"`
	ForaDaPasta     []string `json:"fora_da_pasta"`
	ForaDaPastaErro string   `json:"fora_da_pasta_erro,omitempty"`
}

// SpecUS é a spec registrada de uma US, com o estado derivado das pendências, da MR e da última
// conferência do daemon. É o campo `spec` do snapshot.
type SpecUS struct {
	Caminho      string `json:"caminho"`
	MRURL        string `json:"mr_url"`
	Estado       string `json:"estado"`
	RegistradaEm string `json:"registrada_em"`
	AtualizadaEm string `json:"atualizada_em"`
	// Validacao é a última pendência de validação (TG-59#P1); SHAValidacao, o SHA da cabeça da MR
	// que o orq gravou ao importá-la. SHAAprovado só existe quando essa pendência foi aprovada.
	Validacao    *string `json:"validacao"`
	SHAValidacao *string `json:"sha_validacao"`
	SHAAprovado  *string `json:"sha_aprovado"`
	// SHACabeca é a cabeça da MR na última conferência.
	SHACabeca       *string          `json:"sha_cabeca"`
	Conferencia     *ConferenciaSpec `json:"conferencia"`
	ConferidaEm     *string          `json:"conferida_em"`
	ConferenciaErro *Erro            `json:"conferencia_erro"`
}

// CaminhoSpecValido normaliza a pasta da spec: relativa à raiz do repositório, sem `..`, sem
// barra no fim e sem caractere de controle.
func CaminhoSpecValido(caminho string) (string, error) {
	c := strings.TrimSpace(caminho)
	if c == "" {
		return "", uso("--caminho vazio", "passe a pasta da spec relativa à raiz do repositório, ex. docs/specs/epico/tg-59-nome")
	}
	if temControle(c) || strings.Contains(c, `\`) {
		return "", uso("--caminho com caractere de controle ou barra invertida", "")
	}
	if strings.HasPrefix(c, "/") {
		return "", uso("--caminho precisa ser relativo à raiz do repositório", "tire a barra do começo")
	}
	for _, p := range strings.Split(c, "/") {
		if p == ".." {
			return "", uso("--caminho não pode ter `..`", "")
		}
	}
	c = path.Clean(c)
	if c == "." {
		return "", uso("--caminho aponta a raiz do repositório", "aponte a pasta da spec")
	}
	return c, nil
}

// ResultadoSpec diz o que RegistrarSpec fez.
type ResultadoSpec struct {
	Spec SpecUS `json:"spec"`
	// MRLigada: a MR da spec não estava ligada à US e foi ligada na mesma transação.
	MRLigada bool `json:"mr_ligada"`
	// Substituiu: havia outra spec registrada e --substituir a trocou; Anterior traz o registro velho.
	Substituiu bool    `json:"substituiu"`
	Anterior   *SpecUS `json:"anterior,omitempty"`
}

// RegistrarSpec registra a pasta e a MR da spec da US e liga a MR à US (como orq mr add), na
// mesma transação. Uma spec por US: se já há uma, recusa e mostra o registro atual; com
// substituir, troca, e o evento do diário guarda os valores anteriores e os novos.
func (s *Store) RegistrarSpec(ctx context.Context, coord *Coordenador, ref refs.US, caminho, rawMR string, substituir bool) (ResultadoSpec, error) {
	caminho, err := CaminhoSpecValido(caminho)
	if err != nil {
		return ResultadoSpec{}, err
	}
	m, err := refs.ParseMR(rawMR)
	if err != nil {
		return ResultadoSpec{}, uso(err.Error(), "")
	}
	var res ResultadoSpec
	var alvo usAlvo
	err = s.escrever(ctx, func(tx *sql.Tx) error {
		var err error
		if alvo, err = resolverUS(ctx, tx, ref, coord); err != nil {
			return err
		}
		antes, err := lerLinhaSpec(ctx, tx, alvo.Projeto, alvo.Ref)
		if err != nil {
			return err
		}
		if antes != nil {
			if !substituir {
				return conflito("spec_ja_registrada", fmt.Sprintf("%s já tem spec registrada: pasta %s, MR %s", alvo.Chave(), antes.Caminho, antes.MRURL),
					"avise o responsável e pergunte se substitui; com o sim, repita com --substituir")
			}
			res.Substituiu, res.Anterior = true, antes
		}
		if res.MRLigada, err = s.ligarMR(ctx, tx, coord, alvo, m); err != nil {
			return err
		}
		agora := s.agora()
		if antes == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO spec (projeto, ref, caminho, mr_url, registrada_em, atualizada_em) VALUES (?, ?, ?, ?, ?, ?)`,
				alvo.Projeto, alvo.Ref, caminho, m.URL, agora, agora)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE spec SET caminho = ?, mr_url = ?, atualizada_em = ? WHERE projeto = ? AND ref = ?`, caminho, m.URL, agora, alvo.Projeto, alvo.Ref)
			if err == nil {
				// A conferência era da spec velha.
				_, err = tx.ExecContext(ctx, `DELETE FROM spec_conferencia WHERE projeto = ? AND ref = ?`, alvo.Projeto, alvo.Ref)
			}
		}
		if err != nil {
			return err
		}
		detalhe := map[string]any{"caminho": caminho, "mr_url": m.URL, "mr_ligada": res.MRLigada, "substituiu": res.Substituiu}
		if antes != nil {
			detalhe["anterior"] = map[string]any{"caminho": antes.Caminho, "mr_url": antes.MRURL}
		}
		return s.registrar(ctx, tx, evento{coordenador: autor(coord, alvo), ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: "spec.registrar", detalhe: detalhe})
	})
	if err != nil {
		return ResultadoSpec{}, err
	}
	sp, err := s.LerSpec(ctx, nil, alvo.US)
	if err != nil {
		return ResultadoSpec{}, err
	}
	res.Spec = *sp
	return res, nil
}

// LerSpec devolve a spec da US com o estado; naoEncontrado se a US não tem spec registrada.
func (s *Store) LerSpec(ctx context.Context, coord *Coordenador, ref refs.US) (*SpecUS, error) {
	var out *SpecUS
	err := s.ler(ctx, func(tx *sql.Tx) error {
		alvo, err := resolverUS(ctx, tx, ref, coord)
		if err != nil {
			return err
		}
		if out, err = carregarSpec(ctx, tx, alvo.Projeto, alvo.Ref); err != nil {
			return err
		}
		if out == nil {
			return naoEncontrado(alvo.Chave()+" não tem spec registrada", "registre com orq spec registrar "+refs.Exibir(alvo.Ref)+" --caminho <pasta> --mr <url>")
		}
		return nil
	})
	return out, err
}

// SpecDaUS é LerSpec para quem já tem a US resolvida (o importador): nil, sem erro, se não há spec.
func (s *Store) SpecDaUS(ctx context.Context, u refs.US) (*SpecUS, error) {
	var out *SpecUS
	err := s.ler(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = carregarSpec(ctx, tx, u.Projeto, u.Ref)
		return err
	})
	return out, err
}

// lerLinhaSpec lê só a linha do registro (sem estado); nil se a US não tem spec.
func lerLinhaSpec(ctx context.Context, q querier, projeto string, ref int) (*SpecUS, error) {
	var sp SpecUS
	err := q.QueryRowContext(ctx, `SELECT caminho, mr_url, registrada_em, atualizada_em FROM spec WHERE projeto = ? AND ref = ?`, projeto, ref).
		Scan(&sp.Caminho, &sp.MRURL, &sp.RegistradaEm, &sp.AtualizadaEm)
	if semLinha(err) {
		return nil, nil
	}
	return &sp, err
}

// carregarSpec lê a spec da US com o estado derivado; nil se não há spec. O estado vem, nesta
// ordem, de: MR mesclada; (só vale validação aberta a partir do último registro: depois de
// --substituir, a anterior era da spec velha); a última pendência de validação (aberta: em validação; respondida com
// Aprovar: aprovada; qualquer outra: registrada); e, sobre a aprovada, da conferência do daemon
// quando ela é do SHA aprovado e achou mudança além do decisoes.md.
func carregarSpec(ctx context.Context, q querier, projeto string, ref int) (*SpecUS, error) {
	sp, err := lerLinhaSpec(ctx, q, projeto, ref)
	if err != nil || sp == nil {
		return sp, err
	}
	sp.Estado = SpecRegistrada
	var numero int
	var aberta bool
	var respondida *string
	var opcao *int
	var sha *string
	err = q.QueryRowContext(ctx, `SELECT numero, aberta, respondida_em, resposta_opcao, sha_validacao FROM pendencia
		WHERE projeto = ? AND ref = ? AND tipo = 'validacao' AND aberta_em >= ? ORDER BY numero DESC LIMIT 1`, projeto, ref, sp.AtualizadaEm).Scan(&numero, &aberta, &respondida, &opcao, &sha)
	switch {
	case semLinha(err):
	case err != nil:
		return nil, err
	default:
		id := refs.PendID(ref, numero)
		sp.Validacao, sp.SHAValidacao = &id, sha
		switch {
		case aberta && respondida == nil:
			sp.Estado = SpecEmValidacao
		case respondida != nil && opcao != nil && *opcao == OpcaoAprovar && sha != nil:
			sp.Estado, sp.SHAAprovado = SpecAprovada, sha
		}
	}
	var mrEstado *string
	if err := q.QueryRowContext(ctx, `SELECT estado FROM mr WHERE projeto = ? AND ref = ? AND url = ?`, projeto, ref, sp.MRURL).Scan(&mrEstado); err != nil && !semLinha(err) {
		return nil, err
	}
	conf, em, erro, err := lerConferenciaSpec(ctx, q, projeto, ref)
	if err != nil {
		return nil, err
	}
	sp.ConferidaEm, sp.ConferenciaErro = em, erro
	if conf != nil && sp.SHAAprovado != nil && conf.SHAAprovado == *sp.SHAAprovado {
		sp.Conferencia = conf
		sp.SHACabeca = &conf.SHACabeca
		if sp.Estado == SpecAprovada && !conf.PodeMesclar {
			sp.Estado = SpecMudou
		}
	}
	if mrEstado != nil && *mrEstado == "mesclada" {
		sp.Estado = SpecMesclada
	}
	return sp, nil
}

func lerConferenciaSpec(ctx context.Context, q querier, projeto string, ref int) (*ConferenciaSpec, *string, *Erro, error) {
	var valor, valorEm, codigo, mensagem, erroEm *string
	err := q.QueryRowContext(ctx, `SELECT valor, valor_em, erro_codigo, erro_mensagem, erro_em FROM spec_conferencia WHERE projeto = ? AND ref = ?`, projeto, ref).
		Scan(&valor, &valorEm, &codigo, &mensagem, &erroEm)
	if semLinha(err) {
		return nil, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	var conf *ConferenciaSpec
	if valor != nil {
		var c ConferenciaSpec
		if json.Unmarshal([]byte(*valor), &c) == nil {
			conf = &c
		}
	}
	return conf, valorEm, erroDe(codigo, mensagem, erroEm), nil
}

// SpecParaConferir é o que o daemon confere: uma spec aprovada e ainda não mesclada.
type SpecParaConferir struct {
	US          refs.US
	Caminho     string
	MRURL       string
	SHAAprovado string
	// Conferencia e Erro são a última conferência gravada (a do mesmo SHA aprovado) e a última falha.
	Conferencia *ConferenciaSpec
	Erro        *Erro
}

// SpecsParaConferir lista as specs aprovadas cuja MR ainda não foi mesclada.
func (s *Store) SpecsParaConferir(ctx context.Context) ([]SpecParaConferir, error) {
	var out []SpecParaConferir
	err := s.ler(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT projeto, ref FROM spec ORDER BY projeto, ref`)
		if err != nil {
			return err
		}
		var us []refs.US
		for rows.Next() {
			var u refs.US
			if err := rows.Scan(&u.Projeto, &u.Ref); err != nil {
				rows.Close()
				return err
			}
			us = append(us, u)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, u := range us {
			sp, err := carregarSpec(ctx, tx, u.Projeto, u.Ref)
			if err != nil {
				return err
			}
			if sp == nil || sp.SHAAprovado == nil || (sp.Estado != SpecAprovada && sp.Estado != SpecMudou) {
				continue
			}
			out = append(out, SpecParaConferir{US: u, Caminho: sp.Caminho, MRURL: sp.MRURL, SHAAprovado: *sp.SHAAprovado, Conferencia: sp.Conferencia, Erro: sp.ConferenciaErro})
		}
		return nil
	})
	return out, err
}

// GravarConferenciaSpec guarda o resultado da conferência do daemon (ou a falha dela). É cache:
// não é estado da Central e não gera evento. Com erro, o último valor bom fica.
func (s *Store) GravarConferenciaSpec(ctx context.Context, u refs.US, valor *ConferenciaSpec, erro *Erro) error {
	return s.escrever(ctx, func(tx *sql.Tx) error {
		agora := s.agora()
		if erro != nil {
			_, err := tx.ExecContext(ctx, `INSERT INTO spec_conferencia (projeto, ref, consultado_em, erro_codigo, erro_mensagem, erro_em) VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT (projeto, ref) DO UPDATE SET consultado_em = excluded.consultado_em, erro_codigo = excluded.erro_codigo, erro_mensagem = excluded.erro_mensagem, erro_em = excluded.erro_em`,
				u.Projeto, u.Ref, agora, erro.Codigo, limitar(erro.Mensagem, 500), agora)
			return err
		}
		b, err := json.Marshal(valor)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO spec_conferencia (projeto, ref, valor, valor_em, consultado_em) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (projeto, ref) DO UPDATE SET valor = excluded.valor, valor_em = excluded.valor_em, consultado_em = excluded.consultado_em, erro_codigo = NULL, erro_mensagem = NULL, erro_em = NULL`,
			u.Projeto, u.Ref, string(b), agora, agora)
		return err
	})
}

// AbrirValidacao abre a pendência de validação pela CLI: título e opções são os do orq, e sha é a
// cabeça da MR da spec que quem chama acabou de ler na forja. Sem spec registrada, é erro de uso.
func (s *Store) AbrirValidacao(ctx context.Context, coord *Coordenador, ref refs.US, sha, pane string) (Pendencia, error) {
	if sha == "" {
		return Pendencia{}, uso("validação sem o SHA da cabeça da MR da spec", "")
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
		if err := exigirSpec(ctx, tx, alvo); err != nil {
			return err
		}
		oj, err := json.Marshal(OpcoesValidacao())
		if err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `UPDATE us SET proxima_pendencia = proxima_pendencia + 1 WHERE projeto = ? AND ref = ? RETURNING proxima_pendencia - 1`, alvo.Projeto, alvo.Ref).Scan(&numero); err != nil {
			return err
		}
		titulo := TituloValidacao(alvo.Ref)
		if _, err := tx.ExecContext(ctx, `INSERT INTO pendencia (projeto, ref, numero, tipo, texto, pane_origem, aberta, aberta_em, opcoes, sha_validacao) VALUES (?, ?, ?, 'validacao', ?, ?, 1, ?, ?, ?)`,
			alvo.Projeto, alvo.Ref, numero, titulo, nulo(pane), s.agora(), string(oj), sha); err != nil {
			return err
		}
		return s.registrar(ctx, tx, evento{coordenador: autor(coord, alvo), ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: "pend.add",
			detalhe: map[string]any{"id": refs.PendID(alvo.Ref, numero), "tipo": "validacao", "texto": titulo, "pane_origem": nulo(pane), "sha": sha}})
	})
	if err != nil {
		return Pendencia{}, err
	}
	return s.verPendencia(ctx, alvo.US, numero)
}

// exigirSpec recusa (erro de uso) a validação de uma US sem spec registrada.
func exigirSpec(ctx context.Context, q querier, alvo usAlvo) error {
	sp, err := lerLinhaSpec(ctx, q, alvo.Projeto, alvo.Ref)
	if err != nil {
		return err
	}
	if sp == nil {
		return uso(alvo.Chave()+" não tem spec registrada: a validação precisa dela", "registre com orq spec registrar "+refs.Exibir(alvo.Ref)+" --caminho <pasta> --mr <url>")
	}
	return nil
}
