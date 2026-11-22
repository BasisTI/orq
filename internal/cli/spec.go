package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
	"github.com/spf13/cobra"
)

// SpecConferida é a saída de orq spec conferir.
type SpecConferida struct {
	US      string `json:"us"`
	Caminho string `json:"caminho"`
	MRURL   string `json:"mr_url"`
	// Aprovada: a última validação foi aprovada. Sem ela não há o que conferir.
	Aprovada    bool                     `json:"aprovada"`
	PodeMesclar bool                     `json:"pode_mesclar"`
	Motivo      string                   `json:"motivo,omitempty"`
	Conferencia *central.ConferenciaSpec `json:"conferencia"`
}

func (a *App) specCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "spec", Short: "Registra a spec de uma US e confere a MR dela antes de mesclar"}
	var caminho, mr string
	var substituir bool
	registrar := &cobra.Command{
		Use:   "registrar <ref> --caminho <pasta> --mr <url> [--substituir]",
		Short: "Registra a pasta e a MR da spec da US (e liga a MR à US)",
		Long: `Registra a pasta da spec (relativa à raiz do repositório) e a MR da spec da US. O repositório sai da URL da
MR. O mesmo comando liga a MR à US, como orq mr add, na mesma transação. Uma spec por US: com spec já
registrada, o comando recusa e mostra o registro atual; avise o responsável e pergunte se substitui, e só
com o sim repita com --substituir (o diário guarda os valores anteriores e os novos).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, st, coord, ref, err := a.preparar(cmd, args[0])
			if err != nil {
				return err
			}
			if mr == "" {
				return &output.Error{Code: "uso", Cause: "falta --mr com a URL da MR da spec", Exit: output.ExitUsage}
			}
			r, err := st.RegistrarSpec(ctx, coord, ref, caminho, mr, substituir)
			if err != nil {
				return err
			}
			return a.emit(r, func(w io.Writer) error {
				verbo := "registrada"
				if r.Substituiu {
					verbo = "substituída (antes: " + r.Anterior.Caminho + ", " + r.Anterior.MRURL + ")"
				}
				_, err := fmt.Fprintf(w, "spec %s: %s  %s\n", verbo, r.Spec.Caminho, r.Spec.MRURL)
				return err
			})
		},
	}
	registrar.Flags().StringVar(&caminho, "caminho", "", "pasta da spec, relativa à raiz do repositório")
	registrar.Flags().StringVar(&mr, "mr", "", "URL da MR da spec")
	registrar.Flags().BoolVar(&substituir, "substituir", false, "troca a spec já registrada (só depois de perguntar ao responsável)")

	conferir := &cobra.Command{
		Use:   "conferir <ref>",
		Short: "Diz se a MR da spec pode ser mesclada: só o decisoes.md mudou desde a aprovação",
		Long: `Só leitura. Toma o SHA da validação aprovada, compara o conteúdo da pasta registrada da spec com o da
cabeça da MR (pela API da forja) e responde "pode mesclar" quando nada mudou nela além do decisoes.md; senão
lista os arquivos que mudaram. Arquivo alterado fora da pasta é outro sinal ("a MR não é só da spec") e não
reprova a spec. Sai com erro se a spec não foi aprovada ou se a forja falha.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, st, coord, ref, err := a.preparar(cmd, args[0])
			if err != nil {
				return err
			}
			sp, err := st.LerSpec(ctx, coord, ref)
			if err != nil {
				return err
			}
			res := SpecConferida{US: ref.Chave(), Caminho: sp.Caminho, MRURL: sp.MRURL}
			if sp.SHAAprovado == nil {
				res.Motivo = "a spec não tem validação aprovada (estado: " + sp.Estado + ")"
				return a.emit(res, func(w io.Writer) error { _, err := fmt.Fprintln(w, "não pode mesclar: "+res.Motivo); return err })
			}
			res.Aprovada = true
			f := enriquecer.Ferramentas{Env: a.Env}
			c, err := f.ConferirSpec(ctx, sp.MRURL, sp.Caminho, *sp.SHAAprovado)
			if err != nil {
				var fa *enriquecer.Falha
				if errors.As(err, &fa) && fa.Codigo == enriquecer.ErrPastaAusente {
					return &output.Error{Code: enriquecer.ErrPastaAusente, Cause: fa.Mensagem, Recovery: "confira o --caminho registrado; com o sim da pessoa, orq spec registrar " + refs.Exibir(ref.Ref) + " --caminho <pasta certa> --mr <url> --substituir", Exit: output.ExitConflict}
				}
				return &output.Error{Code: "forja_indisponivel", Cause: "não consegui conferir a MR da spec na forja: " + err.Error(), Recovery: "tente de novo quando a forja responder", Exit: output.ExitUnexpected}
			}
			res.Conferencia, res.PodeMesclar = &c, c.PodeMesclar
			return a.emit(res, func(w io.Writer) error { return textoConferencia(w, res) })
		},
	}
	cmd.AddCommand(registrar, conferir)
	return cmd
}

func textoConferencia(w io.Writer, r SpecConferida) error {
	c := r.Conferencia
	linhas := []string{}
	if r.PodeMesclar {
		linhas = append(linhas, fmt.Sprintf("pode mesclar: só o decisoes.md mudou em %s desde a aprovação (%s → %s)", r.Caminho, curto(c.SHAAprovado), curto(c.SHACabeca)))
	} else {
		linhas = append(linhas, fmt.Sprintf("não pode mesclar: mudou em %s além do decisoes.md desde a aprovação (%s → %s):", r.Caminho, curto(c.SHAAprovado), curto(c.SHACabeca)))
		for _, p := range c.MudouNaPasta {
			linhas = append(linhas, "  "+p)
		}
	}
	if len(c.ForaDaPasta) > 0 {
		linhas = append(linhas, fmt.Sprintf("atenção: a MR não é só da spec; a forja aponta %d arquivo(s) alterado(s) fora da pasta:", len(c.ForaDaPasta)))
		for i, p := range c.ForaDaPasta {
			if i == 20 {
				linhas = append(linhas, fmt.Sprintf("  … e mais %d", len(c.ForaDaPasta)-20))
				break
			}
			linhas = append(linhas, "  "+p)
		}
	}
	if c.ForaDaPastaErro != "" {
		linhas = append(linhas, "atenção: não consegui ver o que mudou fora da pasta: "+c.ForaDaPastaErro)
	}
	_, err := fmt.Fprintln(w, strings.Join(linhas, "\n"))
	return err
}

// curto é o SHA abreviado para o texto.
func curto(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
