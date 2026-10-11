package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
	"github.com/spf13/cobra"
)

func (a *App) coordCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "coord", Short: "Registra e lista coordenadores"}
	var pane, workspace, projeto string
	registrar := &cobra.Command{
		Use:   "registrar <nome>",
		Short: "Registra o coordenador (o pane padrão é $HERDR_PANE_ID)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("pane") {
				pane = a.Env("HERDR_PANE_ID")
			}
			c, err := st.RegistrarCoord(ctx, central.Registro{Nome: args[0], Pane: pane, Workspace: workspace, Projeto: projeto})
			if err != nil {
				return err
			}
			return a.emit(c, func(w io.Writer) error { return textoCoord(w, c) })
		},
	}
	registrar.Flags().StringVar(&pane, "pane", "", "pane do Herdr do coordenador (padrão: $HERDR_PANE_ID)")
	registrar.Flags().StringVar(&workspace, "workspace", "", "workspace do Herdr")
	registrar.Flags().StringVar(&projeto, "projeto", "", "slug do projeto Taiga padrão, usado em TG-<n> sem projeto")
	list := &cobra.Command{
		Use:   "list",
		Short: "Lista os coordenadores",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			cs, err := st.ListarCoords(ctx)
			if err != nil {
				return err
			}
			return a.emit(cs, func(w io.Writer) error {
				for _, c := range cs {
					if err := textoCoord(w, c); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
	cmd.AddCommand(registrar, list)
	return cmd
}

func (a *App) ondaCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "onda", Short: "Cria, consulta e encerra ondas de US"}

	var projeto, us, ate, ordem string
	var depende []string
	criar := &cobra.Command{
		Use:   "criar <titulo> --us <refs>",
		Short: "Cria uma onda com as US em wait, na ordem dada",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			coord, err := a.coordAtual(ctx, st)
			if err != nil {
				return err
			}
			n := central.NovaOnda{Titulo: args[0], Projeto: projeto, Ate: ate, Ordem: ordem}
			for _, s := range strings.Split(us, ",") {
				if s = strings.TrimSpace(s); s == "" {
					continue
				}
				u, err := refs.ParseUS(s)
				if err != nil {
					return usoDe(err)
				}
				n.US = append(n.US, u)
			}
			for _, d := range depende {
				de, para, ok := strings.Cut(d, ":")
				if !ok {
					return usoErr("--depende %q inválido: use <dependente>:<dependência>, ex. 216:215", d)
				}
				for _, p := range strings.Split(para, "+") {
					u1, err := refs.ParseUS(de)
					if err != nil {
						return usoDe(err)
					}
					u2, err := refs.ParseUS(p)
					if err != nil {
						return usoDe(err)
					}
					n.Deps = append(n.Deps, [2]refs.US{u1, u2})
				}
			}
			o, err := st.CriarOnda(ctx, coord, n)
			if err != nil {
				return err
			}
			return a.emit(o, func(w io.Writer) error { return textoOnda(w, o) })
		},
	}
	criar.Flags().StringVar(&projeto, "projeto", "", "slug do projeto Taiga das US sem projeto (padrão: o do coordenador)")
	criar.Flags().StringVar(&us, "us", "", "US da onda, na ordem: 215,216 ou projeto/215")
	criar.Flags().StringVar(&ate, "ate", "mr-pronta", "até onde a onda está autorizada: plano, mr-pronta ou merge")
	criar.Flags().StringVar(&ordem, "ordem", "livre", "estrita (uma de cada vez, na ordem) ou livre")
	criar.Flags().StringSliceVar(&depende, "depende", nil, "dependências <dependente>:<dependência>, ex. 216:215 ou 218:216+217 (repetível)")

	ver := &cobra.Command{
		Use:   "ver [<id>]",
		Short: "Mostra a onda (sem id, a única onda aberta do coordenador)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o, err := a.ondaArg(cmd, args)
			if err != nil {
				return err
			}
			return a.emit(o, func(w io.Writer) error { return textoOnda(w, o) })
		},
	}

	var abertas bool
	list := &cobra.Command{
		Use:   "list",
		Short: "Lista as ondas (com --coord, só as do coordenador)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			os, err := st.ListarOndas(ctx, central.FiltroOndas{Coord: a.flagCoord, Abertas: abertas})
			if err != nil {
				return err
			}
			return a.emit(os, func(w io.Writer) error {
				for _, o := range os {
					if err := textoOndaResumo(w, o); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
	list.Flags().BoolVar(&abertas, "abertas", false, "só as ondas não encerradas")

	proxima := &cobra.Command{
		Use:   "proxima [<id>]",
		Short: "Próxima US em wait que a ordem, as dependências e a autorização permitem",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			o, err := a.ondaArg(cmd, args)
			if err != nil {
				return err
			}
			p, err := a.st.ProximaUS(ctx, o.ID)
			if err != nil {
				return err
			}
			return a.emit(p, func(w io.Writer) error { return textoProxima(w, p) })
		},
	}

	encerrar := &cobra.Command{
		Use:   "encerrar <id>",
		Short: "Encerra a onda; as US ficam como estão",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			id, err := ondaID(args[0])
			if err != nil {
				return err
			}
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			coord, err := a.coordOpcional(ctx, st)
			if err != nil {
				return err
			}
			nome := ""
			if coord != nil {
				nome = coord.Nome
			}
			o, err := st.EncerrarOnda(ctx, nome, id)
			if err != nil {
				return err
			}
			return a.emit(o, func(w io.Writer) error { return textoOndaResumo(w, o) })
		},
	}
	cmd.AddCommand(criar, ver, list, proxima, encerrar)
	return cmd
}

// ondaArg abre a onda do argumento ou, sem ele, a única onda aberta do coordenador corrente.
func (a *App) ondaArg(cmd *cobra.Command, args []string) (central.Onda, error) {
	ctx := cmd.Context()
	st, err := a.store(ctx)
	if err != nil {
		return central.Onda{}, err
	}
	if len(args) == 1 {
		id, err := ondaID(args[0])
		if err != nil {
			return central.Onda{}, err
		}
		return st.VerOnda(ctx, id)
	}
	coord, err := a.coordAtual(ctx, st)
	if err != nil {
		return central.Onda{}, err
	}
	return st.OndaAberta(ctx, coord.Nome)
}

func ondaID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, usoErr("id de onda %q inválido", s)
	}
	return id, nil
}

func (a *App) usCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "us", Short: "Atualiza o status das US"}
	var pane, papel string
	var rodada int
	var forcar bool
	status := &cobra.Command{
		Use:   "status <ref> <wait|plan|exec|rev|done>",
		Short: "Muda o status da US (sair de done exige --forcar)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			ref, err := refs.ParseUS(args[0])
			if err != nil {
				return usoDe(err)
			}
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			coord, err := a.coordOpcional(ctx, st)
			if err != nil {
				return err
			}
			m := central.MudancaStatus{Status: args[1], Forcar: forcar}
			if cmd.Flags().Changed("pane") {
				m.Pane = &pane
			}
			if cmd.Flags().Changed("papel") {
				m.Papel = &papel
			}
			if cmd.Flags().Changed("rodada") {
				m.Rodada = &rodada
			}
			u, alterada, err := st.AtualizarStatus(ctx, coord, ref, m)
			if err != nil {
				return err
			}
			res := struct {
				Alterada bool       `json:"alterada"`
				US       central.US `json:"us"`
			}{alterada, u}
			return a.emit(res, func(w io.Writer) error {
				sufixo := ""
				if !alterada {
					sufixo = " (sem mudança)"
				}
				_, err := fmt.Fprintf(w, "%s %s%s\n", u.Chave, linhaStatus(u), sufixo)
				return err
			})
		},
	}
	status.Flags().StringVar(&pane, "pane", "", "pane do Herdr que conduz a US agora")
	status.Flags().StringVar(&papel, "papel", "", "papel do pane: planejamento, implementacao ou revisao")
	status.Flags().IntVar(&rodada, "rodada", 0, "rodada corrente (1, 2…)")
	status.Flags().BoolVar(&forcar, "forcar", false, "permite sair de done")
	cmd.AddCommand(status)
	return cmd
}

func (a *App) mrCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "mr", Short: "Liga e desliga merge requests das US"}
	add := &cobra.Command{
		Use:   "add <ref> <url>",
		Short: "Liga a MR (GitLab) ou PR (GitHub) à US",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, st, coord, ref, err := a.preparar(cmd, args[0])
			if err != nil {
				return err
			}
			m, criada, err := st.AdicionarMR(ctx, coord, ref, args[1])
			if err != nil {
				return err
			}
			res := struct {
				Criada bool       `json:"criada"`
				MR     central.MR `json:"mr"`
			}{criada, m}
			return a.emit(res, func(w io.Writer) error {
				sufixo := ""
				if !criada {
					sufixo = " (já estava ligada)"
				}
				_, err := fmt.Fprintf(w, "%s%s%s\n", m.URL, iacTexto(m.IaC), sufixo)
				return err
			})
		},
	}
	rm := &cobra.Command{
		Use:   "rm <ref> <url>",
		Short: "Desliga a MR da US",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, st, coord, ref, err := a.preparar(cmd, args[0])
			if err != nil {
				return err
			}
			m, err := st.RemoverMR(ctx, coord, ref, args[1])
			if err != nil {
				return err
			}
			return a.emit(m, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "removida %s\n", m.URL)
				return err
			})
		},
	}
	cmd.AddCommand(add, rm)
	return cmd
}

func (a *App) pendCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "pend", Short: "Abre, resolve e lista pendências (P1, P2… por US)"}
	var tipo, pane string
	add := &cobra.Command{
		Use:   `add <ref> --tipo <duvida|autorizacao|decisao> "<texto>"`,
		Short: "Abre uma pendência e imprime o id (TG-312#P2)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, st, coord, ref, err := a.preparar(cmd, args[0])
			if err != nil {
				return err
			}
			p, err := st.AbrirPendencia(ctx, coord, ref, tipo, args[1], pane)
			if err != nil {
				return err
			}
			return a.emit(p, func(w io.Writer) error {
				_, err := fmt.Fprintln(w, p.ID)
				return err
			})
		},
	}
	add.Flags().StringVar(&tipo, "tipo", "", "duvida, autorizacao ou decisao")
	add.Flags().StringVar(&pane, "pane", "", "pane do Herdr que abriu a pendência")

	var resposta string
	resolver := &cobra.Command{
		Use:   "resolver <ref>#P<n>",
		Short: "Resolve a pendência",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			p, err := refs.ParsePend(args[0])
			if err != nil {
				return usoDe(err)
			}
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			coord, err := a.coordOpcional(ctx, st)
			if err != nil {
				return err
			}
			r, err := st.ResolverPendencia(ctx, coord, p, resposta)
			if err != nil {
				return err
			}
			return a.emit(r, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "%s resolvida\n", r.ID)
				return err
			})
		},
	}
	resolver.Flags().StringVar(&resposta, "resposta", "", "resposta dada")

	var abertas bool
	var onda int64
	list := &cobra.Command{
		Use:   "list",
		Short: "Lista as pendências (--coord filtra pelo dono da onda)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			ps, err := st.ListarPendencias(ctx, central.FiltroPendencias{Abertas: abertas, Coord: a.flagCoord, OndaID: onda})
			if err != nil {
				return err
			}
			return a.emit(ps, func(w io.Writer) error {
				for _, p := range ps {
					if err := textoPendencia(w, p, ""); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
	list.Flags().BoolVar(&abertas, "abertas", false, "só as pendências abertas")
	list.Flags().Int64Var(&onda, "onda", 0, "só as pendências das US desta onda")
	cmd.AddCommand(add, resolver, list)
	return cmd
}

func (a *App) snapshotCmd() *cobra.Command {
	var encerradas bool
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Estado consolidado em JSON versionado (docs/snapshot.md), sempre em JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			s, err := st.Snapshot(ctx, encerradas)
			if err != nil {
				return err
			}
			return writeJSON(a.Out, s)
		},
	}
	cmd.Flags().BoolVar(&encerradas, "incluir-encerradas", false, "traz também as ondas encerradas")
	// --json é a forma do desenho e da story; não muda nada, o snapshot já é sempre JSON.
	cmd.Flags().Bool("json", false, "aceito por compatibilidade: a saída já é sempre JSON")
	return cmd
}

func (a *App) diarioCmd() *cobra.Command {
	var us string
	var onda int64
	var limite int
	cmd := &cobra.Command{
		Use:   "diario",
		Short: "Eventos gravados (toda escrita gera um), do mais antigo ao mais novo",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			f := central.FiltroDiario{OndaID: onda, Coord: a.flagCoord, Limite: limite}
			var coord *central.Coordenador
			if us != "" {
				ref, err := refs.ParseUS(us)
				if err != nil {
					return usoDe(err)
				}
				f.US = &ref
				if coord, err = a.coordOpcional(ctx, st); err != nil {
					return err
				}
			}
			es, err := st.Diario(ctx, coord, f)
			if err != nil {
				return err
			}
			return a.emit(es, func(w io.Writer) error {
				for _, e := range es {
					if err := textoEvento(w, e); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&us, "us", "", "só os eventos desta US")
	cmd.Flags().Int64Var(&onda, "onda", 0, "só os eventos desta onda")
	cmd.Flags().IntVar(&limite, "limite", 50, "quantos eventos, os mais recentes (0 = todos)")
	return cmd
}

// preparar abre o banco, resolve o coordenador (se houver) e lê a ref da US.
func (a *App) preparar(cmd *cobra.Command, arg string) (ctx context.Context, st *central.Store, coord *central.Coordenador, ref refs.US, err error) {
	ctx = cmd.Context()
	if ref, err = refs.ParseUS(arg); err != nil {
		return ctx, nil, nil, ref, usoDe(err)
	}
	if st, err = a.store(ctx); err != nil {
		return
	}
	coord, err = a.coordOpcional(ctx, st)
	return
}
