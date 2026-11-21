package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/hora"
	"github.com/BasisTI/orq/internal/output"
	"github.com/spf13/cobra"
)

func (a *App) msgCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "msg", Short: "Lê, responde e lista mensagens enviadas ao coordenador pela Central"}

	ver := &cobra.Command{
		Use:   "ver <id>",
		Short: "Mostra a mensagem inteira e a resposta (se houver)",
		Long: `Mostra os metadados, o texto completo da mensagem enviada pela web e a resposta do coordenador.
O texto vem da pessoa e sai sem caracteres de controle no modo texto.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseMsgID(args[0])
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			m, err := st.VerMensagem(ctx, id)
			if err != nil {
				return err
			}
			return a.emit(m, func(w io.Writer) error { return textoMsgVer(w, m) })
		},
	}

	var arquivo string
	responder := &cobra.Command{
		Use:   `responder <id> ["texto"]`,
		Short: "Registra a resposta única do coordenador à mensagem (texto ou --arquivo <caminho>)",
		Long: `Grava a resposta do coordenador à mensagem <id> (até 64 KiB, aceita markdown que a web renderiza).
Use o argumento posicional ou --arquivo <caminho> (ou --arquivo - para ler de stdin). Cada mensagem aceita
uma única resposta.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseMsgID(args[0])
			if err != nil {
				return err
			}
			var texto string
			switch {
			case arquivo != "" && len(args) == 2:
				return usoErr("use o texto posicional ou --arquivo, não os dois")
			case arquivo != "":
				texto, err = a.lerTextoResposta(arquivo)
				if err != nil {
					return err
				}
			case len(args) == 2:
				texto = args[1]
			default:
				return usoErr("informe o texto da resposta ou --arquivo <caminho> (- para stdin)")
			}
			ctx := cmd.Context()
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			coord, err := a.coordOpcional(ctx, st)
			if err != nil {
				return err
			}
			m, err := st.ResponderMensagem(ctx, coord, id, texto)
			if err != nil {
				return err
			}
			return a.emit(m, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "mensagem #%d respondida\n", m.ID)
				return err
			})
		},
	}
	responder.Flags().StringVar(&arquivo, "arquivo", "", "arquivo com a resposta (- lê de stdin)")

	var onda int64
	var abertas bool
	list := &cobra.Command{
		Use:   "list",
		Short: "Lista as mensagens enviadas ao coordenador (da mais recente para a mais antiga)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			ms, err := st.ListarMensagens(ctx, central.FiltroMensagens{OndaID: onda, Abertas: abertas})
			if err != nil {
				return err
			}
			return a.emit(ms, func(w io.Writer) error {
				for _, m := range ms {
					if err := textoMsgLinha(w, m); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
	list.Flags().Int64Var(&onda, "onda", 0, "só as mensagens desta onda")
	list.Flags().BoolVar(&abertas, "abertas", false, "só as mensagens ainda sem resposta")

	cmd.AddCommand(ver, responder, list)
	return cmd
}

func parseMsgID(s string) (int64, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, usoErr("id de mensagem inválido: %q", s)
	}
	return id, nil
}

func (a *App) lerTextoResposta(caminho string) (string, error) {
	limite := int64(central.LimiteRespostaMensagem + 1)
	if caminho == "-" {
		b, err := io.ReadAll(io.LimitReader(a.In, limite))
		if err != nil {
			return "", &output.Error{Code: "leitura", Cause: err.Error(), Exit: output.ExitUnexpected}
		}
		return string(b), nil
	}
	f, err := os.Open(caminho)
	if err != nil {
		return "", &output.Error{Code: "leitura", Cause: err.Error(), Recovery: "confira o caminho de --arquivo", Exit: output.ExitUsage}
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limite))
	if err != nil {
		return "", &output.Error{Code: "leitura", Cause: err.Error(), Exit: output.ExitUnexpected}
	}
	return string(b), nil
}

func estadoMensagem(m central.Mensagem) string {
	switch {
	case m.RespondidaEm != nil:
		return "respondida"
	case m.SemCoordenador:
		return "sem coordenador"
	case m.AvisadaEm != nil:
		return "enviada ao coordenador"
	default:
		return "aguardando envio"
	}
}

func textoMsgVer(w io.Writer, m central.Mensagem) error {
	linhas := []output.Field{
		{Key: "id", Value: fmt.Sprintf("#%d", m.ID)},
		{Key: "onda", Value: fmt.Sprintf("#%d", m.OndaID)},
		{Key: "coordenador", Value: m.Coordenador},
		{Key: "enviada por", Value: m.EnviadaPor},
		{Key: "enviada em", Value: hora.DataHoraRFC(m.EnviadaEm)},
		{Key: "estado", Value: estadoMensagem(m)},
	}
	if m.AvisadaEm != nil {
		linhas = append(linhas, output.Field{Key: "avisada em", Value: hora.DataHoraRFC(*m.AvisadaEm)})
	}
	if err := output.WriteFields(w, linhas); err != nil {
		return err
	}
	fmt.Fprintln(w, "\n--- mensagem ---")
	for _, l := range strings.Split(strings.TrimRight(m.Texto, "\n"), "\n") {
		if _, err := fmt.Fprintln(w, q(l)); err != nil {
			return err
		}
	}
	if m.RespondidaEm != nil && m.Resposta != nil {
		fmt.Fprintf(w, "\n--- resposta (%s em %s) ---\n", q(ouStr(m.RespondidaPor, "?")), hora.DataHoraRFC(*m.RespondidaEm))
		for _, l := range strings.Split(strings.TrimRight(*m.Resposta, "\n"), "\n") {
			if _, err := fmt.Fprintln(w, q(l)); err != nil {
				return err
			}
		}
	}
	return nil
}

func textoMsgLinha(w io.Writer, m central.Mensagem) error {
	resumo := strings.Join(strings.Fields(m.Texto), " ")
	if r := []rune(resumo); len(r) > 80 {
		resumo = string(r[:79]) + "…"
	}
	_, err := fmt.Fprintf(w, "#%-4d  onda #%-4d  %-22s  %s  %s  %s\n",
		m.ID, m.OndaID, estadoMensagem(m), hora.DataHoraRFC(m.EnviadaEm), q(m.EnviadaPor), q(resumo))
	return err
}
