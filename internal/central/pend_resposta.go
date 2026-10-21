package central

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
)

// Limites de uma pendência. O detalhe é o markdown do arquivo; a resposta é o que a pessoa
// escreve na web (a página recusa o resto antes de chegar aqui).
const (
	LimiteDetalhe  = 64 << 10
	LimiteResposta = 8 << 10
	limiteTitulo   = 200
	limiteOpcoes   = 10
	limiteDescOp   = 2000
	limiteAutor    = 100
)

// Origens da resposta, além do usuário que a web autentica.
const (
	PorChat  = "chat"
	PorLocal = "local"
)

// OpcoesAutorizacao são as opções que a Central cria numa autorização sem opções no arquivo.
func OpcoesAutorizacao() []Opcao {
	return []Opcao{{ID: 1, Titulo: "Autorizo"}, {ID: 2, Titulo: "Não autorizo"}}
}

// ValidarOpcoes confere as opções do tipo e devolve as que a pendência guarda: decisão pede ao
// menos 2 e no máximo uma recomendada; autorização não traz opções (a Central cria "Autorizo" e
// "Não autorizo"); dúvida aceita opções ou nenhuma, e com opções valem as mesmas regras.
func ValidarOpcoes(tipo string, os []Opcao) ([]Opcao, error) {
	if tipo == "autorizacao" {
		if len(os) > 0 {
			return nil, uso("autorização não tem opções no arquivo", "tire `opcoes`: a Central cria Autorizo e Não autorizo")
		}
		return OpcoesAutorizacao(), nil
	}
	if len(os) == 0 {
		if tipo == "decisao" {
			return nil, uso("decisão precisa de pelo menos 2 opções", "liste as alternativas em `opcoes`")
		}
		return []Opcao{}, nil
	}
	if len(os) < 2 {
		return nil, uso("uma opção só não é escolha: use pelo menos 2 ou nenhuma", "")
	}
	if len(os) > limiteOpcoes {
		return nil, uso(fmt.Sprintf("%d opções; o máximo é %d", len(os), limiteOpcoes), "")
	}
	vistas, recomendadas := map[int]bool{}, 0
	for i, o := range os {
		n := i + 1
		if o.ID < 1 {
			return nil, uso(fmt.Sprintf("opção %d: id precisa ser um inteiro de 1 em diante", n), "")
		}
		if vistas[o.ID] {
			return nil, uso(fmt.Sprintf("opção %d: id %d repetido", n, o.ID), "")
		}
		vistas[o.ID] = true
		if err := linhaValida(fmt.Sprintf("opção %d: titulo", o.ID), o.Titulo, limiteTitulo, true); err != nil {
			return nil, err
		}
		if len(o.Descricao) > limiteDescOp || strings.ContainsFunc(o.Descricao, controleSemQuebra) {
			return nil, uso(fmt.Sprintf("opção %d: descricao longa demais ou com caractere de controle", o.ID), "")
		}
		if o.Recomendada {
			recomendadas++
		}
	}
	if recomendadas > 1 {
		return nil, uso("mais de uma opção recomendada", "marque no máximo uma")
	}
	return os, nil
}

// linhaValida confere um texto de uma linha: não vazio (se exigido), até limite runas, sem controle.
func linhaValida(campo, v string, limite int, obrigatorio bool) error {
	if obrigatorio && strings.TrimSpace(v) == "" {
		return uso(campo+" vazio", "")
	}
	if utf8.RuneCountInString(v) > limite {
		return uso(fmt.Sprintf("%s com mais de %d caracteres", campo, limite), "")
	}
	if temControle(v) {
		return uso(campo+" com caractere de controle ou quebra de linha", "")
	}
	return nil
}

// controleSemQuebra é o controle que um texto de várias linhas não pode ter: a quebra de linha
// e a tabulação passam.
func controleSemQuebra(r rune) bool {
	return output.UnsafeRune(r) && r != '\n' && r != '\r' && r != '\t'
}

// TextoLivreValido confere o texto de várias linhas da resposta.
func TextoLivreValido(campo, v string, limite int) error {
	if len(v) > limite {
		return uso(fmt.Sprintf("%s com mais de %d bytes", campo, limite), "")
	}
	if !utf8.ValidString(v) || strings.ContainsFunc(v, controleSemQuebra) {
		return uso(campo+" com caractere de controle ou UTF-8 inválido", "")
	}
	return nil
}

// Resposta é o que a pessoa respondeu: a opção, o texto de complemento ou os dois, e por onde.
type Resposta struct {
	Opcao *int
	Texto string
	// Por é o usuário que a web autenticou, "local" (web em TCP) ou "chat" (orq pend responder).
	Por string
}

// ResponderPendencia grava a resposta da pessoa. A pendência segue aberta para encerrar a onda
// até alguém resolvê-la, e a resposta pode ser trocada até lá: cada troca é um pend.responder e
// zera o aviso ao coordenador. Uma resposta pelo chat já nasce avisada: quem a grava é o
// coordenador.
func (s *Store) ResponderPendencia(ctx context.Context, coord *Coordenador, p refs.Pend, r Resposta) (Pendencia, error) {
	r.Texto = strings.ReplaceAll(strings.TrimSpace(r.Texto), "\r\n", "\n")
	if err := linhaValida("respondida_por", r.Por, limiteAutor, true); err != nil {
		return Pendencia{}, err
	}
	if err := TextoLivreValido("resposta", r.Texto, LimiteResposta); err != nil {
		return Pendencia{}, err
	}
	if r.Opcao == nil && r.Texto == "" {
		return Pendencia{}, uso("resposta vazia", `passe --opcao N, o texto ou os dois: orq pend responder TG-312#P2 --opcao 1 "…"`)
	}
	var alvo usAlvo
	err := s.escrever(ctx, func(tx *sql.Tx) error {
		var err error
		if alvo, err = resolverUS(ctx, tx, p.US, coord); err != nil {
			return err
		}
		id := refs.PendID(alvo.Ref, p.Numero)
		var aberta bool
		var opcoesJSON, textoAntes, porAntes, emAntes *string
		var opcaoAntes *int
		err = tx.QueryRowContext(ctx, `SELECT aberta, opcoes, resposta, resposta_opcao, respondida_por, respondida_em FROM pendencia WHERE projeto = ? AND ref = ? AND numero = ?`,
			alvo.Projeto, alvo.Ref, p.Numero).Scan(&aberta, &opcoesJSON, &textoAntes, &opcaoAntes, &porAntes, &emAntes)
		if semLinha(err) {
			return naoEncontrado(fmt.Sprintf("%s não existe em %s", id, alvo.Chave()), "veja as pendências com orq pend list")
		}
		if err != nil {
			return err
		}
		if !aberta {
			return conflito("pendencia_resolvida", id+" já foi resolvida; a resposta não muda mais", "")
		}
		opcoes := decodificarOpcoes(opcoesJSON)
		var escolhida Opcao
		switch {
		case len(opcoes) == 0 && r.Opcao != nil:
			return uso(id+" não tem opções", "responda só com o texto")
		case len(opcoes) == 0 && r.Texto == "":
			return uso("resposta vazia", "esta pendência não tem opções: escreva a resposta")
		case len(opcoes) > 0 && r.Opcao == nil:
			return uso(id+" tem opções: escolha uma", "use --opcao N com um dos ids: "+idsDasOpcoes(opcoes))
		case len(opcoes) > 0:
			var ok bool
			if escolhida, ok = achar(opcoes, *r.Opcao); !ok {
				return uso(fmt.Sprintf("%s não tem a opção %d", id, *r.Opcao), "ids válidos: "+idsDasOpcoes(opcoes))
			}
		}
		agora := s.agora()
		var avisada any
		if r.Por == PorChat {
			avisada = agora
		}
		if _, err := tx.ExecContext(ctx, `UPDATE pendencia SET resposta = ?, resposta_opcao = ?, respondida_em = ?, respondida_por = ?, avisada_em = ?, aviso_falhou_em = NULL, resposta_versao = resposta_versao + 1
			WHERE projeto = ? AND ref = ? AND numero = ?`, nulo(r.Texto), r.Opcao, agora, r.Por, avisada, alvo.Projeto, alvo.Ref, p.Numero); err != nil {
			return err
		}
		d := map[string]any{"id": id, "por": r.Por, "opcao": r.Opcao, "texto": nulo(r.Texto)}
		if r.Opcao != nil {
			d["titulo_opcao"] = escolhida.Titulo
		}
		if emAntes != nil {
			d["anterior"] = map[string]any{"opcao": opcaoAntes, "texto": textoAntes, "por": porAntes, "em": emAntes}
		}
		return s.registrar(ctx, tx, evento{coordenador: autor(coord, alvo), ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: "pend.responder", detalhe: d})
	})
	if err != nil {
		return Pendencia{}, err
	}
	return s.verPendencia(ctx, alvo.US, p.Numero)
}

func idsDasOpcoes(os []Opcao) string {
	ids := make([]string, len(os))
	for i, o := range os {
		ids[i] = fmt.Sprint(o.ID)
	}
	return strings.Join(ids, ", ")
}

// RespostaRegistrada é uma linha do histórico de respostas de uma pendência (o diário).
type RespostaRegistrada struct {
	Em          string  `json:"em"`
	Por         string  `json:"por"`
	Opcao       *int    `json:"opcao"`
	TituloOpcao *string `json:"titulo_opcao"`
	Texto       *string `json:"texto"`
}

// HistoricoRespostas devolve todas as respostas da pendência, da mais antiga à mais nova.
func (s *Store) HistoricoRespostas(ctx context.Context, p refs.Pend) ([]RespostaRegistrada, error) {
	hist := []RespostaRegistrada{}
	err := s.ler(ctx, func(tx *sql.Tx) error {
		alvo, err := resolverUS(ctx, tx, p.US, nil)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT ts, detalhe FROM evento WHERE projeto = ? AND ref = ? AND tipo = 'pend.responder' AND json_extract(detalhe, '$.id') = ? ORDER BY id`,
			alvo.Projeto, alvo.Ref, refs.PendID(alvo.Ref, p.Numero))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ts, d string
			if err := rows.Scan(&ts, &d); err != nil {
				return err
			}
			var e struct {
				Por         string  `json:"por"`
				Opcao       *int    `json:"opcao"`
				TituloOpcao *string `json:"titulo_opcao"`
				Texto       *string `json:"texto"`
			}
			if err := json.Unmarshal([]byte(d), &e); err != nil {
				continue
			}
			hist = append(hist, RespostaRegistrada{Em: ts, Por: e.Por, Opcao: e.Opcao, TituloOpcao: e.TituloOpcao, Texto: e.Texto})
		}
		return rows.Err()
	})
	return hist, err
}

// LerUS devolve a US com MRs, pendências e o estado vivo.
func (s *Store) LerUS(ctx context.Context, u refs.US) (US, error) { return s.verUS(ctx, u) }
