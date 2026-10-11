package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/tui"
	"github.com/spf13/cobra"
)

func (a *App) tuiCmd() *cobra.Command {
	var todos bool
	var intervalo time.Duration
	cmd := &cobra.Command{
		Use:   "tui [--coord X | --todos]",
		Short: "Mostra a Central no terminal: ondas, US, MRs e pendências, com foco nos panes",
		Long: `Desenha a Central a partir do mesmo snapshot de orq snapshot, relido a cada --intervalo
(o cache vem do orq daemon; o TUI não consulta as fontes). Com --coord, as ondas abertas de um
coordenador; com --todos, um bloco por coordenador e o total de pendências abertas. Sem nenhum
dos dois, o coordenador corrente ($ORQ_COORD ou o pane registrado) ou, sem ele, todos.

↑/↓ (ou j/k) escolhem a linha, Enter ou o clique focam o pane da US (herdr agent focus), r relê,
q sai. O #ref é link (OSC 8) para a story no Taiga, a MR para a forja, e o status e o ↗ para
https://orq.local/pane/<id>, que o link handler do plugin transforma em foco.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if todos && a.flagCoord != "" {
				return usoErr("--todos e --coord não combinam: --todos mostra todos os coordenadores")
			}
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			o := tui.Opcoes{Todos: todos}
			if !todos {
				c, err := a.coordOpcional(ctx, st)
				if err != nil {
					return err
				}
				if c != nil {
					o.Coord = c.Nome
				}
			}
			f := enriquecer.Ferramentas{Env: a.Env, Timeout: 10 * time.Second}
			m := tui.Novo(ctx, tui.Config{
				Carregar:    func(ctx context.Context) (central.Snapshot, error) { return st.Snapshot(ctx, false) },
				Focar:       func(ctx context.Context, pane string) error { return focarPane(ctx, f, pane) },
				Opcoes:      o,
				Intervalo:   intervalo,
				SairAoFocar: todos,
			})
			p := tea.NewProgram(m, tea.WithInput(a.In), tea.WithOutput(a.Out), tea.WithContext(ctx))
			_, err = p.Run()
			if err != nil && ctx.Err() == nil {
				return &output.Error{Code: "tui_falhou", Cause: err.Error(), Recovery: "rode num terminal (o TUI precisa de um tty); o estado continua em `orq snapshot`", Exit: output.ExitUnexpected}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&todos, "todos", false, "um bloco por coordenador (o popup do plugin)")
	cmd.Flags().DurationVar(&intervalo, "intervalo", 2*time.Second, "intervalo entre as releituras do snapshot")
	return cmd
}

// focarPane chama `herdr agent focus <pane>`, por caminho absoluto.
func focarPane(ctx context.Context, f enriquecer.Ferramentas, pane string) error {
	_, err := f.Rodar(ctx, "herdr", "agent", "focus", pane)
	return err
}

// idPane casa o id público de pane do Herdr (w2A:p9) e os nomes de agente que agent focus
// também aceita; nada que pareça flag ou carregue espaço.
var idPane = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_:.-]{0,63}$`)

// PaneDaURL extrai o pane de https://orq.local/pane/<id>. Outra URL, id vazio ou com caractere
// estranho é erro de uso.
func PaneDaURL(u string) (string, error) {
	resto, ok := strings.CutPrefix(u, tui.URLPane)
	if !ok {
		return "", usoErr("URL %s não é de pane do orq (%s<id>)", q(u), tui.URLPane)
	}
	resto, _, _ = strings.Cut(resto, "?")
	resto, _, _ = strings.Cut(resto, "#")
	resto = strings.TrimSuffix(resto, "/")
	id, err := url.PathUnescape(resto)
	if err != nil || !idPane.MatchString(id) {
		return "", usoErr("pane inválido na URL %s", q(u))
	}
	return id, nil
}

func (a *App) focarCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "focar [<pane>|<url>]",
		Short: "Foca o pane de um agente (herdr agent focus); a ação focar-pane do plugin",
		Long: `Foca o pane com herdr agent focus. Aceita o id do pane ou a URL https://orq.local/pane/<id>;
sem argumento, lê $HERDR_PLUGIN_CLICKED_URL (ou o clicked_url de $HERDR_PLUGIN_CONTEXT_JSON),
que o Herdr preenche quando o link handler do plugin chama a ação focar-pane.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alvo := a.Env("HERDR_PLUGIN_CLICKED_URL")
			if alvo == "" {
				var ctx struct {
					ClickedURL string `json:"clicked_url"`
				}
				if json.Unmarshal([]byte(a.Env("HERDR_PLUGIN_CONTEXT_JSON")), &ctx) == nil {
					alvo = ctx.ClickedURL
				}
			}
			if len(args) == 1 {
				alvo = args[0]
			}
			if alvo == "" {
				return usoErr("diga o pane (ou a URL https://orq.local/pane/<id>); sem argumento, o orq lê $HERDR_PLUGIN_CLICKED_URL ou o clicked_url de $HERDR_PLUGIN_CONTEXT_JSON, e os dois estão vazios")
			}
			pane := alvo
			if strings.Contains(alvo, "://") {
				var err error
				if pane, err = PaneDaURL(alvo); err != nil {
					return err
				}
			} else if !idPane.MatchString(alvo) {
				return usoErr("pane inválido: %s", q(alvo))
			}
			if err := focarPane(cmd.Context(), enriquecer.Ferramentas{Env: a.Env, Timeout: 10 * time.Second}, pane); err != nil {
				return &output.Error{Code: "foco_falhou", Cause: err.Error(), Recovery: "confira o pane com `herdr agent get " + pane + "`: agent focus só foca pane com agente", Exit: output.ExitUnexpected}
			}
			return a.emit(map[string]string{"pane": pane}, func(w io.Writer) error {
				_, err := io.WriteString(w, "foco em "+q(pane)+"\n")
				return err
			})
		},
	}
}
