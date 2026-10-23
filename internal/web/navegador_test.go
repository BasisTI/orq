//go:build browser

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// TestNavegadorSobAPoliticaDeSeguranca abre cada página no Chromium (playwright-core) e falha em
// qualquer violação de CSP ou erro de console fora da lista de permissões. Fora do go test
// padrão e do CI de PR (spec §8.1): rode com
//
//	ORQ_PLAYWRIGHT=<node_modules/playwright-core> ORQ_CHROMIUM=<chrome> [ORQ_CAPTURAS=<pasta>] \
//	  go test -tags=browser -run Navegador ./internal/web
func TestNavegadorSobAPoliticaDeSeguranca(t *testing.T) {
	pw, chrome := os.Getenv("ORQ_PLAYWRIGHT"), os.Getenv("ORQ_CHROMIUM")
	if pw == "" || chrome == "" {
		t.Skip("defina ORQ_PLAYWRIGHT (pasta do playwright-core) e ORQ_CHROMIUM (executável)")
	}
	f := nova(t, false)
	// Um diário com duas páginas, só de dados sintéticos.
	var es []central.EventoExterno
	for i := 0; i < 30; i++ {
		es = append(es, central.EventoExterno{Tipo: "limpar.removido", Detalhe: map[string]any{"tipo": "worktree", "alvo": fmt.Sprintf("/tmp/exemplo/worktree-%d", i)}})
	}
	if err := f.st.RegistrarExternos(context.Background(), &f.co, refs.US{Projeto: "orq", Ref: 15}, es); err != nil {
		t.Fatal(err)
	}
	semOndas := vazia(t, false).f
	unix := nova(t, true)
	// Cada servidor: caminhos (com "=<status>" para a página de erro que o teste provoca de propósito).
	for _, c := range []struct {
		f        *fixture
		prefixo  string
		caminhos []string
	}{
		{f, "", []string{"/", "/p/orq/15/1", "/historico", "/central", "/onda/1", "/onda/999=404", "/onda/abc=404"}},
		{semOndas, "vazio_", []string{"/central"}},
		{unix, "proxy_", []string{"/central=401"}},
	} {
		c.f.navegar(t, pw, chrome, c.prefixo, c.caminhos)
	}
}

// navegar serve a fixture em loopback e roda o navegador nos caminhos.
func (f *fixture) navegar(t *testing.T, pw, chrome, prefixo string, caminhos []string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.srv.Porta = l.Addr().(*net.TCPAddr).Port
	ctx, cancelar := context.WithCancel(context.Background())
	fim := make(chan error, 1)
	go func() { fim <- f.srv.Servir(ctx, l) }()
	defer func() {
		cancelar()
		<-fim
	}()

	ctxCmd, cancelarCmd := context.WithTimeout(ctx, 2*time.Minute)
	defer cancelarCmd()
	args := append([]string{"testdata/navegador.mjs", fmt.Sprintf("http://127.0.0.1:%d", f.srv.Porta)}, caminhos...)
	cmd := exec.CommandContext(ctxCmd, "node", args...)
	cmd.Env = append(os.Environ(), "ORQ_CAPTURA_PREFIXO="+prefixo)
	saida, err := cmd.CombinedOutput()
	var r struct {
		Resumo []struct {
			Aqui   string   `json:"aqui"`
			Aba    string   `json:"abaAtiva"`
			Falhas []string `json:"falhas"`
		} `json:"resumo"`
		Problemas []string `json:"problemas"`
	}
	linhas := strings.Split(strings.TrimSpace(string(saida)), "\n")
	if jerr := json.Unmarshal([]byte(linhas[len(linhas)-1]), &r); jerr != nil {
		t.Fatalf("saída do navegador: %v\n%s", err, saida)
	}
	for _, p := range r.Problemas {
		t.Error(p)
	}
	if len(r.Resumo) != 2*len(caminhos) {
		t.Errorf("páginas abertas = %d, quer %d (%d caminhos × 2 larguras)", len(r.Resumo), 2*len(caminhos), len(caminhos))
	}
	for _, p := range r.Resumo {
		t.Logf("%s: aba ativa %q", p.Aqui, p.Aba)
	}
}
