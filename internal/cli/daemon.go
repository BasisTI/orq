package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/limpeza"
	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/pendencias"
	"github.com/BasisTI/orq/internal/web"
	"github.com/spf13/cobra"
)

// enriquecedor monta o enriquecedor com o banco e o ambiente da execução.
func (a *App) enriquecedor(ctx context.Context) (*enriquecer.Enriquecedor, error) {
	st, err := a.store(ctx)
	if err != nil {
		return nil, err
	}
	return &enriquecer.Enriquecedor{St: st, F: enriquecer.Ferramentas{Env: a.Env}}, nil
}

func (a *App) atualizarCmd() *cobra.Command {
	var fontes []string
	var forcar bool
	cmd := &cobra.Command{
		Use:   "atualizar [--fonte herdr|mr|taiga] [--forcar]",
		Short: "Consulta Herdr, MRs e Taiga uma vez, grava o cache e confere os tokens de sidebar",
		Long: `Consulta as fontes uma vez (todas, sem --fonte) e grava o cache que o snapshot mostra.
Cada chave respeita o TTL da fonte (Herdr 5 s, MR 60 s, Taiga 5 min); --forcar ignora o TTL e a
suspensão por erro de autenticação. Com o Herdr entre as fontes, confere também os tokens de
sidebar (orq no pane do coordenador, orq_mrs no workspace). Valor de outro produtor nesses
nomes não é adotado, sobrescrito nem limpo; aparece em avisos.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := enriquecer.ValidarFontes(fontes); err != nil {
				return usoDe(err)
			}
			e, err := a.enriquecedor(ctx)
			if err != nil {
				return err
			}
			r, err := e.Atualizar(ctx, enriquecer.Opcoes{Fontes: fontes, Forcar: forcar})
			if err != nil {
				return err
			}
			if h, ok := r.Fontes[central.FonteHerdr]; ok && len(h.Erros) == 0 {
				t, err := e.PublicarTokens(ctx)
				if err != nil {
					return err
				}
				r.Tokens = &t
			}
			return a.emit(r, func(w io.Writer) error { return textoAtualizar(w, r) })
		},
	}
	cmd.Flags().StringSliceVar(&fontes, "fonte", nil, "fonte a consultar: herdr, mr ou taiga (repetível; padrão: todas)")
	cmd.Flags().BoolVar(&forcar, "forcar", false, "ignora o TTL e a suspensão por erro de autenticação")
	return cmd
}

func textoAtualizar(w io.Writer, r enriquecer.Resultado) error {
	for _, f := range central.Fontes {
		rf, ok := r.Fontes[f]
		if !ok {
			continue
		}
		if _, err := fmt.Fprintf(w, "%-6s consultadas %d  em dia %d  suspensas %d  erros %d\n", f, rf.Consultadas, rf.EmDia, rf.Suspensas, len(rf.Erros)); err != nil {
			return err
		}
		for _, e := range rf.Erros {
			chave := e.Chave
			if chave == "" {
				chave = f
			}
			if _, err := fmt.Fprintf(w, "       %s: %s: %s\n", q(chave), q(e.Erro.Codigo), q(e.Erro.Mensagem)); err != nil {
				return err
			}
		}
	}
	if t := r.Tokens; t != nil {
		if t.Pulado != nil {
			_, err := fmt.Fprintf(w, "tokens não conferidos: %s\n", q(*t.Pulado))
			return err
		}
		for _, tk := range t.Publicados {
			if _, err := fmt.Fprintf(w, "token %s=%s publicado em %s %s\n", tk.Nome, q(tk.Valor), tk.Alvo, q(tk.AlvoID)); err != nil {
				return err
			}
		}
		for _, tk := range t.Limpos {
			if _, err := fmt.Fprintf(w, "token %s limpo em %s %s\n", tk.Nome, tk.Alvo, q(tk.AlvoID)); err != nil {
				return err
			}
		}
		if len(t.Erros) > 0 {
			if _, err := fmt.Fprintf(w, "erros nos tokens: %s\n", q(strings.Join(t.Erros, "; "))); err != nil {
				return err
			}
		}
		for _, a := range t.Avisos {
			if _, err := fmt.Fprintf(w, "aviso: %s\n", q(a)); err != nil {
				return err
			}
		}
	}
	return nil
}

// opcoesWeb são as flags que ligam o servidor web do daemon; --detach as repassa ao filho.
type opcoesWeb struct {
	Escuta    string // --web: unix:<caminho>, unix: (padrão) ou 127.0.0.1:<porta>; vazio desliga
	URL       string // --web-url: a URL pública, que entra nos avisos e no registrada.json
	Cabecalho string // --web-cabecalho: o cabeçalho com o usuário que o proxy autenticou
}

func (a *App) daemonCmd() *cobra.Command {
	var tick, remoto time.Duration
	var detach bool
	var prontoFD int
	var web opcoesWeb
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Mantém o cache e os tokens de sidebar vivos, em primeiro plano (um por máquina)",
		Long: `Roda em primeiro plano até SIGTERM ou SIGINT. A cada tick consulta o Herdr e confere os
tokens de sidebar; MR e Taiga são olhados a cada --intervalo-remoto, respeitando o TTL de cada
um. Assina pane.agent_status_changed no socket do Herdr ($HERDR_SOCKET_PATH ou
~/.config/herdr/herdr.sock) e republica na hora. Ao sair, relê o Herdr e limpa os tokens que publicou e ainda têm o valor dele.
Só roda um por diretório de estado: o segundo sai com daemon_ativo. O log vai para o stderr.

Com --detach (o startup do plugin), sobe o daemon como outro processo, numa sessão própria e
com o log em daemon.log no diretório de estado, espera ele estar pronto — trava de daemon
tomada, banco aberto e compatível com este orq — e sai. Se o filho sai antes (banco de versão
que este orq não abre, por exemplo), o --detach falha com o erro dele, tirado do daemon.log. Se
já há um daemon, não sobe outro e sai com sucesso dizendo o pid dele.

Um daemon em funcionamento confere o banco a cada transação. Se outro processo migrar o banco
para uma versão que este daemon não escreve (banco_somente_leitura) ou não lê (versao_do_banco),
ele registra o erro no log, solta a trava e sai com código 4, em vez de ficar vivo sem escrever;
rode orq upgrade para subir um daemon da versão nova.

A cada --tick o daemon também importa os arquivos de pendência da pasta de trabalho
($ORQ_TRABALHO_DIR/<projeto>/TG-<ref>/…/pendencias/<slug>.md) e avisa o coordenador da US quando uma
pendência abre e quando a pessoa responde (só com ele idle ou done).

Com --web, serve as pendências numa página, dentro do daemon: --web unix:<caminho> (sem caminho,
$XDG_RUNTIME_DIR/orq/web.sock, 0600), atrás de um proxy que autentica e manda o usuário no cabeçalho
--web-cabecalho; ou --web 127.0.0.1:<porta>, para uso local, em que o cabeçalho é ignorado. Sem --web,
o daemon não abre servidor. Falha ao abrir o socket é falha de partida (web_nao_abriu, saída 4), e o
--detach só termina com a web aberta. ORQ_WEB, ORQ_WEB_URL e ORQ_WEB_CABECALHO (também no orq.env do
plugin) valem como padrão das três flags.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := central.DirPadrao(a.Env)
			if err != nil {
				return err
			}
			if detach {
				r, err := a.desacoplarDaemon(cmd.Context(), dir, tick, remoto, web)
				if err != nil {
					return err
				}
				return a.emit(r, func(w io.Writer) error {
					_, err := fmt.Fprintf(w, "daemon %s, pid %s, log %s\n", r.Estado, r.PID, r.Log)
					return err
				})
			}
			var pronto *os.File
			if prontoFD > 0 {
				// Primeiro de tudo: o descritor do --detach não pode vazar para o herdr, o go e o
				// resto dos subprocessos que o daemon lança.
				syscall.CloseOnExec(prontoFD)
				pronto = os.NewFile(uintptr(prontoFD), "pronto")
			}
			soltar, err := enriquecer.TravarDaemon(dir)
			if err != nil {
				return err
			}
			defer soltar()
			ctx, parar := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer parar()
			e, err := a.enriquecedor(ctx)
			if err != nil {
				return err
			}
			// Só pronto quando o daemon já pode escrever: um banco que este orq só lê (ou não lê)
			// faria o primeiro ciclo sair logo.
			if err := e.St.ConferirEscrita(ctx); err != nil {
				return err
			}
			logw := func(format string, args ...any) {
				fmt.Fprintf(a.Err, "%s %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
			}
			d := &enriquecer.Daemon{E: e, Socket: enriquecer.CaminhoSocket(a.Env), Tick: tick, IntervaloRemoto: remoto, Log: a.Err}
			imp, err := a.importador(e.St)
			if err != nil {
				return err
			}
			imp.BaseURL, imp.Log = web.URL, func(format string, args ...any) { logw("pendências: "+format, args...) }
			d.Pendencias = &pendencias.Servico{St: e.St, Imp: imp, Av: pendencias.Herdr{F: e.F}, Log: func(format string, args ...any) { logw("pendências: "+format, args...) }}
			if limpeza.LigadaPeloAmbiente(a.Env) {
				l, err := a.limpador(ctx)
				if err != nil {
					return err
				}
				d.Limpeza = &limpeza.Auto{L: l, Log: func(format string, args ...any) {
					fmt.Fprintf(a.Err, "%s limpeza: %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
				}}
			}
			// A web abre antes da prontidão: o --detach só termina com ela escutando, e um socket
			// que não abre é falha de partida, não um daemon sem página.
			parada, err := a.abrirWeb(ctx, e.St, web, logw)
			if err != nil {
				return err
			}
			defer parada()
			if pronto != nil {
				_, _ = pronto.Write([]byte{1})
				_ = pronto.Close()
			}
			return d.Rodar(ctx)
		},
	}
	cmd.Flags().DurationVar(&tick, "tick", 5*time.Second, "intervalo da consulta ao Herdr e da conferência dos tokens")
	cmd.Flags().DurationVar(&remoto, "intervalo-remoto", 15*time.Second, "intervalo com que MR e Taiga são olhados (cada um ainda respeita o TTL)")
	cmd.Flags().StringVar(&web.Escuta, "web", a.Env("ORQ_WEB"), "serve as pendências na web: unix:<caminho> (unix: usa $XDG_RUNTIME_DIR/orq/web.sock) ou 127.0.0.1:<porta>; vazio desliga")
	cmd.Flags().StringVar(&web.URL, "web-url", a.Env("ORQ_WEB_URL"), "URL pública da web, que entra nos avisos e no registrada.json (padrão: só o caminho)")
	cmd.Flags().StringVar(&web.Cabecalho, "web-cabecalho", a.Env("ORQ_WEB_CABECALHO"), "cabeçalho com o usuário que o proxy autenticou, aceito só no socket Unix (padrão X-Orq-User)")
	cmd.Flags().BoolVar(&detach, "detach", false, "sobe o daemon em segundo plano, com o log em daemon.log, e sai")
	cmd.Flags().IntVar(&prontoFD, "pronto-fd", 0, "uso interno do --detach: descritor em que avisar que o banco abriu")
	_ = cmd.Flags().MarkHidden("pronto-fd")
	return cmd
}

// ArquivoLogDaemon recebe o stdout e o stderr do daemon desacoplado, no diretório de estado.
const ArquivoLogDaemon = "daemon.log"

// limiteLogDaemon: acima disso, o log vira daemon.log.1 antes de um daemon novo subir.
const limiteLogDaemon = 5 << 20

// ResultadoDetach é a saída de orq daemon --detach.
type ResultadoDetach struct {
	Estado string `json:"estado"` // iniciado ou ja_ativo
	PID    string `json:"pid"`
	Log    string `json:"log"`
}

func (a *App) desacoplarDaemon(ctx context.Context, dir string, tick, remoto time.Duration, web opcoesWeb) (ResultadoDetach, error) {
	log := filepath.Join(dir, ArquivoLogDaemon)
	r := ResultadoDetach{Log: log}
	if ativo, pid, err := enriquecer.DaemonAtivo(dir); err != nil {
		return r, err
	} else if ativo {
		r.Estado, r.PID = "ja_ativo", pid
		return r, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return r, err
	}
	if st, err := os.Stat(log); err == nil && st.Size() > limiteLogDaemon {
		_ = os.Rename(log, log+".1")
	}
	lf, err := os.OpenFile(log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return r, err
	}
	defer lf.Close()
	exe, err := os.Executable()
	if err != nil {
		return r, err
	}
	// O filho avisa por este pipe quando o banco abriu: só a trava de daemon não basta, porque
	// ele ainda pode sair com versao_do_banco (ou esperar a trava de início de outro orq).
	pr, pw, err := os.Pipe()
	if err != nil {
		return r, err
	}
	defer pr.Close()
	var tamanhoLog int64
	if st, err := lf.Stat(); err == nil {
		tamanhoLog = st.Size()
	}
	args := []string{"daemon", "--tick", tick.String(), "--intervalo-remoto", remoto.String(), "--pronto-fd", "3"}
	for _, f := range []struct{ nome, valor string }{{"--web", web.Escuta}, {"--web-url", web.URL}, {"--web-cabecalho", web.Cabecalho}} {
		if f.valor != "" {
			args = append(args, f.nome+"="+f.valor)
		}
	}
	cmd := exec.Command(exe, args...)
	cmd.ExtraFiles = []*os.File{pw}
	// O filho recebe o ambiente do processo e, por cima, o ORQ_* que esta execução resolveu
	// (inclusive o do orq.env do plugin), com o diretório de estado fixado.
	cmd.Env = append(os.Environ(), "ORQ_STATE_DIR="+dir)
	for _, k := range []string{"ORQ_HERDR_BIN", "ORQ_GLAB_BIN", "ORQ_GH_BIN", "ORQ_TAIGA_BIN", "ORQ_GIT_BIN", "ORQ_DOCKER_BIN", "ORQ_TRABALHO_DIR", "ORQ_TMP_DIR", "ORQ_LIMPEZA_AUTOMATICA", "ORQ_WEB", "ORQ_WEB_URL", "ORQ_WEB_CABECALHO"} {
		if v := a.Env(k); v != "" {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	cmd.Stdout, cmd.Stderr = lf, lf
	desacoplar(cmd)
	err = cmd.Start()
	_ = pw.Close()
	if err != nil {
		return r, &output.Error{Code: "daemon_nao_subiu", Cause: err.Error(), Recovery: "rode orq daemon em primeiro plano para ver o erro", Exit: output.ExitUnexpected}
	}
	saiu := make(chan error, 1)
	go func() { saiu <- cmd.Wait() }()
	pronto := make(chan bool, 1)
	go func() {
		var b [1]byte
		n, _ := pr.Read(b[:])
		pronto <- n == 1
	}()
	limite := time.NewTimer(prontidaoDaemon)
	defer limite.Stop()
	for {
		select {
		case err := <-saiu:
			// Saiu antes de ficar pronto. Outro --detach pode ter ganho a corrida.
			if ativo, pid, _ := enriquecer.DaemonAtivo(dir); ativo {
				r.Estado, r.PID = "ja_ativo", pid
				return r, nil
			}
			return r, &output.Error{Code: "daemon_nao_subiu", Cause: fmt.Sprintf("o daemon saiu ao subir (%v): %s", err, caudaDoLog(log, tamanhoLog)), Recovery: "veja " + log, Exit: output.ExitConflict}
		case ok := <-pronto:
			if ok {
				r.Estado, r.PID = "iniciado", strconv.Itoa(cmd.Process.Pid)
				return r, nil
			}
			// Pipe fechado sem aviso: o filho saiu, e o ramo acima traz o motivo.
			pronto = nil
		case <-limite.C:
			return r, &output.Error{Code: "daemon_nao_subiu", Cause: fmt.Sprintf("o daemon (pid %d) não ficou pronto em %s: não abriu o banco", cmd.Process.Pid, prontidaoDaemon), Recovery: "veja " + log, Exit: output.ExitUnexpected}
		case <-ctx.Done():
			return r, ctx.Err()
		}
	}
}

// prontidaoDaemon é quanto o --detach espera o filho abrir o banco (inclui a fila da trava de
// início e uma migration em andamento).
const prontidaoDaemon = 20 * time.Second

// caudaDoLog devolve o que o filho escreveu no log a partir de desde, em uma linha.
func caudaDoLog(log string, desde int64) string {
	b, err := os.ReadFile(log)
	if err != nil || int64(len(b)) <= desde {
		return "sem saída no log"
	}
	t := strings.Join(strings.Fields(string(b[desde:])), " ")
	if r := []rune(t); len(r) > 600 {
		t = string(r[len(r)-600:])
	}
	return t
}

// abrirWeb abre o socket da web, se pedido, e serve em segundo plano. A função devolvida para o
// servidor, fecha o socket e espera: chame-a ao sair do daemon, por qualquer motivo.
func (a *App) abrirWeb(ctx context.Context, st *central.Store, o opcoesWeb, logw func(string, ...any)) (func(), error) {
	if o.Escuta == "" {
		return func() {}, nil
	}
	esc, err := web.Abrir(o.Escuta, a.Env)
	if err != nil {
		return nil, err
	}
	srv := &web.Servidor{St: st, Unix: esc.Unix, CabecalhoUsuario: o.Cabecalho, Porta: esc.Porta, Log: logw}
	ctxWeb, parar := context.WithCancel(ctx)
	fim := make(chan struct{})
	go func() {
		defer close(fim)
		if err := srv.Servir(ctxWeb, esc.Listener); err != nil {
			logw("web: o servidor parou: %v", err)
		}
	}()
	logw("web: escutando em %s", esc.Descricao)
	return func() {
		parar()
		<-fim
		_ = esc.Close()
	}, nil
}
