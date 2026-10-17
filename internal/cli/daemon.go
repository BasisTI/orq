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

func (a *App) daemonCmd() *cobra.Command {
	var tick, remoto time.Duration
	var detach bool
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Mantém o cache e os tokens de sidebar vivos, em primeiro plano (um por máquina)",
		Long: `Roda em primeiro plano até SIGTERM ou SIGINT. A cada tick consulta o Herdr e confere os
tokens de sidebar; MR e Taiga são olhados a cada --intervalo-remoto, respeitando o TTL de cada
um. Assina pane.agent_status_changed no socket do Herdr ($HERDR_SOCKET_PATH ou
~/.config/herdr/herdr.sock) e republica na hora. Ao sair, relê o Herdr e limpa os tokens que publicou e ainda têm o valor dele.
Só roda um por diretório de estado: o segundo sai com daemon_ativo. O log vai para o stderr.

Com --detach (o startup do plugin), sobe o daemon como outro processo, numa sessão própria e
com o log em daemon.log no diretório de estado, espera ele segurar a trava e sai. Se já há um
daemon, não sobe outro e sai com sucesso dizendo o pid dele.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := central.DirPadrao(a.Env)
			if err != nil {
				return err
			}
			if detach {
				r, err := a.desacoplarDaemon(cmd.Context(), dir, tick, remoto)
				if err != nil {
					return err
				}
				return a.emit(r, func(w io.Writer) error {
					_, err := fmt.Fprintf(w, "daemon %s, pid %s, log %s\n", r.Estado, r.PID, r.Log)
					return err
				})
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
			d := &enriquecer.Daemon{E: e, Socket: enriquecer.CaminhoSocket(a.Env), Tick: tick, IntervaloRemoto: remoto, Log: a.Err}
			if limpeza.LigadaPeloAmbiente(a.Env) {
				l, err := a.limpador(ctx)
				if err != nil {
					return err
				}
				d.Limpeza = &limpeza.Auto{L: l, Log: func(format string, args ...any) {
					fmt.Fprintf(a.Err, "%s limpeza: %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
				}}
			}
			return d.Rodar(ctx)
		},
	}
	cmd.Flags().DurationVar(&tick, "tick", 5*time.Second, "intervalo da consulta ao Herdr e da conferência dos tokens")
	cmd.Flags().DurationVar(&remoto, "intervalo-remoto", 15*time.Second, "intervalo com que MR e Taiga são olhados (cada um ainda respeita o TTL)")
	cmd.Flags().BoolVar(&detach, "detach", false, "sobe o daemon em segundo plano, com o log em daemon.log, e sai")
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

func (a *App) desacoplarDaemon(ctx context.Context, dir string, tick, remoto time.Duration) (ResultadoDetach, error) {
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
	cmd := exec.Command(exe, "daemon", "--tick", tick.String(), "--intervalo-remoto", remoto.String())
	// O filho recebe o ambiente do processo e, por cima, o ORQ_* que esta execução resolveu
	// (inclusive o do orq.env do plugin), com o diretório de estado fixado.
	cmd.Env = append(os.Environ(), "ORQ_STATE_DIR="+dir)
	for _, k := range []string{"ORQ_HERDR_BIN", "ORQ_GLAB_BIN", "ORQ_GH_BIN", "ORQ_TAIGA_BIN", "ORQ_GIT_BIN", "ORQ_DOCKER_BIN", "ORQ_TRABALHO_DIR", "ORQ_TMP_DIR", "ORQ_LIMPEZA_AUTOMATICA"} {
		if v := a.Env(k); v != "" {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	cmd.Stdout, cmd.Stderr = lf, lf
	desacoplar(cmd)
	if err := cmd.Start(); err != nil {
		return r, &output.Error{Code: "daemon_nao_subiu", Cause: err.Error(), Recovery: "rode orq daemon em primeiro plano para ver o erro", Exit: output.ExitUnexpected}
	}
	saiu := make(chan error, 1)
	go func() { saiu <- cmd.Wait() }()
	limite := time.NewTimer(5 * time.Second)
	defer limite.Stop()
	tique := time.NewTicker(50 * time.Millisecond)
	defer tique.Stop()
	for {
		select {
		case err := <-saiu:
			// Saiu antes de segurar a trava. Outro --detach pode ter ganho a corrida.
			if ativo, pid, _ := enriquecer.DaemonAtivo(dir); ativo {
				r.Estado, r.PID = "ja_ativo", pid
				return r, nil
			}
			return r, &output.Error{Code: "daemon_nao_subiu", Cause: fmt.Sprintf("o daemon saiu logo ao subir (%v)", err), Recovery: "veja " + log, Exit: output.ExitUnexpected}
		case <-tique.C:
			if ativo, pid, _ := enriquecer.DaemonAtivo(dir); ativo && pid == strconv.Itoa(cmd.Process.Pid) {
				r.Estado, r.PID = "iniciado", pid
				return r, nil
			}
		case <-limite.C:
			return r, &output.Error{Code: "daemon_nao_subiu", Cause: "o daemon não segurou a trava em 5 s (pid " + strconv.Itoa(cmd.Process.Pid) + ")", Recovery: "veja " + log, Exit: output.ExitUnexpected}
		case <-ctx.Done():
			return r, ctx.Err()
		}
	}
}
