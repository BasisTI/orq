package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/upgrade"
	"github.com/spf13/cobra"
)

func (a *App) upgradeCmd() *cobra.Command {
	var ref string
	cmd := &cobra.Command{
		Use:   "upgrade [--ref main]",
		Short: "Atualiza o orq do go install e o do plugin do Herdr, reinicia o daemon e confere as versões",
		Long: `Atualiza de uma vez os binários do orq que compartilham o banco da Central:

  1. go install github.com/BasisTI/orq/cmd/orq@<ref>, quando existe um orq do go install que
     não é o mesmo arquivo que o do plugin;
  2. herdr plugin install BasisTI/orq --ref <ref> --yes, que refaz o bin/orq do plugin;
  3. reinicia o daemon: pára o antigo (SIGTERM), espera a trava soltar e sobe o novo com os
     mesmos --tick e --intervalo-remoto e o mesmo ambiente. Se nenhum binário novo sobe, volta ao
     daemon anterior; um daemon parado é dito como falha;
  4. confere que todos os binários encontrados (PATH, go install, plugin) e o daemon em
     execução estão no mesmo commit, e diz qual ficou para trás e se ele ainda abre o banco;
  5. lista os panes ` + "`orq tui`" + ` abertos com o binário antigo, que precisam ser fechados e abertos
     de novo (orq central abrir).

Uma falha no go install ou no plugin não interrompe a outra etapa nem deixa o daemon parado: o
comando sai com upgrade_incompleto (exit 4) dizendo o que ficou para trás. Rodar de novo só
repete o que falta. Não abre o banco, então funciona mesmo quando este orq recusa o banco com
versao_do_banco. Não mexe no servidor do Herdr.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := central.DirPadrao(a.Env)
			if err != nil {
				return err
			}
			o := upgrade.Opcoes{Ref: ref}
			if a.mode() == output.Text {
				o.Log = func(format string, args ...any) { fmt.Fprintf(a.Err, "orq upgrade: "+format+"\n", args...) }
			}
			r, err := upgrade.Executar(cmd.Context(), upgrade.Ambiente{Env: a.Env, Dir: dir}, o)
			if r == nil {
				return err
			}
			if eerr := a.emit(r, func(w io.Writer) error { return textoUpgrade(w, r) }); eerr != nil {
				return eerr
			}
			return err
		},
	}
	cmd.Flags().StringVar(&ref, "ref", upgrade.RefPadrao, "branch, tag ou commit do BasisTI/orq a instalar")
	return cmd
}

func textoUpgrade(w io.Writer, r *upgrade.Resultado) error {
	p := func(format string, args ...any) { fmt.Fprintf(w, format+"\n", args...) }
	for _, e := range r.Etapas {
		p("%-6s %-10s %s", e.Estado, e.Nome, q(e.Detalhe))
	}
	if r.Referencia != "" {
		p("versão de referência: %s", r.Referencia)
		for _, b := range r.Depois {
			marca := "ok   "
			if b.Erro != "" || !upgrade.MesmoCommit(b.Commit, r.Referencia) {
				marca = "ATRAS"
			}
			quem := strings.Join(b.Rotulos, " + ")
			if b.PID != "" {
				quem += " pid " + b.PID
			}
			p("%s  %-22s %s  %s", marca, quem, q(b.Versao), q(b.Caminho))
		}
	}
	for _, av := range r.Avisos {
		p("aviso: %s", q(av))
	}
	if len(r.TUIs) > 0 {
		var pids []string
		for _, t := range r.TUIs {
			pids = append(pids, t.PID)
		}
		p("reabra os panes `orq tui` (pids %s): eles seguem com o binário antigo; feche e abra de novo, ou rode orq central abrir", strings.Join(pids, ", "))
	}
	if r.OK {
		p("tudo na mesma versão")
	}
	return nil
}

// avisarLeitores roda antes de uma migration: diz, no stderr, quais outros orq conhecidos (o do
// PATH, do go install, do plugin e o daemon em execução) deixam de abrir o banco depois dela.
func (a *App) avisarLeitores(m central.Migracao) {
	if m.De == 0 {
		return // banco novo: nenhum outro binário o usa ainda
	}
	dir, err := central.DirPadrao(a.Env)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	eu, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(eu); err == nil {
		eu = r
	}
	meuCommit := currentCommit(currentVersion())
	for _, b := range (upgrade.Ambiente{Env: a.Env, Dir: dir}).Conhecidos(ctx) {
		if b.Erro != "" {
			continue
		}
		if r, err := filepath.EvalSymlinks(b.Caminho); err == nil && r == eu {
			continue
		}
		if b.Commit != "" && upgrade.MesmoCommit(b.Commit, meuCommit) {
			continue
		}
		quem := strings.Join(b.Rotulos, " + ") + " (" + b.Caminho + ", versão " + b.Versao + ")"
		if b.PID != "" {
			quem = "daemon em execução (pid " + b.PID + ", versão " + b.Versao + ")"
		}
		switch {
		case b.Schema == 0 || b.Schema < m.LeitorMinimo:
			fmt.Fprintf(a.Err, "aviso: ao migrar o banco da versão %d para a %d, o orq %s deixa de abrir o banco (versao_do_banco); rode `orq upgrade`\n", m.De, m.Para, quem)
		case b.Schema < m.EscritorMinimo:
			fmt.Fprintf(a.Err, "aviso: ao migrar o banco da versão %d para a %d, o orq %s passa a abrir o banco só para leitura; rode `orq upgrade`\n", m.De, m.Para, quem)
		}
	}
}
