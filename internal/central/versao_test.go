package central

import (
	"context"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/BasisTI/orq/internal/output"
)

// migrationsMais devolve as migrations embutidas com uma 0005 a mais, o orq "do futuro".
func migrationsMais(t *testing.T, extra string) fs.FS {
	t.Helper()
	m := fstest.MapFS{}
	es, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		b, err := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		if err != nil {
			t.Fatal(err)
		}
		m["migrations/"+e.Name()] = &fstest.MapFile{Data: b}
	}
	m["migrations/0005_futura.sql"] = &fstest.MapFile{Data: []byte(extra + "\nCREATE TABLE futura (id INTEGER PRIMARY KEY);\n")}
	return m
}

// novoDoFuturo migra um banco novo com a 0005 e devolve o diretório.
func novoDoFuturo(t *testing.T, diretivas string) string {
	t.Helper()
	dir := t.TempDir()
	s, err := OpenCom(context.Background(), dir, Opcoes{Migracoes: migrationsMais(t, diretivas)})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestMigrationsEmbutidasDeclaramValoresValidos(t *testing.T) {
	ms, err := listarMigrations(migrationsFS)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != Schema() || Schema() < 4 {
		t.Fatalf("%d migrations, Schema() = %d", len(ms), Schema())
	}
	// A 0004 é a primeira com a tabela; o que ela declara vale para os binários seguintes.
	if m := ms[3]; m.leitor != 4 || m.escritor != 4 {
		t.Fatalf("0004 declara %d/%d", m.leitor, m.escritor)
	}
}

func TestMigrationGravaOMinimo(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, err := s.Versoes(ctx)
	if err != nil || e.Banco != Schema() || e.LeitorMinimo != 4 || e.EscritorMinimo != 4 {
		t.Fatalf("estado = %+v, %v", e, err)
	}
	// Sem a diretiva, a migration vale o próprio número: só abre quem a tem.
	d := novoDoFuturo(t, "-- sem diretiva")
	got, existe, err := LerEstado(ctx, d)
	if err != nil || !existe || got.Banco != 5 || got.LeitorMinimo != 5 || got.EscritorMinimo != 5 {
		t.Fatalf("estado da 0005 sem diretiva = %+v, %v, %v", got, existe, err)
	}
}

// O binário com Schema maior ou igual ao leitor mínimo abre o banco mais novo, sem migrar.
func TestAbreBancoMaisNovoQuandoOLeitorMinimoPermite(t *testing.T) {
	ctx := context.Background()
	dir := novoDoFuturo(t, "-- orq:leitor-minimo 4\n-- orq:escritor-minimo 4")
	chamou := false
	s, err := OpenCom(ctx, dir, Opcoes{AntesDeMigrar: func(Migracao) { chamou = true }})
	if err != nil {
		t.Fatalf("abrir: %v", err)
	}
	defer s.Close()
	if chamou {
		t.Fatal("abrir um banco mais novo não migra nem avisa")
	}
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v != 5 {
		t.Fatalf("user_version = %d, %v", v, err)
	}
	// Escreve normalmente (escritor mínimo 4) e o banco continua na versão 5.
	if _, err := s.RegistrarCoord(ctx, Registro{Nome: "a", Pane: "wA:p1"}); err != nil {
		t.Fatalf("escrever: %v", err)
	}
}

// Declarar só o leitor mínimo deixa o binário anterior ler, mas não escrever.
func TestBancoMaisNovoSomenteLeitura(t *testing.T) {
	ctx := context.Background()
	dir := novoDoFuturo(t, "-- orq:leitor-minimo 4")
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("abrir: %v", err)
	}
	defer s.Close()
	if _, err := s.ListarCoords(ctx); err != nil {
		t.Fatalf("ler: %v", err)
	}
	_, err = s.RegistrarCoord(ctx, Registro{Nome: "a", Pane: "wA:p1"})
	if codigo(err) != "banco_somente_leitura" {
		t.Fatalf("escrever = %v", err)
	}
	if !strings.Contains(recoveryDe(err), "orq upgrade") {
		t.Fatalf("recovery não aponta orq upgrade: %v", err)
	}
	e, err := s.Versoes(ctx)
	if err != nil || e.EscritorMinimo != 5 || e.LeitorMinimo != 4 || e.Binario != Schema() {
		t.Fatalf("versoes = %+v, %v", e, err)
	}
}

// Migration que exige a própria versão (ou mais) para ler recusa o binário anterior.
func TestBancoMaisNovoRecusadoAbaixoDoLeitorMinimo(t *testing.T) {
	dir := novoDoFuturo(t, "-- orq:leitor-minimo 5")
	_, err := Open(context.Background(), dir)
	if codigo(err) != "versao_do_banco" {
		t.Fatalf("abrir = %v", err)
	}
	if !strings.Contains(recoveryDe(err), "orq upgrade") {
		t.Fatalf("recovery = %q", recoveryDe(err))
	}
}

// Banco legado: mais novo e sem banco_meta mantém a recusa de sempre; o v3 migra normalmente.
func TestBancoSemOMinimoGravado(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("DROP TABLE banco_meta"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if _, err := Open(ctx, dir); codigo(err) != "versao_do_banco" {
		t.Fatalf("v99 sem banco_meta: %v", err)
	}
	// v3 de verdade: sem a tabela e na versão 3, migra para a atual e grava o mínimo.
	dir = t.TempDir()
	s, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("DROP TABLE banco_meta"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA user_version = 3"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	var de, para int
	s, err = OpenCom(ctx, dir, Opcoes{AntesDeMigrar: func(m Migracao) { de, para = m.De, m.Para }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, err := s.Versoes(ctx)
	if err != nil || e.Banco != Schema() || e.LeitorMinimo != 4 || e.EscritorMinimo != 4 || de != 3 || para != Schema() {
		t.Fatalf("estado = %+v, %v; migração %d→%d", e, err, de, para)
	}
}

// O aviso antes de migrar traz o que o banco passa a exigir, somando as migrations pendentes.
func TestAntesDeMigrarInformaOExigido(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	var m Migracao
	s, err = OpenCom(ctx, dir, Opcoes{Migracoes: migrationsMais(t, "-- orq:leitor-minimo 4\n-- orq:escritor-minimo 5"), AntesDeMigrar: func(x Migracao) { m = x }})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if (m != Migracao{De: 4, Para: 5, LeitorMinimo: 4, EscritorMinimo: 5}) {
		t.Fatalf("migração = %+v", m)
	}
}

func TestDiretivaInvalida(t *testing.T) {
	for _, d := range []string{"-- orq:leitor-minimo 9", "-- orq:leitor-minimo x", "-- orq:leitor-minimo 5\n-- orq:escritor-minimo 4"} {
		_, err := OpenCom(context.Background(), t.TempDir(), Opcoes{Migracoes: migrationsMais(t, d)})
		if err == nil || !strings.Contains(err.Error(), "0005_futura.sql") {
			t.Errorf("%q: err = %v", d, err)
		}
	}
}

// Dois processos abrindo o mesmo banco v3 migram uma vez só, sob a trava de início.
func TestMigracaoConcorrenteRespeitaATrava(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("DROP TABLE banco_meta"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("PRAGMA user_version = 3"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	var mu sync.Mutex
	migracoes := 0
	var wg sync.WaitGroup
	erros := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := OpenCom(ctx, dir, Opcoes{AntesDeMigrar: func(Migracao) { mu.Lock(); migracoes++; mu.Unlock() }})
			if err != nil {
				erros <- err
				return
			}
			_ = st.Close()
		}()
	}
	wg.Wait()
	close(erros)
	for err := range erros {
		t.Errorf("abrir: %v", err)
	}
	if migracoes != 1 {
		t.Fatalf("%d aberturas migraram, queria 1", migracoes)
	}
}

func recoveryDe(err error) string { return output.AsError(err).Recovery }

// Um Store aberto antes de outro processo migrar o banco (o daemon fica aberto o tempo todo)
// confere o mínimo gravado a cada transação, não só ao abrir.
func TestConexaoAbertaAntesDaMigrationRespeitaOEscritorMinimo(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	antigo, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer antigo.Close()
	if _, err := antigo.RegistrarCoord(ctx, Registro{Nome: "antes"}); err != nil {
		t.Fatalf("antes da migration: %v", err)
	}
	novo, err := OpenCom(ctx, dir, Opcoes{Migracoes: migrationsMais(t, "-- orq:leitor-minimo 4\n-- orq:escritor-minimo 5")})
	if err != nil {
		t.Fatal(err)
	}
	defer novo.Close()

	if _, err := antigo.RegistrarCoord(ctx, Registro{Nome: "depois"}); codigo(err) != "banco_somente_leitura" {
		t.Fatalf("RegistrarCoord pela conexão antiga = %v, quero banco_somente_leitura", err)
	}
	if err := antigo.GravarCache(ctx, FonteHerdr, "", map[string]string{"k": "v"}, nil); codigo(err) != "banco_somente_leitura" {
		t.Fatalf("GravarCache pela conexão antiga = %v, quero banco_somente_leitura", err)
	}
	// A leitura continua: o leitor mínimo é 4.
	if _, err := antigo.Snapshot(ctx, false); err != nil {
		t.Fatalf("leitura pela conexão antiga: %v", err)
	}
	if e := output.AsError(antigo.GravarCache(ctx, FonteHerdr, "", nil, nil)); !strings.Contains(e.Recovery, "orq upgrade") {
		t.Fatalf("recovery = %q", e.Recovery)
	}
	if _, err := novo.RegistrarCoord(ctx, Registro{Nome: "novo"}); err != nil {
		t.Fatalf("o orq novo escreve: %v", err)
	}
}

// Quando a migration sobe o leitor mínimo, a conexão que perdeu a leitura falha com o erro
// tipado em vez de ler tabelas de um esquema que não conhece.
func TestConexaoAbertaAntesDaMigrationRespeitaOLeitorMinimo(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	antigo, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer antigo.Close()
	novo, err := OpenCom(ctx, dir, Opcoes{Migracoes: migrationsMais(t, "-- orq:leitor-minimo 5\n-- orq:escritor-minimo 5")})
	if err != nil {
		t.Fatal(err)
	}
	defer novo.Close()

	if _, err := antigo.Snapshot(ctx, false); codigo(err) != "versao_do_banco" {
		t.Fatalf("leitura pela conexão antiga = %v, quero versao_do_banco", err)
	}
	if _, err := antigo.RegistrarCoord(ctx, Registro{Nome: "x"}); codigo(err) != "versao_do_banco" {
		t.Fatalf("escrita pela conexão antiga = %v, quero versao_do_banco", err)
	}
	if e := output.AsError(antigo.GravarCache(ctx, FonteHerdr, "", nil, nil)); !strings.Contains(e.Recovery, "orq upgrade") {
		t.Fatalf("recovery = %q", e.Recovery)
	}
}
