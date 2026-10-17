package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/limpeza"
	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
	"github.com/spf13/cobra"
)

// limpador monta o Limpador com o banco e o ambiente da execução.
func (a *App) limpador(ctx context.Context) (*limpeza.Limpador, error) {
	st, err := a.store(ctx)
	if err != nil {
		return nil, err
	}
	raizes, err := limpeza.RaizesDe(a.Env)
	if err != nil {
		return nil, err
	}
	return &limpeza.Limpador{St: st, F: enriquecer.Ferramentas{Env: a.Env}, Raizes: raizes, PaneProprio: a.Env("HERDR_PANE_ID")}, nil
}

func (a *App) limparCmd() *cobra.Command {
	var dry bool
	var onda int64
	cmd := &cobra.Command{
		Use:   "limpar <ref> [--dry-run] | --onda <id> [--dry-run]",
		Short: "Remove os recursos de uma US cujas MRs foram mescladas (panes, worktree, branches, pastas, Docker)",
		Long: `Só age se todas as MRs da US estão mescladas (conferidas na forja, não no cache) e, havendo head
aprovado gravado no recursos.json, se o head mesclado é ele. Então, na ordem:
  - fecha os panes da US (nome tg-<ref>_* ou <projeto>-tg-<ref>_*) cujo agente está idle ou done,
    relendo o Herdr logo antes de fechar;
  - remove o worktree, se limpo e com o conteúdo já na branch de destino (squash: pelo conteúdo);
  - apaga a branch local e a remota, nas mesmas condições;
  - remove a pasta temporária (/tmp/orq/<projeto>/TG-<ref>) e, por último, a pasta de trabalho
    (~/tmp/orquestrador/<projeto>/TG-<ref>), que só sai se todo o resto foi resolvido;
  - remove os contêineres, redes, volumes e imagens com o rótulo orq.us=<projeto>/<ref>.
O que não passa numa conferência fica preservado, com o motivo, e vira evento no diário.
Nunca usa prune. Com --dry-run, mostra o que seria feito sem tocar em nada.

Com --onda, remove só os recursos Docker com o rótulo orq.onda=<id> de uma onda encerrada ou
pronta para encerrar.

O orq daemon roda isto sozinho no merge (veja ORQ_LIMPEZA_AUTOMATICA).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			l, err := a.limpador(ctx)
			if err != nil {
				return err
			}
			var res limpeza.Resultado
			switch {
			case onda > 0 && len(args) == 0:
				o, err := l.St.VerOnda(ctx, onda)
				if err != nil {
					return err
				}
				if enc := central.EncerramentoDe(o); o.EncerradaEm == nil && (enc == nil || !enc.Pronta) {
					return &output.Error{Code: "onda_aberta", Cause: fmt.Sprintf("a onda %d não está encerrada nem pronta para encerrar", onda), Recovery: "veja orq onda proxima " + strconv.FormatInt(onda, 10), Exit: output.ExitConflict}
				}
				if res, err = l.LimparOnda(ctx, onda, dry, nil); err != nil {
					return err
				}
			case len(args) == 1 && onda == 0:
				ref, err := refs.ParseUS(args[0])
				if err != nil {
					return usoDe(err)
				}
				coord, err := a.coordOpcional(ctx, l.St)
				if err != nil {
					return err
				}
				if res, err = l.Limpar(ctx, coord, ref, limpeza.Opcoes{DryRun: dry}); err != nil {
					return err
				}
			default:
				return usoErr("passe a ref da US ou --onda <id>, não os dois")
			}
			if err := a.emit(res, func(w io.Writer) error { return textoLimpeza(w, res) }); err != nil {
				return err
			}
			if res.Recusa != "" {
				return &output.Error{Code: "limpeza_recusada", Cause: res.Recusa, Recovery: "nada foi removido; veja orq diario --us", Exit: output.ExitConflict}
			}
			for _, i := range res.Itens {
				if i.Estado == limpeza.Falhou {
					return &output.Error{Code: "limpeza_falhou", Cause: fmt.Sprintf("%s %s: %s", i.Tipo, i.Alvo, i.Motivo), Recovery: "o resto foi feito; corrija e rode orq limpar de novo", Exit: output.ExitUnexpected}
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dry, "dry-run", false, "mostra o que seria removido, sem remover nada nem gravar no diário")
	cmd.Flags().Int64Var(&onda, "onda", 0, "limpa só o Docker com o rótulo orq.onda=<id>")
	return cmd
}

func textoLimpeza(w io.Writer, r limpeza.Resultado) error {
	modo := ""
	if r.DryRun {
		modo = " (dry-run)"
	}
	if _, err := fmt.Fprintf(w, "limpar %s%s\n", q(r.US), modo); err != nil {
		return err
	}
	if r.Recusa != "" {
		_, err := fmt.Fprintf(w, "  recusada: %s\n", q(r.Recusa))
		return err
	}
	for _, i := range r.Itens {
		motivo := ""
		if i.Motivo != "" {
			motivo = " — " + q(i.Motivo)
		}
		if _, err := fmt.Fprintf(w, "  %-10s %-15s %s%s\n", i.Estado, i.Tipo, q(i.Alvo), motivo); err != nil {
			return err
		}
	}
	if len(r.Itens) == 0 {
		_, err := fmt.Fprintln(w, "  nada a fazer")
		return err
	}
	return nil
}

// RecursosGravados é a saída de orq recursos gravar e ver.
type RecursosGravados struct {
	Arquivo  string            `json:"arquivo"`
	Recursos *limpeza.Recursos `json:"recursos"`
	// Env traz o que vai em --env no herdr tab create; Rotulos, o que vai nos rótulos Docker.
	Env     map[string]string `json:"env"`
	Rotulos map[string]string `json:"rotulos"`
	Avisos  []string          `json:"avisos"`
}

func (a *App) recursosCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "recursos", Short: "Grava e mostra os recursos que o coordenador criou para a US"}
	var repo, worktree, branch, remoto string
	var panes, aprovados []string
	var avisos []string
	gravar := &cobra.Command{
		Use:   "gravar <ref> [--repo R --worktree W --branch B] [--pane NOME]… [--aprovado URL=SHA]…",
		Short: "Grava (ou completa) o recursos.json da US na pasta de trabalho",
		Long: `Grava ~/tmp/orquestrador/<projeto>/TG-<ref>/recursos.json: os repositórios com worktree e branch
(--repo, --worktree e --branch vão juntos; o mesmo worktree de novo atualiza a entrada), os nomes
de agente dos panes (--pane, repetível) e o head aprovado de cada MR (--aprovado URL=SHA). Chamadas
seguintes completam o arquivo. A saída traz o TMPDIR do worker (--env no herdr tab create) e os
rótulos Docker (orq.us e orq.onda).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			ref, err := refs.ParseUS(args[0])
			if err != nil {
				return usoDe(err)
			}
			l, err := a.limpador(ctx)
			if err != nil {
				return err
			}
			coord, err := a.coordOpcional(ctx, l.St)
			if err != nil {
				return err
			}
			loc, _, err := l.St.LocalizarUS(ctx, coord, ref)
			if err != nil {
				return err
			}
			// A gravação espera uma limpeza em curso (e vice-versa): o arquivo não muda no meio dela.
			soltar, err := l.Raizes.TravarUS(loc.US, limpeza.EsperaTrava)
			if err != nil {
				return err
			}
			defer soltar()
			rec, err := limpeza.LerRecursos(l.Raizes.Arquivo(loc.US), loc.US)
			if errors.Is(err, limpeza.ErrSemRecursos) {
				rec, err = &limpeza.Recursos{}, nil
			}
			if err != nil {
				return &output.Error{Code: "recursos_invalido", Cause: err.Error(), Recovery: "corrija ou apague o arquivo", Exit: output.ExitConflict}
			}
			if (repo != "" || worktree != "" || branch != "") && (repo == "" || worktree == "" || branch == "") {
				return usoErr("--repo, --worktree e --branch vão juntos")
			}
			if repo != "" {
				r := limpeza.Repo{Repo: filepath.Clean(repo), Worktree: filepath.Clean(worktree), Branch: branch, Remoto: remoto}
				trocou := false
				for i := range rec.Repos {
					if rec.Repos[i].Worktree == r.Worktree {
						if rec.Repos[i].Branch != r.Branch {
							avisos = append(avisos, "o worktree "+r.Worktree+" era da branch "+rec.Repos[i].Branch+": na rodada de correção prefira um caminho novo (sufixo da rodada), para uma limpeza velha nunca apontar para o checkout novo")
						}
						rec.Repos[i], trocou = r, true
					}
				}
				if !trocou {
					rec.Repos = append(rec.Repos, r)
				}
			}
			for _, p := range panes {
				if !contemStr(rec.Panes, p) {
					rec.Panes = append(rec.Panes, p)
				}
			}
			for _, ap := range aprovados {
				url, sha, ok := strings.Cut(ap, "=")
				m, err := refs.ParseMR(url)
				if !ok || err != nil {
					return usoErr("--aprovado %q: use URL=SHA", ap)
				}
				if rec.Aprovados == nil {
					rec.Aprovados = map[string]string{}
				}
				rec.Aprovados[m.URL] = sha
			}
			arquivo, err := rec.Gravar(l.Raizes, loc.US)
			if err != nil {
				return usoDe(err)
			}
			if err := l.St.RegistrarExternos(ctx, coord, loc.US, []central.EventoExterno{{Tipo: "recursos.gravar", Detalhe: map[string]any{
				"arquivo": arquivo, "repos": len(rec.Repos), "panes": len(rec.Panes), "aprovados": len(rec.Aprovados)}}}); err != nil {
				return err
			}
			return a.emitRecursos(l, loc, rec, arquivo, avisos...)
		},
	}
	gravar.Flags().StringVar(&repo, "repo", "", "checkout principal do repositório")
	gravar.Flags().StringVar(&worktree, "worktree", "", "worktree da US nesse repositório")
	gravar.Flags().StringVar(&branch, "branch", "", "branch da US")
	gravar.Flags().StringVar(&remoto, "remoto", "", "remoto git (padrão origin)")
	gravar.Flags().StringSliceVar(&panes, "pane", nil, "nome de agente de um pane da US (repetível)")
	gravar.Flags().StringSliceVar(&aprovados, "aprovado", nil, "head aprovado de uma MR, URL=SHA (repetível)")
	ver := &cobra.Command{
		Use:   "ver <ref>",
		Short: "Mostra o recursos.json da US",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			ref, err := refs.ParseUS(args[0])
			if err != nil {
				return usoDe(err)
			}
			l, err := a.limpador(ctx)
			if err != nil {
				return err
			}
			coord, err := a.coordOpcional(ctx, l.St)
			if err != nil {
				return err
			}
			loc, _, err := l.St.LocalizarUS(ctx, coord, ref)
			if err != nil {
				return err
			}
			arquivo := l.Raizes.Arquivo(loc.US)
			rec, err := limpeza.LerRecursos(arquivo, loc.US)
			if errors.Is(err, limpeza.ErrSemRecursos) {
				return &output.Error{Code: "nao_encontrado", Cause: "sem recursos gravados em " + arquivo, Recovery: "grave com orq recursos gravar " + args[0], Exit: output.ExitNotFound}
			}
			if err != nil {
				return &output.Error{Code: "recursos_invalido", Cause: err.Error(), Exit: output.ExitConflict}
			}
			return a.emitRecursos(l, loc, rec, arquivo)
		},
	}
	reabrir := &cobra.Command{
		Use:   "reabrir <ref>",
		Short: "Abre uma rodada de correção: sobe a geração do recursos.json antes de recriar o worktree",
		Long: `Para corrigir depois do teste, rode isto ANTES de recriar o worktree e os panes. Sob a trava da US
(espera uma limpeza em andamento terminar), sobe a geração do recursos.json e guarda as MRs de agora
como encerradas: até haver uma MR nova ligada (orq mr add), o orq limpar recusa. Depois recrie o
worktree a partir da main, numa branch nova e de preferência num caminho novo (sufixo da rodada), e
grave-o com orq recursos gravar.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			ref, err := refs.ParseUS(args[0])
			if err != nil {
				return usoDe(err)
			}
			l, err := a.limpador(ctx)
			if err != nil {
				return err
			}
			coord, err := a.coordOpcional(ctx, l.St)
			if err != nil {
				return err
			}
			rec, arquivo, err := l.Reabrir(ctx, coord, ref)
			if err != nil {
				return err
			}
			loc, _, err := l.St.LocalizarUS(ctx, coord, ref)
			if err != nil {
				return err
			}
			return a.emitRecursos(l, loc, rec, arquivo)
		},
	}
	cmd.AddCommand(gravar, ver, reabrir)
	return cmd
}

func (a *App) emitRecursos(l *limpeza.Limpador, loc central.Localizacao, rec *limpeza.Recursos, arquivo string, extras ...string) error {
	out := RecursosGravados{
		Arquivo: arquivo, Recursos: rec,
		Env:     map[string]string{"TMPDIR": l.Raizes.PastaTmp(loc.US)},
		Rotulos: map[string]string{limpeza.RotuloChaveUS: limpeza.RotuloUS(loc.US), limpeza.RotuloChaveOnda: strconv.FormatInt(loc.OndaID, 10)},
		Avisos:  append(avisosRyuk(a.Env), extras...),
	}
	return a.emit(out, func(w io.Writer) error {
		fmt.Fprintf(w, "recursos de %s em %s\n", q(loc.US.Chave()), q(arquivo))
		for _, r := range rec.Repos {
			fmt.Fprintf(w, "  repo %s  worktree %s  branch %s\n", q(r.Repo), q(r.Worktree), q(r.Branch))
		}
		fmt.Fprintf(w, "  panes: %s\n", q(strings.Join(rec.Panes, ", ")))
		fmt.Fprintf(w, "  TMPDIR=%s\n  rótulos Docker: %s=%s  %s=%s\n", q(out.Env["TMPDIR"]), limpeza.RotuloChaveUS, out.Rotulos[limpeza.RotuloChaveUS], limpeza.RotuloChaveOnda, out.Rotulos[limpeza.RotuloChaveOnda])
		for _, av := range out.Avisos {
			fmt.Fprintf(w, "  aviso: %s\n", q(av))
		}
		return nil
	})
}

// avisosRyuk avisa quando o Testcontainers está com o Ryuk (o coletor que remove os contêineres
// dele) desligado: sem ele, quem limpa é o rótulo, e só no merge.
func avisosRyuk(env func(string) string) []string {
	var av []string
	if strings.EqualFold(env("TESTCONTAINERS_RYUK_DISABLED"), "true") {
		av = append(av, "TESTCONTAINERS_RYUK_DISABLED=true: o Testcontainers não vai remover os contêineres dele sozinho")
	}
	home := env("HOME")
	if home == "" {
		return av
	}
	b, err := os.ReadFile(filepath.Join(home, ".testcontainers.properties"))
	if err != nil {
		return av
	}
	for _, linha := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(linha), "=")
		if ok && strings.TrimSpace(k) == "ryuk.disabled" && strings.EqualFold(strings.TrimSpace(v), "true") {
			av = append(av, "~/.testcontainers.properties tem ryuk.disabled=true: o Testcontainers não vai remover os contêineres dele sozinho")
		}
	}
	return av
}

func contemStr(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
