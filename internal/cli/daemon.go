package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/enriquecer"
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
	}
	return nil
}

func (a *App) daemonCmd() *cobra.Command {
	var tick, remoto time.Duration
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Mantém o cache e os tokens de sidebar vivos, em primeiro plano (um por máquina)",
		Long: `Roda em primeiro plano até SIGTERM ou SIGINT. A cada tick consulta o Herdr e confere os
tokens de sidebar; MR e Taiga são olhados a cada --intervalo-remoto, respeitando o TTL de cada
um. Assina pane.agent_status_changed no socket do Herdr ($HERDR_SOCKET_PATH ou
~/.config/herdr/herdr.sock) e republica na hora. Ao sair, relê o Herdr e limpa os tokens que publicou e ainda têm o valor dele.
Só roda um por diretório de estado: o segundo sai com daemon_ativo. O log vai para o stderr.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := central.DirPadrao(a.Env)
			if err != nil {
				return err
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
			return d.Rodar(ctx)
		},
	}
	cmd.Flags().DurationVar(&tick, "tick", 5*time.Second, "intervalo da consulta ao Herdr e da conferência dos tokens")
	cmd.Flags().DurationVar(&remoto, "intervalo-remoto", 15*time.Second, "intervalo com que MR e Taiga são olhados (cada um ainda respeita o TTL)")
	return cmd
}
