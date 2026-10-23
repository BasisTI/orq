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
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.srv.Porta = l.Addr().(*net.TCPAddr).Port
	ctx, cancelar := context.WithCancel(context.Background())
	fim := make(chan error, 1)
	go func() { fim <- f.srv.Servir(ctx, l) }()
	t.Cleanup(func() {
		cancelar()
		<-fim
	})

	ctxCmd, cancelarCmd := context.WithTimeout(ctx, 2*time.Minute)
	defer cancelarCmd()
	args := []string{"testdata/navegador.mjs", fmt.Sprintf("http://127.0.0.1:%d", f.srv.Porta), "/", "/p/orq/15/1", "/historico", "/central"}
	saida, err := exec.CommandContext(ctxCmd, "node", args...).CombinedOutput()
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
	if len(r.Resumo) != 8 {
		t.Errorf("páginas abertas = %d, quer 8 (4 caminhos × 2 larguras)", len(r.Resumo))
	}
	for _, p := range r.Resumo {
		t.Logf("%s: aba ativa %q", p.Aqui, p.Aba)
	}
}
