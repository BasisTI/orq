// Package central guarda o estado da Central de US num SQLite: coordenadores, ondas, US, MRs,
// pendências e o diário de eventos. Vários coordenadores escrevem no mesmo arquivo a partir de
// processos diferentes, então toda escrita é uma transação curta que já começa com a trava de
// escrita (BEGIN IMMEDIATE) e espera a vez pelo busy_timeout.
package central

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BasisTI/orq/internal/output"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Arquivo é o nome do banco dentro do diretório de estado; ArquivoTrava serializa a abertura.
const (
	Arquivo      = "central.db"
	ArquivoTrava = "central.db.init.lock"
)

// busyTimeout é quanto uma transação espera a trava de outro processo antes de desistir.
const busyTimeout = 15 * time.Second

// Store é o acesso ao banco da Central.
type Store struct {
	db *sql.DB
	// Now é o relógio; os testes o trocam.
	Now func() time.Time
}

// DirPadrao resolve o diretório de estado: $ORQ_STATE_DIR, depois $XDG_STATE_HOME/orq e,
// por fim, ~/.local/state/orq.
func DirPadrao(env func(string) string) (string, error) {
	if d := env("ORQ_STATE_DIR"); d != "" {
		return d, nil
	}
	if x := env("XDG_STATE_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "orq"), nil
	}
	home := env("HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", &output.Error{Code: "estado_indisponivel", Cause: "sem $HOME para achar o diretório de estado", Recovery: "defina ORQ_STATE_DIR", Exit: output.ExitUnexpected}
		}
		home = h
	}
	return filepath.Join(home, ".local", "state", "orq"), nil
}

// Open abre (e cria, se preciso) o banco em dir e aplica as migrations pendentes.
func Open(ctx context.Context, dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, &output.Error{Code: "estado_indisponivel", Cause: err.Error(), Recovery: "confira ORQ_STATE_DIR e as permissões do diretório", Exit: output.ExitUnexpected}
	}
	path := filepath.Join(dir, Arquivo)
	// busy_timeout vem primeiro: o journal_mode(WAL) já precisa da trava num banco novo.
	dsn := "file:" + (&url.URL{Path: path}).EscapedPath() +
		"?_pragma=busy_timeout(" + strconv.FormatInt(busyTimeout.Milliseconds(), 10) + ")" +
		"&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	destravar, err := travarInicio(ctx, filepath.Join(dir, ArquivoTrava))
	if err != nil {
		return nil, err
	}
	defer destravar()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, Now: time.Now}
	// O Ping abre a primeira conexão (e aplica o WAL) ainda dentro da trava.
	inicio := time.Now()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, ocupado(err, inicio)
	}
	if err := s.migrar(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close fecha o banco.
func (s *Store) Close() error { return s.db.Close() }

// migrar aplica, numa transação só, os arquivos NNNN_*.sql com número acima do user_version.
func (s *Store) migrar(ctx context.Context) error {
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return err
	}
	type mig struct {
		versao int
		nome   string
	}
	var ms []mig
	for _, e := range entries {
		n, err := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s sem número", e.Name())
		}
		ms = append(ms, mig{n, e.Name()})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].versao < ms[j].versao })
	// Banco em dia não toma a trava de escrita: a maioria das aberturas é só leitura.
	var atual int
	inicio := time.Now()
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&atual); err != nil {
		return ocupado(err, inicio)
	}
	if len(ms) > 0 && atual == ms[len(ms)-1].versao {
		return nil
	}
	return s.escrever(ctx, func(tx *sql.Tx) error {
		var atual int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&atual); err != nil {
			return err
		}
		if len(ms) > 0 && atual > ms[len(ms)-1].versao {
			return &output.Error{Code: "versao_do_banco", Cause: fmt.Sprintf("o banco está na versão %d, mais nova que este orq (%d)", atual, ms[len(ms)-1].versao), Recovery: "atualize o orq com go install github.com/BasisTI/orq/cmd/orq@latest", Exit: output.ExitConflict}
		}
		for _, m := range ms {
			if m.versao <= atual {
				continue
			}
			b, err := migrations.ReadFile("migrations/" + m.nome)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, string(b)); err != nil {
				return fmt.Errorf("migration %s: %w", m.nome, err)
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.versao)); err != nil {
				return err
			}
		}
		return nil
	})
}

// escrever roda fn numa transação de escrita; o driver abre com BEGIN IMMEDIATE (_txlock).
func (s *Store) escrever(ctx context.Context, fn func(tx *sql.Tx) error) error {
	inicio := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ocupado(err, inicio)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return ocupado(err, inicio)
	}
	return ocupado(tx.Commit(), inicio)
}

// ler roda fn numa transação, para que leituras de várias tabelas vejam o mesmo instante.
func (s *Store) ler(ctx context.Context, fn func(tx *sql.Tx) error) error {
	inicio := time.Now()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ocupado(err, inicio)
	}
	defer func() { _ = tx.Rollback() }()
	return ocupado(fn(tx), inicio)
}

// ocupado traduz SQLITE_BUSY num erro tipado. O recovery separa o tempo de espera esgotado da
// recusa imediata, que o busy_timeout não cobre e indica disputa que o orq devia ter evitado.
func ocupado(err error, inicio time.Time) error {
	if err == nil || !strings.Contains(err.Error(), "SQLITE_BUSY") {
		return err
	}
	espera := time.Since(inicio)
	e := &output.Error{Code: "banco_ocupado", Cause: fmt.Sprintf("%s (depois de %d ms)", err, espera.Milliseconds()), Exit: output.ExitConflict}
	if espera >= busyTimeout-time.Second {
		e.Recovery = fmt.Sprintf("outro processo segurou o banco por mais de %d s; veja se algum orq travou e tente de novo", int(busyTimeout.Seconds()))
	} else {
		e.Recovery = "o SQLite recusou a trava na hora, sem esperar o busy_timeout; tente de novo e, se repetir, reporte o caso no BasisTI/orq com esta causa"
	}
	return e
}

func (s *Store) agora() string { return s.Now().UTC().Format(time.RFC3339) }

// evento grava uma linha no diário, na mesma transação da escrita que ela descreve.
type evento struct {
	coordenador string
	ondaID      int64
	projeto     string
	ref         int
	tipo        string
	detalhe     map[string]any
}

func (s *Store) registrar(ctx context.Context, tx *sql.Tx, e evento) error {
	d, err := json.Marshal(e.detalhe)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO evento (ts, coordenador, onda_id, projeto, ref, tipo, detalhe) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.agora(), nulo(e.coordenador), nuloInt(e.ondaID), nulo(e.projeto), nuloInt(int64(e.ref)), e.tipo, string(d))
	return err
}

func nulo(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nuloInt(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func naoEncontrado(cause, recovery string) error {
	return &output.Error{Code: "nao_encontrado", Cause: cause, Recovery: recovery, Exit: output.ExitNotFound}
}

func uso(cause, recovery string) error {
	return &output.Error{Code: "uso", Cause: cause, Recovery: recovery, Exit: output.ExitUsage}
}

func conflito(code, cause, recovery string) error {
	return &output.Error{Code: code, Cause: cause, Recovery: recovery, Exit: output.ExitConflict}
}

// semLinha diz se err é o sql.ErrNoRows.
func semLinha(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func temControle(s string) bool { return strings.ContainsFunc(s, output.UnsafeRune) }
