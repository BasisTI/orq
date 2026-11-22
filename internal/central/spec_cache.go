package central

import (
	"context"
	"database/sql"

	"github.com/BasisTI/orq/internal/refs"
)

// Limites do cache da spec (US #60): acima deles o arquivo não é guardado e a Central mostra o
// aviso com o link da MR.
const (
	LimiteArquivoSpec = 1 << 20
	LimiteSpec        = 4 << 20
)

// Papéis de um arquivo guardado: da pasta da US ou do épico (README e os referenciados por link).
const (
	PapelPasta = "pasta"
	PapelEpico = "epico"
)

// ArquivoSpec é um arquivo da spec lido na forja. Caminho é relativo à raiz do repositório. Com
// GrandeDemais, Conteudo fica vazio e Tamanho diz quanto o arquivo tem.
type ArquivoSpec struct {
	Caminho      string `json:"caminho"`
	Papel        string `json:"papel"`
	Ordem        int    `json:"ordem"`
	Conteudo     string `json:"conteudo"`
	Tamanho      int64  `json:"tamanho"`
	GrandeDemais bool   `json:"grande_demais"`
}

// VersaoSpec é o conteúdo da spec num SHA, como o daemon o leu.
type VersaoSpec struct {
	SHA      string        `json:"sha"`
	LidaEm   string        `json:"lida_em"`
	Arquivos []ArquivoSpec `json:"arquivos"`
}

// LeituraSpec é o estado da leitura da ref atual: o SHA da versão atual, quando a forja foi
// consultada pela última vez e a última falha (o último conteúdo bom segue valendo).
type LeituraSpec struct {
	SHA          *string `json:"sha"`
	ConsultadoEm string  `json:"consultado_em"`
	Erro         *Erro   `json:"erro"`
}

// SpecCache é o que a web mostra da spec de uma US: o registro com o estado, a leitura e a versão
// pedida (nil se o daemon ainda não a leu ou ela já foi descartada).
type SpecCache struct {
	Spec    SpecUS       `json:"spec"`
	Leitura *LeituraSpec `json:"leitura"`
	Versao  *VersaoSpec  `json:"versao"`
}

// LerSpecCache devolve a spec da US com a versão guardada do sha; sha vazio pede a versão da ref
// atual. naoEncontrado se a US não tem spec registrada.
func (s *Store) LerSpecCache(ctx context.Context, u refs.US, sha string) (SpecCache, error) {
	var out SpecCache
	err := s.ler(ctx, func(tx *sql.Tx) error {
		alvo, err := resolverUS(ctx, tx, u, nil)
		if err != nil {
			return err
		}
		sp, err := carregarSpec(ctx, tx, alvo.Projeto, alvo.Ref)
		if err != nil {
			return err
		}
		if sp == nil {
			return naoEncontrado(alvo.Chave()+" não tem spec registrada", "registre com orq spec registrar "+refs.Exibir(alvo.Ref)+" --caminho <pasta> --mr <url>")
		}
		out.Spec = *sp
		if out.Leitura, err = lerLeituraSpec(ctx, tx, alvo.Projeto, alvo.Ref); err != nil {
			return err
		}
		if sha == "" && out.Leitura != nil && out.Leitura.SHA != nil {
			sha = *out.Leitura.SHA
		}
		if sha == "" {
			return nil
		}
		out.Versao, err = lerVersaoSpec(ctx, tx, alvo.Projeto, alvo.Ref, sha)
		return err
	})
	return out, err
}

func lerLeituraSpec(ctx context.Context, q querier, projeto string, ref int) (*LeituraSpec, error) {
	var l LeituraSpec
	var codigo, mensagem, em *string
	err := q.QueryRowContext(ctx, `SELECT sha, consultado_em, erro_codigo, erro_mensagem, erro_em FROM spec_leitura WHERE projeto = ? AND ref = ?`, projeto, ref).
		Scan(&l.SHA, &l.ConsultadoEm, &codigo, &mensagem, &em)
	if semLinha(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	l.Erro = erroDe(codigo, mensagem, em)
	return &l, nil
}

func lerVersaoSpec(ctx context.Context, q querier, projeto string, ref int, sha string) (*VersaoSpec, error) {
	v := VersaoSpec{SHA: sha}
	err := q.QueryRowContext(ctx, `SELECT lida_em FROM spec_versao WHERE projeto = ? AND ref = ? AND sha = ?`, projeto, ref, sha).Scan(&v.LidaEm)
	if semLinha(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT caminho, papel, ordem, conteudo, tamanho, grande_demais FROM spec_arquivo WHERE projeto = ? AND ref = ? AND sha = ? ORDER BY ordem, caminho`, projeto, ref, sha)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a ArquivoSpec
		if err := rows.Scan(&a.Caminho, &a.Papel, &a.Ordem, &a.Conteudo, &a.Tamanho, &a.GrandeDemais); err != nil {
			return nil, err
		}
		v.Arquivos = append(v.Arquivos, a)
	}
	return &v, rows.Err()
}

// SpecParaLer é o que o daemon decide por spec registrada: a ref atual a ler e as versões que o
// cache já tem e as pendências de validação abertas ainda pedem.
type SpecParaLer struct {
	US      refs.US
	Caminho string
	MRURL   string
	// Mesclada: a MR da spec já foi mesclada (a leitura passa a ser pela branch alvo).
	Mesclada bool
	// SHAAtual é o SHA da versão atual do cache ("" se nunca leu); Versoes, os SHAs guardados.
	SHAAtual string
	Versoes  map[string]bool
	// Validacoes são os SHAs das pendências de validação abertas desta spec.
	Validacoes   []string
	ConsultadoEm string
	Erro         *Erro
}

// SpecsParaLer lista as specs registradas com o estado do cache de cada uma.
func (s *Store) SpecsParaLer(ctx context.Context) ([]SpecParaLer, error) {
	var out []SpecParaLer
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
			if sp == nil {
				continue
			}
			item := SpecParaLer{US: u, Caminho: sp.Caminho, MRURL: sp.MRURL, Mesclada: sp.Estado == SpecMesclada, Versoes: map[string]bool{}}
			if item.Validacoes, err = validacoesAbertas(ctx, tx, u.Projeto, u.Ref); err != nil {
				return err
			}
			l, err := lerLeituraSpec(ctx, tx, u.Projeto, u.Ref)
			if err != nil {
				return err
			}
			if l != nil {
				item.ConsultadoEm, item.Erro = l.ConsultadoEm, l.Erro
				if l.SHA != nil {
					item.SHAAtual = *l.SHA
				}
			}
			vs, err := tx.QueryContext(ctx, `SELECT sha FROM spec_versao WHERE projeto = ? AND ref = ?`, u.Projeto, u.Ref)
			if err != nil {
				return err
			}
			for vs.Next() {
				var sha string
				if err := vs.Scan(&sha); err != nil {
					vs.Close()
					return err
				}
				item.Versoes[sha] = true
			}
			if err := vs.Close(); err != nil {
				return err
			}
			out = append(out, item)
		}
		return nil
	})
	return out, err
}

// validacoesAbertas são os SHAs das pendências de validação ainda sem resposta.
func validacoesAbertas(ctx context.Context, q querier, projeto string, ref int) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT sha_validacao FROM pendencia WHERE projeto = ? AND ref = ? AND tipo = 'validacao' AND aberta = 1 AND respondida_em IS NULL AND sha_validacao IS NOT NULL ORDER BY numero`, projeto, ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			return nil, err
		}
		out = append(out, sha)
	}
	return out, rows.Err()
}

// GravarVersaoSpec guarda o conteúdo da spec no sha. Com atual, ele passa a ser a versão da ref
// atual e a falha da leitura anterior some. Depois de gravar, descarta as versões que nem são a
// atual nem de uma validação aberta. É cache: não é estado da Central e não gera evento. Spec que
// deixou de existir (o registro foi trocado enquanto o daemon lia) não grava nada.
func (s *Store) GravarVersaoSpec(ctx context.Context, u refs.US, sha string, atual bool, arquivos []ArquivoSpec) error {
	return s.escrever(ctx, func(tx *sql.Tx) error {
		if sp, err := lerLinhaSpec(ctx, tx, u.Projeto, u.Ref); err != nil || sp == nil {
			return err
		}
		agora := s.agora()
		if _, err := tx.ExecContext(ctx, `DELETE FROM spec_arquivo WHERE projeto = ? AND ref = ? AND sha = ?`, u.Projeto, u.Ref, sha); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO spec_versao (projeto, ref, sha, lida_em) VALUES (?, ?, ?, ?)
			ON CONFLICT (projeto, ref, sha) DO UPDATE SET lida_em = excluded.lida_em`, u.Projeto, u.Ref, sha, agora); err != nil {
			return err
		}
		for _, a := range arquivos {
			if _, err := tx.ExecContext(ctx, `INSERT INTO spec_arquivo (projeto, ref, sha, caminho, papel, ordem, conteudo, tamanho, grande_demais) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				u.Projeto, u.Ref, sha, a.Caminho, a.Papel, a.Ordem, a.Conteudo, a.Tamanho, a.GrandeDemais); err != nil {
				return err
			}
		}
		if atual {
			if _, err := tx.ExecContext(ctx, `INSERT INTO spec_leitura (projeto, ref, sha, consultado_em) VALUES (?, ?, ?, ?)
				ON CONFLICT (projeto, ref) DO UPDATE SET sha = excluded.sha, consultado_em = excluded.consultado_em, erro_codigo = NULL, erro_mensagem = NULL, erro_em = NULL`,
				u.Projeto, u.Ref, sha, agora); err != nil {
				return err
			}
		}
		return podarSpec(ctx, tx, u.Projeto, u.Ref)
	})
}

// MarcarLeituraSpec registra que a forja foi consultada e limpa a falha anterior. Com sha, a versão
// dele, que já precisa estar guardada, passa a ser a da ref atual; vazio deixa a atual como está.
func (s *Store) MarcarLeituraSpec(ctx context.Context, u refs.US, sha string) error {
	return s.escrever(ctx, func(tx *sql.Tx) error {
		if sp, err := lerLinhaSpec(ctx, tx, u.Projeto, u.Ref); err != nil || sp == nil {
			return err
		}
		if sha != "" {
			v, err := lerVersaoSpec(ctx, tx, u.Projeto, u.Ref, sha)
			if err != nil {
				return err
			}
			if v == nil {
				return nil
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO spec_leitura (projeto, ref, sha, consultado_em) VALUES (?, ?, ?, ?)
			ON CONFLICT (projeto, ref) DO UPDATE SET sha = COALESCE(excluded.sha, spec_leitura.sha), consultado_em = excluded.consultado_em, erro_codigo = NULL, erro_mensagem = NULL, erro_em = NULL`,
			u.Projeto, u.Ref, nulo(sha), s.agora()); err != nil {
			return err
		}
		return podarSpec(ctx, tx, u.Projeto, u.Ref)
	})
}

// GravarErroLeituraSpec guarda a falha da leitura da forja. O último conteúdo bom fica.
func (s *Store) GravarErroLeituraSpec(ctx context.Context, u refs.US, erro Erro) error {
	return s.escrever(ctx, func(tx *sql.Tx) error {
		if sp, err := lerLinhaSpec(ctx, tx, u.Projeto, u.Ref); err != nil || sp == nil {
			return err
		}
		agora := s.agora()
		_, err := tx.ExecContext(ctx, `INSERT INTO spec_leitura (projeto, ref, consultado_em, erro_codigo, erro_mensagem, erro_em) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (projeto, ref) DO UPDATE SET consultado_em = excluded.consultado_em, erro_codigo = excluded.erro_codigo, erro_mensagem = excluded.erro_mensagem, erro_em = excluded.erro_em`,
			u.Projeto, u.Ref, agora, erro.Codigo, limitar(erro.Mensagem, 500), agora)
		return err
	})
}

// podarSpec descarta as versões que nem são a da ref atual nem de uma validação aberta.
func podarSpec(ctx context.Context, tx *sql.Tx, projeto string, ref int) error {
	manter := map[string]bool{}
	var atual *string
	if err := tx.QueryRowContext(ctx, `SELECT sha FROM spec_leitura WHERE projeto = ? AND ref = ?`, projeto, ref).Scan(&atual); err != nil && !semLinha(err) {
		return err
	}
	if atual != nil {
		manter[*atual] = true
	}
	vals, err := validacoesAbertas(ctx, tx, projeto, ref)
	if err != nil {
		return err
	}
	for _, v := range vals {
		manter[v] = true
	}
	rows, err := tx.QueryContext(ctx, `SELECT sha FROM spec_versao WHERE projeto = ? AND ref = ?`, projeto, ref)
	if err != nil {
		return err
	}
	var apagar []string
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			rows.Close()
			return err
		}
		if !manter[sha] {
			apagar = append(apagar, sha)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, sha := range apagar {
		if _, err := tx.ExecContext(ctx, `DELETE FROM spec_arquivo WHERE projeto = ? AND ref = ? AND sha = ?`, projeto, ref, sha); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM spec_versao WHERE projeto = ? AND ref = ? AND sha = ?`, projeto, ref, sha); err != nil {
			return err
		}
	}
	return nil
}

// apagarCacheSpec tira todo o cache da spec (o registro foi substituído).
func apagarCacheSpec(ctx context.Context, tx *sql.Tx, projeto string, ref int) error {
	for _, t := range []string{"spec_arquivo", "spec_versao", "spec_leitura"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+t+` WHERE projeto = ? AND ref = ?`, projeto, ref); err != nil {
			return err
		}
	}
	return nil
}
