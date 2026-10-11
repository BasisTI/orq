package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/output"
	"github.com/spf13/cobra"
)

// RotuloPaneCentral prefixa o rótulo dos panes que orq central abrir cria: é por ele que a
// segunda chamada reconhece os panes que já existem.
const RotuloPaneCentral = "orq · "

// PaneCentral é o que orq central abrir fez com o pane de um coordenador.
type PaneCentral struct {
	Coordenador string `json:"coordenador"`
	PaneID      string `json:"pane_id"`
	Acao        string `json:"acao"` // criado, mantido, fechado
}

// ResultadoCentral é a saída de orq central abrir.
type ResultadoCentral struct {
	Workspace   string        `json:"workspace"`
	WorkspaceID *string       `json:"workspace_id"` // nulo quando não há coordenador com onda aberta nem workspace
	Criado      bool          `json:"criado"`
	Focado      bool          `json:"focado"`
	Panes       []PaneCentral `json:"panes"`
}

func (a *App) centralCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "central", Short: "Workspace Central do Herdr: um pane orq tui por coordenador"}
	var nome string
	var semFoco bool
	abrir := &cobra.Command{
		Use:   "abrir [--nome Central] [--sem-foco]",
		Short: "Cria ou reaproveita o workspace Central com um pane orq tui --coord X por coordenador com onda aberta",
		Long: `Cria o workspace (pelo rótulo, --nome) se ele não existe e deixa nele um pane rodando
orq tui --coord X para cada coordenador com onda aberta. Idempotente: os panes são reconhecidos
pelo rótulo "orq · <coordenador>"; o que já existe fica, o que falta é criado, e o pane de um
coordenador que não tem mais onda aberta é fechado (ele não teria o que mostrar; a próxima
chamada o recria quando houver onda). Panes sem esse rótulo no workspace não são tocados.
Sem --sem-foco, foca o workspace no fim. Sem --nome, vale $ORQ_CENTRAL_NOME (que o orq.env do
plugin pode definir) e, sem ela, Central.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("nome") {
				if v := a.Env("ORQ_CENTRAL_NOME"); v != "" {
					nome = v
				}
			}
			if strings.TrimSpace(nome) == "" || strings.ContainsFunc(nome, output.UnsafeRune) {
				return usoErr("--nome vazio ou com caractere de controle")
			}
			r, err := a.abrirCentral(cmd.Context(), nome, !semFoco)
			if err != nil {
				return err
			}
			return a.emit(r, func(w io.Writer) error { return textoCentral(w, r) })
		},
	}
	abrir.Flags().StringVar(&nome, "nome", "Central", "rótulo do workspace (padrão: $ORQ_CENTRAL_NOME ou Central)")
	abrir.Flags().BoolVar(&semFoco, "sem-foco", false, "não foca o workspace no fim")
	cmd.AddCommand(abrir)
	return cmd
}

func textoCentral(w io.Writer, r ResultadoCentral) error {
	ws := "-"
	if r.WorkspaceID != nil {
		ws = *r.WorkspaceID
	}
	estado := "reaproveitado"
	if r.Criado {
		estado = "criado"
	}
	if _, err := fmt.Fprintf(w, "workspace %s (%s) %s\n", q(r.Workspace), ws, estado); err != nil {
		return err
	}
	if len(r.Panes) == 0 {
		_, err := fmt.Fprintln(w, "nenhum coordenador com onda aberta")
		return err
	}
	for _, p := range r.Panes {
		if _, err := fmt.Fprintf(w, "  %-8s %s  %s\n", p.Acao, p.PaneID, q(p.Coordenador)); err != nil {
			return err
		}
	}
	return nil
}

// herdrJSON roda o herdr e decodifica o result da resposta.
func herdrJSON(ctx context.Context, f enriquecer.Ferramentas, v any, args ...string) error {
	out, err := f.Rodar(ctx, "herdr", args...)
	if err != nil {
		return herdrErro(args, err)
	}
	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return herdrErro(args, fmt.Errorf("resposta inválida: %v", err))
	}
	if r.Error != nil {
		return herdrErro(args, fmt.Errorf("%s: %s", r.Error.Code, r.Error.Message))
	}
	if v == nil {
		return nil
	}
	if err := json.Unmarshal(r.Result, v); err != nil {
		return herdrErro(args, fmt.Errorf("resposta inválida: %v", err))
	}
	return nil
}

func herdrErro(args []string, err error) error {
	return &output.Error{Code: "herdr_falhou", Cause: "herdr " + strings.Join(args, " ") + ": " + err.Error(),
		Recovery: "confira se o servidor do Herdr está no ar (herdr status) e rode de novo; o que já foi feito fica e a próxima chamada continua dali", Exit: output.ExitUnexpected}
}

// comandoShell monta a linha que o shell do pane roda: argumentos entre aspas simples.
func comandoShell(args ...string) string {
	var ps []string
	for _, a := range args {
		ps = append(ps, "'"+strings.ReplaceAll(a, "'", `'\''`)+"'")
	}
	return strings.Join(ps, " ")
}

func (a *App) abrirCentral(ctx context.Context, nome string, focar bool) (ResultadoCentral, error) {
	r := ResultadoCentral{Workspace: nome, Panes: []PaneCentral{}}
	st, err := a.store(ctx)
	if err != nil {
		return r, err
	}
	snap, err := st.Snapshot(ctx, false)
	if err != nil {
		return r, err
	}
	var coords []string
	for _, c := range snap.Coordenadores {
		if len(c.Ondas) > 0 {
			coords = append(coords, c.Nome)
		}
	}
	dir, err := central.DirPadrao(a.Env)
	if err != nil {
		return r, err
	}
	exe, err := os.Executable()
	if err != nil {
		return r, err
	}
	f := enriquecer.Ferramentas{Env: a.Env, Timeout: 15 * time.Second}
	herdr, err := f.Caminho("herdr")
	if err != nil {
		return r, herdrErro([]string{}, err)
	}
	// O pane herda o estado e as ferramentas desta execução: o TUI tem de ler o mesmo banco.
	envs := []string{"ORQ_STATE_DIR=" + dir, "ORQ_HERDR_BIN=" + herdr}
	for _, k := range []string{"ORQ_GLAB_BIN", "ORQ_GH_BIN", "ORQ_TAIGA_BIN"} {
		if v := a.Env(k); v != "" {
			envs = append(envs, k+"="+v)
		}
	}
	var flagsEnv []string
	for _, e := range envs {
		flagsEnv = append(flagsEnv, "--env", e)
	}

	var wl struct {
		Workspaces []struct {
			ID    string `json:"workspace_id"`
			Label string `json:"label"`
		} `json:"workspaces"`
	}
	if err := herdrJSON(ctx, f, &wl, "workspace", "list"); err != nil {
		return r, err
	}
	var ws string
	for _, w := range wl.Workspaces {
		if w.Label == nome {
			ws = w.ID
			break
		}
	}
	livre := "" // pane de shell recém-criado com o workspace, ainda sem coordenador
	if ws == "" {
		if len(coords) == 0 {
			return r, nil
		}
		var wc struct {
			Workspace struct {
				ID string `json:"workspace_id"`
			} `json:"workspace"`
			RootPane struct {
				ID string `json:"pane_id"`
			} `json:"root_pane"`
		}
		args := append([]string{"workspace", "create", "--label", nome, "--no-focus"}, flagsEnv...)
		if home := a.Env("HOME"); home != "" {
			args = append(args, "--cwd", home)
		}
		if err := herdrJSON(ctx, f, &wc, args...); err != nil {
			return r, err
		}
		ws, livre, r.Criado = wc.Workspace.ID, wc.RootPane.ID, true
	}
	r.WorkspaceID = &ws

	var pl struct {
		Panes []struct {
			ID    string `json:"pane_id"`
			Label string `json:"label"`
		} `json:"panes"`
	}
	if err := herdrJSON(ctx, f, &pl, "pane", "list", "--workspace", ws); err != nil {
		return r, err
	}
	existentes := map[string]string{} // coordenador → pane
	ultimo := ""
	for _, p := range pl.Panes {
		if c, ok := strings.CutPrefix(p.Label, RotuloPaneCentral); ok {
			if _, dup := existentes[c]; !dup {
				existentes[c] = p.ID
			}
			ultimo = p.ID
		} else if ultimo == "" && p.ID != livre {
			ultimo = p.ID
		}
	}
	if ultimo == "" {
		ultimo = livre
	}
	quer := map[string]bool{}
	for _, c := range coords {
		quer[c] = true
		if p, ok := existentes[c]; ok {
			r.Panes = append(r.Panes, PaneCentral{c, p, "mantido"})
			continue
		}
		pane := livre
		if pane != "" {
			livre = ""
		} else {
			var sp struct {
				Pane struct {
					ID string `json:"pane_id"`
				} `json:"pane"`
			}
			args := append([]string{"pane", "split", ultimo, "--direction", "down", "--no-focus"}, flagsEnv...)
			if err := herdrJSON(ctx, f, &sp, args...); err != nil {
				return r, err
			}
			pane = sp.Pane.ID
		}
		if err := herdrJSON(ctx, f, nil, "pane", "rename", pane, RotuloPaneCentral+c); err != nil {
			return r, err
		}
		if err := herdrJSON(ctx, f, nil, "pane", "run", pane, comandoShell(exe, "tui", "--coord", c)); err != nil {
			return r, err
		}
		ultimo = pane
		r.Panes = append(r.Panes, PaneCentral{c, pane, "criado"})
	}
	var sobras []string
	for c := range existentes {
		if !quer[c] {
			sobras = append(sobras, c)
		}
	}
	sort.Strings(sobras)
	for _, c := range sobras {
		if err := herdrJSON(ctx, f, nil, "pane", "close", existentes[c]); err != nil {
			return r, err
		}
		r.Panes = append(r.Panes, PaneCentral{c, existentes[c], "fechado"})
	}
	if focar && len(coords) > 0 {
		if err := herdrJSON(ctx, f, nil, "workspace", "focus", ws); err != nil {
			return r, err
		}
		r.Focado = true
	}
	return r, nil
}
