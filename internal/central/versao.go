package central

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BasisTI/orq/internal/output"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// A versão de um binário do orq, para decidir se ele lê o banco, é a última migration que ele
// embute (Schema). Cada migration declara, nos comentários do próprio arquivo, a menor versão
// dessas que ainda usa o banco migrado por ela:
//
//	-- orq:leitor-minimo N    abre o banco e lê (padrão: o número da própria migration)
//	-- orq:escritor-minimo N  também escreve (padrão: o número da própria migration)
//
// O banco guarda o maior valor de cada um entre as migrations aplicadas, na tabela banco_meta
// (criada pela 0004). Um orq cuja última migration é menor que o escritor mínimo, mas não menor
// que o leitor mínimo, abre o banco só para leitura; menor que o leitor mínimo, recusa
// (versao_do_banco). Sem declaração, vale o comportamento de antes: só abre banco da mesma
// versão. Banco sem banco_meta (anterior à 0004) também é recusado quando é mais novo.
const (
	diretivaLeitor   = "-- orq:leitor-minimo "
	diretivaEscritor = "-- orq:escritor-minimo "
	chaveLeitor      = "leitor_minimo"
	chaveEscritor    = "escritor_minimo"
)

// Opcoes ajusta a abertura do banco.
type Opcoes struct {
	// Migracoes substitui as migrations embutidas (o diretório "migrations" dentro do fs); os
	// testes usam para simular um orq mais novo ou mais velho.
	Migracoes fs.FS
	// AntesDeMigrar roda, sob a trava de início e antes de a primeira migration pendente ser
	// aplicada, com o que o banco passará a exigir. Não roda com o banco em dia.
	AntesDeMigrar func(Migracao)
}

func (o Opcoes) fonte() fs.FS {
	if o.Migracoes != nil {
		return o.Migracoes
	}
	return migrationsFS
}

// Migracao descreve a migração que está para acontecer.
type Migracao struct {
	De, Para       int // user_version atual e o que ele passa a ser
	LeitorMinimo   int // menor Schema que ainda abre o banco depois da migração
	EscritorMinimo int // menor Schema que ainda escreve nele
}

// EstadoVersao é a versão do banco e o que ele exige de quem o abre.
type EstadoVersao struct {
	Banco          int `json:"banco"`
	LeitorMinimo   int `json:"leitor_minimo"`
	EscritorMinimo int `json:"escritor_minimo"`
	// Binario é o Schema do orq que leu; zero quando o estado vem só do arquivo.
	Binario int `json:"binario,omitempty"`
}

// PodeLer diz se um orq com esse Schema abre o banco.
func (e EstadoVersao) PodeLer(schema int) bool { return schema >= e.LeitorMinimo }

// PodeEscrever diz se um orq com esse Schema escreve no banco.
func (e EstadoVersao) PodeEscrever(schema int) bool { return schema >= e.EscritorMinimo }

// Schema é a última migration embutida neste binário: a versão dele, para o leitor mínimo.
func Schema() int {
	ms, err := listarMigrations(migrationsFS)
	if err != nil || len(ms) == 0 {
		return 0
	}
	return ms[len(ms)-1].versao
}

type migracao struct {
	versao           int
	nome, sql        string
	leitor, escritor int
}

func listarMigrations(fsys fs.FS) ([]migracao, error) {
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return nil, err
	}
	var ms []migracao
	for _, e := range entries {
		n, err := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0])
		if err != nil {
			return nil, fmt.Errorf("migration %s sem número", e.Name())
		}
		b, err := fs.ReadFile(fsys, "migrations/"+e.Name())
		if err != nil {
			return nil, err
		}
		m := migracao{versao: n, nome: e.Name(), sql: string(b), leitor: n, escritor: n}
		for _, l := range strings.Split(m.sql, "\n") {
			l = strings.TrimSpace(l)
			for _, d := range []struct {
				prefixo string
				dest    *int
			}{{diretivaLeitor, &m.leitor}, {diretivaEscritor, &m.escritor}} {
				if v, ok := strings.CutPrefix(l, d.prefixo); ok {
					x, err := strconv.Atoi(strings.TrimSpace(v))
					if err != nil || x < 1 || x > n {
						return nil, fmt.Errorf("migration %s: %q deve ser um número de 1 a %d", e.Name(), strings.TrimSpace(l), n)
					}
					*d.dest = x
				}
			}
		}
		if m.escritor < m.leitor {
			return nil, fmt.Errorf("migration %s: escritor-minimo (%d) menor que leitor-minimo (%d)", e.Name(), m.escritor, m.leitor)
		}
		ms = append(ms, m)
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].versao < ms[j].versao })
	return ms, nil
}

type consultador interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// lerMeta devolve o mínimo gravado no banco; ok é falso num banco sem banco_meta.
func lerMeta(ctx context.Context, q consultador) (leitor, escritor int, ok bool, err error) {
	var n int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'banco_meta'").Scan(&n); err != nil {
		return 0, 0, false, err
	}
	if n == 0 {
		return 0, 0, false, nil
	}
	for chave, dest := range map[string]*int{chaveLeitor: &leitor, chaveEscritor: &escritor} {
		var v string
		switch err := q.QueryRowContext(ctx, "SELECT valor FROM banco_meta WHERE chave = ?", chave).Scan(&v); {
		case semLinha(err):
			return 0, 0, false, nil
		case err != nil:
			return 0, 0, false, err
		}
		x, err := strconv.Atoi(v)
		if err != nil {
			return 0, 0, false, nil
		}
		*dest = x
	}
	return leitor, escritor, true, nil
}

// declarado junta o mínimo gravado no banco com o das migrations que vão ser aplicadas.
func (s *Store) declarado(ctx context.Context, ms []migracao, atual int) (leitor, escritor int, err error) {
	leitor, escritor, _, err = lerMeta(ctx, s.db)
	if err != nil {
		return 0, 0, err
	}
	for _, m := range ms {
		if m.versao > atual {
			leitor, escritor = max(leitor, m.leitor), max(escritor, m.escritor)
		}
	}
	return leitor, escritor, nil
}

// gravarVersoes grava em banco_meta o mínimo exigido depois de aplicar as migrations acima de de.
func gravarVersoes(ctx context.Context, tx *sql.Tx, ms []migracao, de int) error {
	leitor, escritor, _, err := lerMeta(ctx, tx)
	if err != nil {
		return err
	}
	var temTabela int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'banco_meta'").Scan(&temTabela); err != nil {
		return err
	}
	if temTabela == 0 {
		return nil
	}
	for _, m := range ms {
		if m.versao > de {
			leitor, escritor = max(leitor, m.leitor), max(escritor, m.escritor)
		}
	}
	for chave, v := range map[string]int{chaveLeitor: leitor, chaveEscritor: escritor} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO banco_meta (chave, valor) VALUES (?, ?)
			ON CONFLICT (chave) DO UPDATE SET valor = excluded.valor`, chave, strconv.Itoa(v)); err != nil {
			return err
		}
	}
	return nil
}

// abrirMaisNovo decide o que fazer com um banco de versão atual, maior que a última migration
// (ultima) deste binário: abrir, só para leitura ou recusar.
func (s *Store) abrirMaisNovo(ctx context.Context, atual, ultima int) error {
	leitor, escritor, ok, err := lerMeta(ctx, s.db)
	if err != nil {
		return err
	}
	if !ok || ultima < leitor {
		return erroVersaoDoBanco(atual, ultima, leitor)
	}
	if ultima < escritor {
		s.somenteLeitura = &EstadoVersao{Banco: atual, LeitorMinimo: leitor, EscritorMinimo: escritor, Binario: ultima}
	}
	return nil
}

const recoveryUpgrade = "rode `orq upgrade`: ele atualiza o orq do go install e o do plugin do Herdr, reinicia o daemon e confere que todos ficaram na mesma versão"

func erroVersaoDoBanco(atual, ultima, leitor int) error {
	cause := fmt.Sprintf("o banco está na versão %d, mais nova que este orq (%d)", atual, ultima)
	if leitor > 0 {
		cause = fmt.Sprintf("o banco está na versão %d e só abre em orq que tenha a migration %d ou mais nova; este chega à %d", atual, leitor, ultima)
	}
	return &output.Error{Code: "versao_do_banco", Cause: cause, Recovery: recoveryUpgrade, Exit: output.ExitConflict}
}

func (s *Store) erroSomenteLeitura() error {
	e := s.somenteLeitura
	return &output.Error{Code: "banco_somente_leitura", Cause: fmt.Sprintf("o banco está na versão %d e só aceita escrita de orq que tenha a migration %d ou mais nova; este chega à %d e só lê", e.Banco, e.EscritorMinimo, e.Binario), Recovery: recoveryUpgrade, Exit: output.ExitConflict}
}

// Versoes devolve a versão do banco aberto e o que ele exige.
func (s *Store) Versoes(ctx context.Context) (EstadoVersao, error) {
	if s.somenteLeitura != nil {
		return *s.somenteLeitura, nil
	}
	return lerEstado(ctx, s.db, Schema())
}

func lerEstado(ctx context.Context, q consultador, binario int) (EstadoVersao, error) {
	e := EstadoVersao{Binario: binario}
	if err := q.QueryRowContext(ctx, "PRAGMA user_version").Scan(&e.Banco); err != nil {
		return e, err
	}
	leitor, escritor, ok, err := lerMeta(ctx, q)
	if err != nil {
		return e, err
	}
	if !ok {
		// Banco anterior ao leitor mínimo: só abre quem tem a mesma versão.
		leitor, escritor = e.Banco, e.Banco
	}
	e.LeitorMinimo, e.EscritorMinimo = leitor, escritor
	return e, nil
}

// LerEstado lê a versão do banco em dir sem migrar nem tomar a trava de escrita; existe é falso
// quando ainda não há banco. O orq upgrade usa para dizer quem não abre o banco.
func LerEstado(ctx context.Context, dir string) (e EstadoVersao, existe bool, err error) {
	path := filepath.Join(dir, Arquivo)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return e, false, nil
		}
		return e, false, err
	}
	db, err := sql.Open("sqlite", "file:"+(&url.URL{Path: path}).EscapedPath()+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return e, true, err
	}
	defer db.Close()
	e, err = lerEstado(ctx, db, 0)
	return e, true, err
}
