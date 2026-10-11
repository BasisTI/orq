// Package cli monta a árvore de comandos do orq.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/output"
	"github.com/spf13/cobra"
)

// Version é definida no build com -ldflags "-X github.com/BasisTI/orq/internal/cli.Version=...".
var Version = "dev"

// App carrega a E/S e o ambiente do processo, para os comandos serem testáveis.
type App struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	Env    func(string) string
	OutTTY bool

	ran       bool
	output    string
	flagCoord string
	st        *central.Store
}

var errUsage = errors.New("uso incorreto")

func (a *App) root() *cobra.Command {
	root := &cobra.Command{
		Use:           "orq",
		Short:         "Central de US do orquestrador: coordenadores, ondas, US, MRs e pendências",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// Flags lidas: daqui em diante o erro vem do comando, não do cobra.
			a.ran = true
			_, err := output.DetectMode(a.output, a.OutTTY)
			return err
		},
	}
	root.PersistentFlags().StringVar(&a.output, "output", "", "formato da saída: json ou text (padrão: text num terminal, json fora dele)")
	root.PersistentFlags().StringVar(&a.flagCoord, "coord", "", "coordenador corrente (padrão: $ORQ_COORD ou o registrado em $HERDR_PANE_ID)")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return fmt.Errorf("%w: %v", errUsage, err)
	})
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Mostra a versão do orq",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			v := currentVersion()
			return a.emit(map[string]string{"versao": v}, func(w io.Writer) error {
				_, err := fmt.Fprintln(w, "orq "+v)
				return err
			})
		},
	})
	root.AddCommand(a.coordCmd(), a.ondaCmd(), a.usCmd(), a.mrCmd(), a.pendCmd(), a.snapshotCmd(), a.diarioCmd(), a.atualizarCmd(), a.daemonCmd(), a.tuiCmd(), a.focarCmd(), a.centralCmd(), a.pluginCmd())
	return root
}

// Main roda a CLI e devolve o código de saída.
func Main(args []string, in io.Reader, out, errOut io.Writer, env func(string) string, outTTY bool) int {
	a := &App{In: in, Out: out, Err: errOut, Env: comAmbientePlugin(env), OutTTY: outTTY}
	return a.Run(args)
}

// Run executa args e devolve o código de saída.
func (a *App) Run(args []string) int {
	root := a.root()
	root.SetArgs(args)
	root.SetIn(a.In)
	root.SetOut(a.Out)
	root.SetErr(a.Err)
	err := root.Execute()
	if a.st != nil {
		_ = a.st.Close()
	}
	if err == nil {
		return output.ExitOK
	}
	return a.finish(err)
}

// finish escreve err no stderr e devolve o código de saída, nunca 0.
func (a *App) finish(err error) int {
	mode, merr := output.DetectMode(a.output, a.OutTTY)
	if merr != nil {
		mode = output.Text
	}
	var e *output.Error
	switch {
	case errors.Is(err, errUsage) || !a.ran:
		e = &output.Error{Code: "uso", Cause: err.Error(), Recovery: "rode `orq --help`", Exit: output.ExitUsage}
	default:
		e = output.AsError(err)
	}
	if e.Exit == output.ExitOK {
		e.Exit = output.ExitUnexpected
	}
	_ = output.WriteError(a.Err, mode, e)
	return e.Exit
}

func (a *App) mode() output.Mode {
	m, _ := output.DetectMode(a.output, a.OutTTY)
	return m
}

// emit escreve v em JSON ou chama text, conforme o formato.
func (a *App) emit(v any, text func(w io.Writer) error) error {
	if a.mode() == output.JSON {
		return output.WriteJSON(a.Out, v)
	}
	return text(a.Out)
}

// store abre o banco uma vez por execução.
func (a *App) store(ctx context.Context) (*central.Store, error) {
	if a.st != nil {
		return a.st, nil
	}
	dir, err := central.DirPadrao(a.Env)
	if err != nil {
		return nil, err
	}
	st, err := central.Open(ctx, dir)
	if err != nil {
		return nil, err
	}
	a.st = st
	return st, nil
}

func (a *App) resolucao() central.Resolucao {
	return central.Resolucao{Flag: a.flagCoord, EnvCoord: a.Env("ORQ_COORD"), EnvPane: a.Env("HERDR_PANE_ID")}
}

// coordAtual exige o coordenador corrente.
func (a *App) coordAtual(ctx context.Context, st *central.Store) (central.Coordenador, error) {
	return st.ResolverCoord(ctx, a.resolucao())
}

// coordOpcional resolve o coordenador quando dá. Nome pedido (--coord ou $ORQ_COORD) que não
// existe é erro; pane sem coordenador registrado só quer dizer que não há coordenador corrente.
func (a *App) coordOpcional(ctx context.Context, st *central.Store) (*central.Coordenador, error) {
	r := a.resolucao()
	c, err := st.ResolverCoord(ctx, r)
	if err == nil {
		return &c, nil
	}
	if r.Flag == "" && r.EnvCoord == "" && output.AsError(err).Code == "coord_nao_resolvido" {
		return nil, nil
	}
	return nil, err
}

func usoErr(format string, args ...any) error {
	return &output.Error{Code: "uso", Cause: fmt.Sprintf(format, args...), Exit: output.ExitUsage}
}

func usoDe(err error) error {
	return &output.Error{Code: "uso", Cause: err.Error(), Exit: output.ExitUsage}
}

// moduleVersion casa a versão de módulo que `go install …@versão` grava.
var moduleVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// currentVersion prefere o valor do ldflags; enquanto ele for "dev", usa a versão do módulo
// principal gravada no binário, sem o v inicial.
func currentVersion() string {
	info, ok := debug.ReadBuildInfo()
	if Version != "dev" || !ok || info == nil || !moduleVersion.MatchString(info.Main.Version) {
		return Version
	}
	return strings.TrimPrefix(info.Main.Version, "v")
}
