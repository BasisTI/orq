package cli

import (
	"context"
	"fmt"

	"github.com/BasisTI/orq/internal/central"
)

// avisarPendenciasAbertas escreve no stderr uma linha por pendência ainda aberta (ou só
// respondida) das US da onda, e de uma só US quando chave não é vazia. É só aviso, em
// leitura: não responde, não resolve e não muda o exit. Traz a chave e o estado, nunca o
// texto da pendência, que pode ser de outro projeto. Falha ao ler não derruba o comando que
// já escreveu.
func (a *App) avisarPendenciasAbertas(ctx context.Context, st *central.Store, ondaID int64, chave string) {
	ps, err := st.ListarPendencias(ctx, central.FiltroPendencias{Abertas: true, OndaID: ondaID})
	if err != nil {
		return
	}
	for _, p := range ps {
		if chave != "" && p.US != chave {
			continue
		}
		if p.Estado == "respondida" {
			// resolver --resposta troca a resposta registrada: ler antes.
			fmt.Fprintf(a.Err, "aviso: %s respondida com a US concluída ou a onda encerrada: leia com orq pend ver %s, aja e feche com orq pend resolver %s (vale a resposta registrada)\n", p.Chave, p.Chave, p.Chave)
			continue
		}
		fmt.Fprintf(a.Err, "aviso: %s %s com a US concluída ou a onda encerrada: resolva, leve para outra onda ou para o Taiga e feche com orq pend resolver %s --resposta \"levada para <destino>\"\n", p.Chave, p.Estado, p.Chave)
	}
}
