//go:build browser

package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// Os testes de navegador abrem as telas no Chromium (playwright-core) e falham em qualquer
// violação de CSP e em qualquer erro de console fora da lista de permissões. Fora do go test
// padrão e do CI de PR (spec §8.1): rode com
//
//	ORQ_PLAYWRIGHT=<node_modules/playwright-core> ORQ_CHROMIUM=<chrome> [ORQ_CAPTURAS=<pasta>] \
//	  go test -tags=browser -run Navegador ./internal/web

func ferramentasDoNavegador(t *testing.T) (pw, chrome string) {
	t.Helper()
	pw, chrome = os.Getenv("ORQ_PLAYWRIGHT"), os.Getenv("ORQ_CHROMIUM")
	if pw == "" || chrome == "" {
		t.Skip("defina ORQ_PLAYWRIGHT (pasta do playwright-core) e ORQ_CHROMIUM (executável)")
	}
	return pw, chrome
}

// servirNoNavegador sobe um servidor em loopback sobre o store e devolve o endereço base.
func servirNoNavegador(t *testing.T, st *central.Store, agora func() time.Time) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &Servidor{St: st, Porta: l.Addr().(*net.TCPAddr).Port, Agora: agora}
	ctx, cancelar := context.WithCancel(context.Background())
	fim := make(chan error, 1)
	go func() { fim <- srv.Servir(ctx, l) }()
	t.Cleanup(func() {
		cancelar()
		<-fim
	})
	return fmt.Sprintf("http://127.0.0.1:%d", srv.Porta)
}

func abrirStore(t *testing.T) *central.Store {
	t.Helper()
	st, err := central.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

type saidaNavegador struct {
	Resumo []struct {
		Aqui   string   `json:"aqui"`
		Aba    string   `json:"abaAtiva"`
		Falhas []string `json:"falhas"`
	} `json:"resumo"`
	Problemas []string `json:"problemas"`
}

// rodarNode executa um script de testdata e devolve o JSON da última linha da saída.
func rodarNode(t *testing.T, env []string, script string, args ...string) saidaNavegador {
	t.Helper()
	ctx, cancelar := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancelar()
	cmd := exec.CommandContext(ctx, "node", append([]string{"testdata/" + script}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	saida, err := cmd.CombinedOutput()
	linhas := strings.Split(strings.TrimSpace(string(saida)), "\n")
	var r saidaNavegador
	if jerr := json.Unmarshal([]byte(linhas[len(linhas)-1]), &r); jerr != nil {
		t.Fatalf("saída do navegador (%s): %v\n%s", script, err, saida)
	}
	return r
}

func TestNavegadorSobAPoliticaDeSeguranca(t *testing.T) {
	pw, chrome := ferramentasDoNavegador(t)
	st := abrirStore(t)
	semear(t, context.Background(), st)
	env := []string{"ORQ_PLAYWRIGHT=" + pw, "ORQ_CHROMIUM=" + chrome}
	if c := os.Getenv("ORQ_CAPTURAS"); c != "" {
		env = append(env, "ORQ_CAPTURAS="+c)
	}
	// Um diário com duas páginas (ondas 1: 30 eventos sintéticos).
	var es []central.EventoExterno
	for i := 0; i < 30; i++ {
		es = append(es, central.EventoExterno{Tipo: "limpar.removido", Detalhe: map[string]any{"tipo": "worktree", "alvo": fmt.Sprintf("/tmp/exemplo/worktree-%d", i)}})
	}
	if err := st.RegistrarExternos(context.Background(), nil, refsUS("catalogo", 404), es); err != nil {
		t.Fatal(err)
	}
	// "=<status>" é a página de erro que o teste provoca de propósito.
	caminhos := []string{"/", "/p/catalogo/404/2", "/historico", "/onda/1", "/onda/999=404", "/onda/abc=404", "/central", "/pendencias", "/pendencias?projeto=nuvem&coord=cais", "/pendencias?projeto=agenda&coord=bussola&onda=4", "/projeto/catalogo", "/projeto/nuvem", "/projeto/faturamento", "/projeto/agenda", "/projeto/nada=404"}

	t.Run("Central", func(t *testing.T) {
		r := rodarNode(t, env, "navegador.mjs", append([]string{servirNoNavegador(t, st, nil)}, caminhos...)...)
		for _, p := range r.Problemas {
			t.Error(p)
		}
		if want := 2 * len(caminhos); len(r.Resumo) != want {
			t.Errorf("páginas abertas = %d, quer %d (%d caminhos × 2 larguras)", len(r.Resumo), want, len(caminhos))
		}
		for _, p := range r.Resumo {
			t.Logf("%s: aba ativa %q", p.Aqui, p.Aba)
		}
	})

	// Dados vencidos: o relógio do servidor uma hora à frente, sem o daemon renovar o cache.
	t.Run("DadosVencidos", func(t *testing.T) {
		base := servirNoNavegador(t, st, func() time.Time { return time.Now().Add(time.Hour) })
		r := rodarNode(t, append(env, "ORQ_CAPTURA_PREFIXO=vencida-"), "navegador.mjs", base, "/")
		for _, p := range r.Problemas {
			t.Error(p)
		}
	})

	// Pelo socket Unix, sem o cabeçalho do proxy: a página 401 com o banner.
	t.Run("SemProxy401", func(t *testing.T) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		srv := &Servidor{St: st, Unix: true}
		ctx, cancelar := context.WithCancel(context.Background())
		fim := make(chan error, 1)
		go func() { fim <- srv.Servir(ctx, l) }()
		defer func() { cancelar(); <-fim }()
		r := rodarNode(t, append(env, "ORQ_CAPTURA_PREFIXO=proxy_"), "navegador.mjs", fmt.Sprintf("http://127.0.0.1:%d", l.Addr().(*net.TCPAddr).Port), "/=401")
		for _, p := range r.Problemas {
			t.Error(p)
		}
	})

	t.Run("SemOndas", func(t *testing.T) {
		vz := abrirStore(t)
		if _, err := vz.RegistrarCoord(context.Background(), central.Registro{Nome: "alfa", Pane: "wA:p1", Projeto: "proj"}); err != nil {
			t.Fatal(err)
		}
		base := servirNoNavegador(t, vz, nil)
		r := rodarNode(t, append(env, "ORQ_CAPTURA_PREFIXO=vazia-"), "navegador.mjs", base, "/")
		for _, p := range r.Problemas {
			t.Error(p)
		}
	})
}

// controle é o canal de teste que muda o banco enquanto o navegador está aberto (o navegador
// não tem como criar onda; a página só responde pendência).
func controle(t *testing.T, st *central.Store) string {
	t.Helper()
	ctx := context.Background()
	mux := http.NewServeMux()
	falha := func(w http.ResponseWriter, err error) {
		if err != nil {
			http.Error(w, err.Error(), 500)
		}
	}
	mux.HandleFunc("/nova-onda", func(w http.ResponseWriter, r *http.Request) {
		co, err := st.RegistrarCoord(ctx, central.Registro{Nome: "aurora", Pane: "wA:p1", Projeto: "catalogo"})
		if err == nil {
			_, err = st.CriarOnda(ctx, co, central.NovaOnda{Titulo: "Onda nova do teste", US: []refs.US{{Projeto: "catalogo", Ref: 900}}})
		}
		falha(w, err)
	})
	mux.HandleFunc("/encerrar-onda", func(w http.ResponseWriter, r *http.Request) {
		_, err := st.EncerrarOnda(ctx, "", 8)
		falha(w, err)
	})
	// A pendência é de outra pessoa/coordenador: o teste precisa que ela mude embaixo da página aberta.
	pend := func(r *http.Request) refs.Pend {
		ref, _ := strconv.Atoi(r.URL.Query().Get("ref"))
		n, _ := strconv.Atoi(r.URL.Query().Get("n"))
		return refs.Pend{US: refs.US{Projeto: r.URL.Query().Get("proj"), Ref: ref}, Numero: n}
	}
	mux.HandleFunc("/responder", func(w http.ResponseWriter, r *http.Request) {
		_, err := st.ResponderPendencia(ctx, nil, pend(r), central.Resposta{Texto: "respondida por outra pessoa", Por: "outra"})
		falha(w, err)
	})
	nPend := 0
	mux.HandleFunc("/nova-pend", func(w http.ResponseWriter, r *http.Request) {
		nPend++
		ref, _ := strconv.Atoi(r.URL.Query().Get("ref"))
		_, err := st.RegistrarPendencia(ctx, central.NovaPendencia{US: refs.US{Projeto: r.URL.Query().Get("proj"), Ref: ref}, Tipo: "duvida", Titulo: "Chegou depois da carga", Autor: "w", Detalhe: "d", Slug: fmt.Sprintf("np%d", nPend), Hash: fmt.Sprintf("np%d", nPend)})
		falha(w, err)
	})
	mux.HandleFunc("/resolver", func(w http.ResponseWriter, r *http.Request) {
		_, err := st.ResolverPendencia(ctx, nil, pend(r), "resolvida pelo coordenador")
		falha(w, err)
	})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(l)
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + l.Addr().String()
}

// TestNavegadorVivo: o /vivo não apaga a página, não fecha o que está aberto, não apaga o que foi
// digitado e, com a lista diferente da da página (prioridade que muda, onda nova, onda
// encerrada), avisa e segue atualizando só o que a página tem.
func TestNavegadorVivo(t *testing.T) {
	pw, chrome := ferramentasDoNavegador(t)
	st := abrirStore(t)
	semear(t, context.Background(), st)
	env := []string{"ORQ_PLAYWRIGHT=" + pw, "ORQ_CHROMIUM=" + chrome}
	if c := os.Getenv("ORQ_CAPTURAS"); c != "" {
		env = append(env, "ORQ_CAPTURAS="+c)
	}
	r := rodarNode(t, env, "vivo.mjs", servirNoNavegador(t, st, nil), controle(t, st))
	for _, p := range r.Problemas {
		t.Error(p)
	}
}

// TestNavegadorPendencias: a pendência no lugar, os erros dentro do formulário sem perder o texto,
// o /vivo que não toca o corpo aberto, a cascata e o Voltar que mantém a página inteira (story 3).
func TestNavegadorPendencias(t *testing.T) {
	pw, chrome := ferramentasDoNavegador(t)
	st := abrirStore(t)
	semear(t, context.Background(), st)
	env := []string{"ORQ_PLAYWRIGHT=" + pw, "ORQ_CHROMIUM=" + chrome}
	if c := os.Getenv("ORQ_CAPTURAS"); c != "" {
		env = append(env, "ORQ_CAPTURAS="+c)
	}
	r := rodarNode(t, env, "pendencias.mjs", servirNoNavegador(t, st, nil), controle(t, st))
	for _, p := range r.Problemas {
		t.Error(p)
	}
	// O celular: um banco novo, porque responder muda o estado.
	t.Run("Celular", func(t *testing.T) {
		st := abrirStore(t)
		semear(t, context.Background(), st)
		r := rodarNode(t, append(env, "ORQ_LARGURA=390"), "pendencias.mjs", servirNoNavegador(t, st, nil), controle(t, st))
		for _, p := range r.Problemas {
			t.Error(p)
		}
	})
}

// TestNavegadorComposicao: o POST publica só a composição que a página renderizou mais a mudança
// que ele aplicou; o que mudou fora dele (pendência nova, resolvida, onda/projeto que aparece ou
// some) continua sendo avisado pelo /vivo (S1), e a aba antiga move o item (M1).
func TestNavegadorComposicao(t *testing.T) {
	pw, chrome := ferramentasDoNavegador(t)
	env := []string{"ORQ_PLAYWRIGHT=" + pw, "ORQ_CHROMIUM=" + chrome}
	for _, caso := range []string{"pendência nova", "pendência resolvida por outro", "onda nova", "onda encerrada", "sem mudança alheia", "M1", "M2", "concorrente", "concorrente na Central", "cascata", "voltar", "vivo no meio da fila"} {
		t.Run(caso, func(t *testing.T) {
			st := abrirStore(t)
			semear(t, context.Background(), st)
			r := rodarNode(t, append(env, "ORQ_CASO="+caso), "composicao.mjs", servirNoNavegador(t, st, nil), controle(t, st))
			for _, p := range r.Problemas {
				t.Error(p)
			}
		})
	}
}
