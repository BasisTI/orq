package central

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/BasisTI/orq/internal/refs"
)

// NovaPendencia é uma pendência lida de um arquivo (veja o pacote pendencias).
type NovaPendencia struct {
	US      refs.US // projeto e ref saem do caminho do arquivo
	Tipo    string
	Titulo  string
	Autor   string
	Detalhe string // o markdown depois do frontmatter
	Opcoes  []Opcao
	Slug    string
	Arquivo string
	Hash    string // sha256 do arquivo, para notar que mudou depois do registro
}

// ResultadoRegistro diz o que RegistrarPendencia fez.
type ResultadoRegistro struct {
	Pendencia Pendencia
	Criada    bool // falso: o slug já estava registrado
	// Alterada: o arquivo mudou depois do registro e este é o primeiro olhar sobre a mudança.
	Alterada bool
}

// RegistrarPendencia numera e grava a pendência de um arquivo. É idempotente por US + slug: o
// arquivo registrado de novo não cria nada; se o conteúdo mudou, a pendência não muda, e o
// diário ganha um pend.arquivo_alterado (uma vez por conteúdo novo). A US precisa estar numa
// onda aberta para uma pendência nova.
func (s *Store) RegistrarPendencia(ctx context.Context, n NovaPendencia) (ResultadoRegistro, error) {
	if !contem(TiposPendencia, n.Tipo) {
		return ResultadoRegistro{}, uso(fmt.Sprintf("tipo %q inválido", n.Tipo), "use "+strings.Join(TiposPendencia, ", "))
	}
	if n.Slug == "" {
		return ResultadoRegistro{}, uso("pendência de arquivo sem slug", "")
	}
	var res ResultadoRegistro
	var numero int
	var alvo usAlvo
	err := s.escrever(ctx, func(tx *sql.Tx) error {
		var hash, alterado *string
		err := tx.QueryRowContext(ctx, `SELECT numero, arquivo_hash, arquivo_alterado FROM pendencia WHERE projeto = ? AND ref = ? AND slug = ?`, n.US.Projeto, n.US.Ref, n.Slug).
			Scan(&numero, &hash, &alterado)
		if err == nil {
			if alvo, err = resolverUS(ctx, tx, n.US, nil); err != nil {
				return err
			}
			res.Alterada, err = s.conferirArquivo(ctx, tx, alvo, numero, n.Slug, n.Arquivo, hash, alterado, n.Hash)
			return err
		}
		if !semLinha(err) {
			return err
		}
		res.Criada = true
		if alvo, err = resolverUS(ctx, tx, n.US, nil); err != nil {
			return err
		}
		var aberta bool
		if err := tx.QueryRowContext(ctx, `SELECT encerrada_em IS NULL FROM onda WHERE id = ?`, alvo.ondaID).Scan(&aberta); err != nil {
			return err
		}
		if !aberta {
			return conflito("onda_encerrada", fmt.Sprintf("%s está numa onda encerrada", alvo.Chave()), "ponha a US numa onda aberta com orq onda criar")
		}
		opcoes, err := ValidarOpcoes(n.Tipo, n.Opcoes)
		if err != nil {
			return err
		}
		if err := linhaValida("titulo", n.Titulo, limiteTitulo, true); err != nil {
			return err
		}
		if err := linhaValida("autor", n.Autor, limiteAutor, true); err != nil {
			return err
		}
		if len(n.Detalhe) > LimiteDetalhe {
			return uso(fmt.Sprintf("detalhe com %d bytes; o máximo é %d", len(n.Detalhe), LimiteDetalhe), "")
		}
		oj, err := json.Marshal(opcoes)
		if err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `UPDATE us SET proxima_pendencia = proxima_pendencia + 1 WHERE projeto = ? AND ref = ? RETURNING proxima_pendencia - 1`, alvo.Projeto, alvo.Ref).Scan(&numero); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO pendencia (projeto, ref, numero, tipo, texto, aberta, aberta_em, detalhe, opcoes, slug, arquivo, arquivo_hash, autor)
			VALUES (?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?)`,
			alvo.Projeto, alvo.Ref, numero, n.Tipo, n.Titulo, s.agora(), n.Detalhe, string(oj), n.Slug, n.Arquivo, n.Hash, n.Autor); err != nil {
			return err
		}
		return s.registrar(ctx, tx, evento{coordenador: alvo.coordenador, ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: "pend.add",
			detalhe: map[string]any{"id": refs.PendID(alvo.Ref, numero), "tipo": n.Tipo, "texto": n.Titulo, "origem": "arquivo", "slug": n.Slug, "arquivo": n.Arquivo, "autor": n.Autor, "opcoes": len(opcoes)}})
	})
	if err != nil {
		return ResultadoRegistro{}, err
	}
	res.Pendencia, err = s.verPendencia(ctx, alvo.US, numero)
	return res, err
}

// conferirArquivo compara o conteúdo atual do arquivo com o do registro e grava o evento da
// mudança, uma vez por conteúdo. Voltar ao conteúdo registrado apaga a marca.
func (s *Store) conferirArquivo(ctx context.Context, tx *sql.Tx, alvo usAlvo, numero int, slug, arquivo string, registrado, visto *string, atual string) (bool, error) {
	if atual == "" || registrado == nil {
		return false, nil
	}
	id := refs.PendID(alvo.Ref, numero)
	if atual == *registrado {
		if visto == nil {
			return false, nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE pendencia SET arquivo_alterado = NULL WHERE projeto = ? AND ref = ? AND numero = ?`, alvo.Projeto, alvo.Ref, numero); err != nil {
			return false, err
		}
		return false, s.registrar(ctx, tx, evento{coordenador: alvo.coordenador, ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: "pend.arquivo_restaurado",
			detalhe: map[string]any{"id": id, "slug": slug, "arquivo": arquivo}})
	}
	if visto != nil && *visto == atual {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pendencia SET arquivo_alterado = ? WHERE projeto = ? AND ref = ? AND numero = ?`, atual, alvo.Projeto, alvo.Ref, numero); err != nil {
		return false, err
	}
	return true, s.registrar(ctx, tx, evento{coordenador: alvo.coordenador, ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: "pend.arquivo_alterado",
		detalhe: map[string]any{"id": id, "slug": slug, "arquivo": arquivo, "hash_registrado": *registrado, "hash_atual": atual}})
}

// Importada é o que a Central guarda de uma pendência que veio de arquivo.
type Importada struct {
	Numero   int
	Hash     string
	Alterado string // último conteúdo diferente já registrado no diário; vazio se nenhum
}

// ChaveSlug identifica um arquivo importado: a US e o slug.
type ChaveSlug struct {
	Projeto string
	Ref     int
	Slug    string
}

// PendenciasImportadas lê, de uma vez, as pendências com slug. A varredura do daemon usa para
// não consultar o banco arquivo por arquivo a cada 5 s.
func (s *Store) PendenciasImportadas(ctx context.Context) (map[ChaveSlug]Importada, error) {
	m := map[ChaveSlug]Importada{}
	err := s.ler(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT projeto, ref, slug, numero, COALESCE(arquivo_hash, ''), COALESCE(arquivo_alterado, '') FROM pendencia WHERE slug IS NOT NULL`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k ChaveSlug
			var i Importada
			if err := rows.Scan(&k.Projeto, &k.Ref, &k.Slug, &i.Numero, &i.Hash, &i.Alterado); err != nil {
				return err
			}
			m[k] = i
		}
		return rows.Err()
	})
	return m, err
}

// Tipos de aviso ao coordenador.
const (
	AvisoAbertura = "abertura"
	AvisoResposta = "resposta"
)

// Aviso é uma notificação que o daemon ainda deve ao coordenador da US.
type Aviso struct {
	Tipo      string
	Pendencia Pendencia
	// Coordenador é o dono da onda; Pane é o pane dele no Herdr (nulo se não está registrado).
	Coordenador string
	Pane        *string
	// Opcao é a opção escolhida na resposta (nula se a resposta é só texto).
	Opcao *Opcao
}

// AvisosPendentes lista o que falta avisar, do mais antigo ao mais novo: pendência importada
// aberta e ainda sem resposta que o coordenador não recebeu, e resposta (nova ou trocada) que
// ele não recebeu.
func (s *Store) AvisosPendentes(ctx context.Context) ([]Aviso, error) {
	var avisos []Aviso
	err := s.ler(ctx, func(tx *sql.Tx) error {
		ps, err := carregarPendenciasSQL(ctx, tx, false, `WHERE p.aberta = 1 AND (
			(p.respondida_em IS NOT NULL AND p.avisada_em IS NULL) OR
			(p.respondida_em IS NULL AND p.slug IS NOT NULL AND p.abertura_avisada_em IS NULL))
			ORDER BY COALESCE(p.respondida_em, p.aberta_em), p.id`)
		if err != nil {
			return err
		}
		for _, p := range ps {
			a := Aviso{Tipo: AvisoAbertura, Pendencia: p, Coordenador: p.Coordenador}
			if p.RespondidaEm != nil {
				a.Tipo = AvisoResposta
				if p.RespostaOpcao != nil {
					if o, ok := achar(p.Opcoes, *p.RespostaOpcao); ok {
						a.Opcao = &o
					}
				}
			}
			if err := tx.QueryRowContext(ctx, `SELECT pane_id FROM coordenador WHERE nome = ?`, p.Coordenador).Scan(&a.Pane); err != nil && !semLinha(err) {
				return err
			}
			avisos = append(avisos, a)
		}
		return nil
	})
	return avisos, err
}

// MarcarAvisada grava que o coordenador recebeu o aviso. Resposta trocada depois da leitura do
// aviso não conta: o aviso novo ainda é devido.
func (s *Store) MarcarAvisada(ctx context.Context, a Aviso) error {
	return s.escrever(ctx, func(tx *sql.Tx) error {
		var alvo usAlvo
		var err error
		if alvo, err = resolverUS(ctx, tx, a.Pendencia.RefUS(), nil); err != nil {
			return err
		}
		var res sql.Result
		switch a.Tipo {
		case AvisoResposta:
			res, err = tx.ExecContext(ctx, `UPDATE pendencia SET avisada_em = ?, aviso_falhou_em = NULL WHERE projeto = ? AND ref = ? AND numero = ? AND respondida_em = ? AND avisada_em IS NULL`,
				s.agora(), alvo.Projeto, alvo.Ref, a.Pendencia.Numero, *a.Pendencia.RespondidaEm)
		default:
			res, err = tx.ExecContext(ctx, `UPDATE pendencia SET abertura_avisada_em = ?, aviso_falhou_em = NULL WHERE projeto = ? AND ref = ? AND numero = ? AND abertura_avisada_em IS NULL`,
				s.agora(), alvo.Projeto, alvo.Ref, a.Pendencia.Numero)
		}
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		return s.registrar(ctx, tx, evento{coordenador: alvo.coordenador, ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: "pend.avisada",
			detalhe: map[string]any{"id": a.Pendencia.ID, "aviso": a.Tipo, "coordenador": a.Coordenador}})
	})
}

// RegistrarFalhaAviso grava pend.aviso_falhou (o coordenador não existe) uma vez por aviso:
// o ciclo seguinte não repete o evento nem a marca. O aviso continua devido.
func (s *Store) RegistrarFalhaAviso(ctx context.Context, a Aviso, motivo string) error {
	return s.escrever(ctx, func(tx *sql.Tx) error {
		alvo, err := resolverUS(ctx, tx, a.Pendencia.RefUS(), nil)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE pendencia SET aviso_falhou_em = ? WHERE projeto = ? AND ref = ? AND numero = ? AND aviso_falhou_em IS NULL`,
			s.agora(), alvo.Projeto, alvo.Ref, a.Pendencia.Numero)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		return s.registrar(ctx, tx, evento{coordenador: alvo.coordenador, ondaID: alvo.ondaID, projeto: alvo.Projeto, ref: alvo.Ref, tipo: "pend.aviso_falhou",
			detalhe: map[string]any{"id": a.Pendencia.ID, "aviso": a.Tipo, "coordenador": a.Coordenador, "motivo": motivo}})
	})
}

// ConferirArquivoPendencia confere o conteúdo atual do arquivo de uma pendência já registrada
// (hash) e grava a mudança no diário, uma vez por conteúdo. Não toca na pendência em si.
func (s *Store) ConferirArquivoPendencia(ctx context.Context, u refs.US, slug, arquivo, hash string) (alterada bool, err error) {
	err = s.escrever(ctx, func(tx *sql.Tx) error {
		var numero int
		var registrado, visto *string
		err := tx.QueryRowContext(ctx, `SELECT numero, arquivo_hash, arquivo_alterado FROM pendencia WHERE projeto = ? AND ref = ? AND slug = ?`, u.Projeto, u.Ref, slug).
			Scan(&numero, &registrado, &visto)
		if semLinha(err) {
			return naoEncontrado(fmt.Sprintf("%s não tem pendência com o slug %q", u.Chave(), slug), "")
		}
		if err != nil {
			return err
		}
		alvo, err := resolverUS(ctx, tx, u, nil)
		if err != nil {
			return err
		}
		alterada, err = s.conferirArquivo(ctx, tx, alvo, numero, slug, arquivo, registrado, visto, hash)
		return err
	})
	return alterada, err
}
