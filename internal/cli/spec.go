package cli

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/hora"
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
				if _, err := fmt.Fprintf(w, "spec %s: %s  %s\n", verbo, r.Spec.Caminho, r.Spec.MRURL); err != nil {
					return err
				}
				if r.Substituiu {
					if _, err := fmt.Fprintf(w, "versão %d da spec\n", r.Spec.Versao); err != nil {
						return err
					}
					if aviso := avisoLinksAtivos(r.LinksAtivos); aviso != "" {
						_, err := fmt.Fprintln(w, aviso)
						return err
					}
				}
				return nil
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
	cmd.AddCommand(registrar, conferir, a.specLinkCmd())
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

// avisoLinksAtivos é o aviso do --substituir: os links públicos ativos seguem e mostram a spec nova.
func avisoLinksAtivos(n int) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return "1 link ativo passa a mostrar a spec nova"
	}
	return fmt.Sprintf("%d links ativos passam a mostrar a spec nova", n)
}

// LinkGeradoSaida é a saída de orq spec link gerar: o link e a URL, que só aparece desta vez.
type LinkGeradoSaida struct {
	US       string  `json:"us"`
	ID       int64   `json:"id"`
	Rotulo   string  `json:"rotulo"`
	CriadoEm string  `json:"criado_em"`
	ExpiraEm *string `json:"expira_em"`
	URL      string  `json:"url"`
	// Aviso lembra que o orq guarda só o hash e não mostra a URL de novo.
	Aviso string `json:"aviso"`
}

// LinksSaida é a saída de orq spec link listar.
type LinksSaida struct {
	US    string             `json:"us"`
	Links []central.LinkSpec `json:"links"`
}

func (a *App) specLinkCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "link", Short: "Gera, lista e revoga os links públicos da spec de uma US"}
	var para, expira, urlPublica string
	gerar := &cobra.Command{
		Use:   "gerar <ref> --para <rótulo> [--expira 30d] [--url-publica <base>]",
		Short: "Gera um link público, só leitura, para a spec da US (a URL sai uma única vez)",
		Long: `Gera um link público para a spec registrada da US: abre só a spec, em modo leitura, pelo socket público do
daemon (orq daemon --web-publico). O código tem 256 bits; o orq guarda só o hash dele e não consegue mostrar a URL
de novo: copie-a daqui. Cada spec pode ter vários links ativos, cada um com o seu rótulo e revogado à parte.

A base da URL vem de --url-publica ou de ORQ_WEB_PUBLICO_URL (ambiente ou orq.env); sem nenhuma das duas o comando
recusa (url_publica_ausente) e não cria o link. Nunca usa ORQ_WEB_URL, que é o da Central, atrás do login.
--expira (30d, 12h, 90m) é opcional; sem ele o link vale até ser revogado.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, st, coord, ref, err := a.preparar(cmd, args[0])
			if err != nil {
				return err
			}
			base := strings.TrimSpace(urlPublica)
			if base == "" {
				base = strings.TrimSpace(a.Env("ORQ_WEB_PUBLICO_URL"))
			}
			if base, err = baseURLPublica(base); err != nil {
				return err
			}
			validade, err := parseValidadeLink(expira)
			if err != nil {
				return err
			}
			l, err := st.GerarLink(ctx, coord, ref, para, validade)
			if err != nil {
				return err
			}
			out := LinkGeradoSaida{US: ref.Chave(), ID: l.ID, Rotulo: l.Rotulo, CriadoEm: l.CriadoEm, ExpiraEm: l.ExpiraEm, URL: base + "/s/" + l.Codigo,
				Aviso: "esta URL não aparece de novo: o orq guarda só o hash do código; para perdê-la sem risco, revogue o link e gere outro"}
			return a.emit(out, func(w io.Writer) error {
				validade := "sem validade"
				if l.ExpiraEm != nil {
					validade = "vale até " + hora.DataHoraRFC(*l.ExpiraEm)
				}
				_, err := fmt.Fprintf(w, "link %d (%s), %s:\n%s\n%s\n", l.ID, q(l.Rotulo), validade, out.URL, out.Aviso)
				return err
			})
		},
	}
	gerar.Flags().StringVar(&para, "para", "", "para quem é o link (rótulo, até 80 caracteres)")
	gerar.Flags().StringVar(&expira, "expira", "", "validade do link: 30d, 12h ou 90m (sem a flag, vale até ser revogado)")
	gerar.Flags().StringVar(&urlPublica, "url-publica", "", "base da URL pública (padrão: ORQ_WEB_PUBLICO_URL)")

	listar := &cobra.Command{
		Use:   "listar <ref>",
		Short: "Lista os links públicos da spec da US: rótulo, validade, estado, último acesso e acessos",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, st, coord, ref, err := a.preparar(cmd, args[0])
			if err != nil {
				return err
			}
			ls, err := st.ListarLinks(ctx, coord, ref)
			if err != nil {
				return err
			}
			return a.emit(LinksSaida{US: ref.Chave(), Links: ls}, func(w io.Writer) error {
				if len(ls) == 0 {
					_, err := fmt.Fprintf(w, "%s não tem link público\n", ref.Chave())
					return err
				}
				for _, l := range ls {
					validade := "sem validade"
					if l.ExpiraEm != nil {
						validade = "até " + hora.DataHoraRFC(*l.ExpiraEm)
					}
					ultimo := "nunca acessado"
					if l.UltimoAcesso != nil {
						ultimo = "último acesso " + hora.DataHoraRFC(*l.UltimoAcesso)
					}
					if _, err := fmt.Fprintf(w, "%d  %s  %s  criado %s  %s  %s  %d acessos\n", l.ID, l.Estado, q(l.Rotulo), hora.DataHoraRFC(l.CriadoEm), validade, ultimo, l.Acessos); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}

	revogar := &cobra.Command{
		Use:   "revogar <ref> <id>",
		Short: "Revoga um link público, de forma definitiva",
		Long:  "Revoga o link: ele deixa de abrir (404). A revogação é definitiva; para voltar a compartilhar, gere outro link.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, st, coord, ref, err := a.preparar(cmd, args[0])
			if err != nil {
				return err
			}
			id, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil || id <= 0 {
				return &output.Error{Code: "uso", Cause: fmt.Sprintf("id de link %q inválido", args[1]), Recovery: "veja os ids com orq spec link listar " + refs.Exibir(ref.Ref), Exit: output.ExitUsage}
			}
			l, err := st.RevogarLink(ctx, coord, ref, id)
			if err != nil {
				return err
			}
			return a.emit(l, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "link %d (%s) revogado\n", l.ID, q(l.Rotulo))
				return err
			})
		},
	}
	cmd.AddCommand(gerar, listar, revogar)
	return cmd
}

// baseURLPublica confere a base da URL pública: obrigatória, http ou https, sem barra no fim.
func baseURLPublica(base string) (string, error) {
	if base == "" {
		return "", &output.Error{Code: "url_publica_ausente", Cause: "sem a base da URL pública: nem --url-publica nem ORQ_WEB_PUBLICO_URL",
			Recovery: "passe --url-publica https://<host público> ou ponha ORQ_WEB_PUBLICO_URL no ambiente ou no orq.env; o ORQ_WEB_URL é o da Central e não serve", Exit: output.ExitUsage}
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", &output.Error{Code: "uso", Cause: "a base da URL pública precisa ser http ou https, com host e sem query nem âncora", Recovery: "ex. https://specs.exemplo.com.br", Exit: output.ExitUsage}
	}
	return strings.TrimRight(base, "/"), nil
}

// parseValidadeLink lê o --expira: número inteiro e d (dias), h (horas) ou m (minutos). Vazio é
// sem validade.
func parseValidadeLink(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	unidades := map[byte]time.Duration{'d': 24 * time.Hour, 'h': time.Hour, 'm': time.Minute}
	u, ok := unidades[s[len(s)-1]]
	n, err := strconv.Atoi(s[:len(s)-1])
	if !ok || err != nil || n <= 0 || time.Duration(n) > central.MaxValidadeLink/u {
		return 0, &output.Error{Code: "uso", Cause: fmt.Sprintf("--expira %q inválido", s), Recovery: "use um inteiro positivo com d, h ou m: 30d, 12h, 90m (até 3650d)", Exit: output.ExitUsage}
	}
	return time.Duration(n) * u, nil
}
