package limpeza

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
)

// Estados de um item da limpeza. Remover é o plano (dry-run); Removido, Preservado e Falhou são
// o resultado de executar. Ausente quer dizer que não havia nada a fazer (já tinha sumido).
const (
	Remover    = "remover"
	Removido   = "removido"
	Ausente    = "ausente"
	Preservado = "preservado"
	Falhou     = "falhou"
)

// Item é um recurso da US e o que a limpeza faz (ou fez) com ele.
type Item struct {
	Tipo   string `json:"tipo"` // pane, worktree, branch_local, branch_remota, pasta_tmp, pasta_trabalho, docker_*
	Alvo   string `json:"alvo"`
	Estado string `json:"estado"`
	Motivo string `json:"motivo,omitempty"`

	deps []*Item
	exec func(ctx context.Context) error
}

// preservar é o erro de um exec que, na hora de agir, achou motivo para não remover.
type preservar string

func (p preservar) Error() string { return string(p) }

// Resultado é a saída de orq limpar.
type Resultado struct {
	US     string  `json:"us"`
	DryRun bool    `json:"dry_run"`
	Recusa string  `json:"recusa,omitempty"` // a limpeza nem começou: o porquê
	Itens  []*Item `json:"itens"`
	// Agiu: removeu ao menos um recurso. Completa: nenhum item ficou preservado ou falhou, e a
	// pasta de trabalho (com o recursos.json) se foi.
	Agiu     bool `json:"agiu"`
	Completa bool `json:"completa"`
}

// Limpador remove os recursos de uma US.
type Limpador struct {
	St     *central.Store
	F      enriquecer.Ferramentas
	Raizes Raizes
	// PaneProprio é o pane onde o comando roda ($HERDR_PANE_ID): nunca é fechado.
	PaneProprio string
	// antesDoItem, se não nulo, roda logo antes de executar cada item (os testes seguram a
	// execução ali para reabrir a US de outro lado).
	antesDoItem func(*Item)
	// aposPlano, se não nulo, roda entre o plano e a execução (os testes intercalam a reabertura).
	aposPlano func()
	// Cwd é o diretório corrente; vazio usa os.Getwd. Worktree que contém o Cwd é preservado.
	Cwd string
}

// Opcoes de uma execução.
type Opcoes struct {
	DryRun bool
	// Registrar decide se o resultado vai para o diário; nil registra sempre (menos no dry-run).
	// O daemon usa para não repetir o mesmo "preservado" a cada tentativa.
	Registrar func(Resultado) bool
}

// Limpar confere a US e remove os recursos dela. A recusa (MR não mesclada, head diferente do
// aprovado, sem recursos.json) volta em Resultado.Recusa, sem erro; erro é só falha do banco
// ou referência inexistente.
func (l *Limpador) Limpar(ctx context.Context, coord *central.Coordenador, ref refs.US, op Opcoes) (Resultado, error) {
	loc, _, err := l.St.LocalizarUS(ctx, coord, ref)
	if err != nil {
		return Resultado{}, err
	}
	u := loc.US
	res := Resultado{US: u.Chave(), DryRun: op.DryRun, Itens: []*Item{}}
	var us central.US
	if !op.DryRun {
		// A trava vai do plano ao último item: reabrir a US (e regravar os recursos) espera a
		// limpeza acabar, e só então o coordenador recria o worktree. O dry-run não escreve e
		// não trava.
		soltar, err := l.Raizes.TravarUS(u, 0)
		if err != nil {
			if output.AsError(err).Code != "limpeza_em_andamento" {
				return res, err
			}
			res.Recusa = "outra limpeza, gravação ou reabertura desta US está em andamento; tento de novo depois"
			return res, nil
		}
		defer soltar()
	}
	// A US é lida já sob a trava.
	if _, us, err = l.St.LocalizarUS(ctx, coord, ref); err != nil {
		return res, err
	}
	finalizar := func() (Resultado, error) {
		if !op.DryRun && (op.Registrar == nil || op.Registrar(res)) {
			if err := l.St.RegistrarExternos(ctx, coord, u, eventos(res)); err != nil {
				return res, err
			}
		}
		return res, nil
	}
	rec, hash, err := lerRecursosHash(l.Raizes.Arquivo(u), u)
	switch {
	case errors.Is(err, ErrSemRecursos):
		res.Recusa = "sem recursos gravados em " + l.Raizes.Arquivo(u) + " (grave com orq recursos gravar)"
		return finalizar()
	case err != nil:
		res.Recusa = "recursos.json inválido: " + err.Error()
		return finalizar()
	}
	mrs, recusa := l.conferirMRs(ctx, us, rec)
	if recusa != "" {
		res.Recusa = recusa
		return finalizar()
	}
	itens := l.planejar(ctx, u, rec, mrs)
	res.Itens = itens
	if !op.DryRun {
		if l.aposPlano != nil {
			l.aposPlano()
		}
		l.executar(ctx, itens, l.geracao(coord, u, hash, urlsDasMRs(us)))
	}
	res.Agiu, res.Completa = resumo(itens)
	return finalizar()
}

// resumo calcula Agiu e Completa. Completa exige que a pasta de trabalho tenha sido removida
// (ou nem exista), que é o último item.
func resumo(itens []*Item) (agiu, completa bool) {
	completa = true
	for _, i := range itens {
		switch i.Estado {
		case Removido:
			agiu = true
		case Ausente:
		default:
			completa = false
		}
	}
	return agiu, completa
}

func eventos(r Resultado) []central.EventoExterno {
	if r.Recusa != "" {
		return []central.EventoExterno{{Tipo: "limpar.recusada", Detalhe: map[string]any{"motivo": r.Recusa}}}
	}
	var es []central.EventoExterno
	for _, i := range r.Itens {
		tipo := ""
		switch i.Estado {
		case Removido:
			tipo = "limpar.removido"
		case Preservado:
			tipo = "limpar.preservado"
		case Falhou:
			tipo = "limpar.falhou"
		default:
			continue
		}
		d := map[string]any{"tipo": i.Tipo, "alvo": i.Alvo}
		if i.Motivo != "" {
			d["motivo"] = i.Motivo
		}
		es = append(es, central.EventoExterno{Tipo: tipo, Detalhe: d})
	}
	return es
}

// Assinatura identifica o desfecho de um resultado, para o daemon não repetir o que já registrou.
func Assinatura(r Resultado) string {
	var ps []string
	if r.Recusa != "" {
		ps = append(ps, "recusa:"+r.Recusa)
	}
	for _, i := range r.Itens {
		if i.Estado != Ausente {
			ps = append(ps, i.Estado+"|"+i.Tipo+"|"+i.Alvo+"|"+i.Motivo)
		}
	}
	sort.Strings(ps)
	return strings.Join(ps, "\n")
}

// conferirMRs consulta cada MR da US na forja (não confia no cache): todas precisam estar
// mescladas e, havendo head aprovado gravado, o head mesclado tem de ser ele. Devolve as MRs
// mescladas por branch de origem.
func (l *Limpador) conferirMRs(ctx context.Context, us central.US, rec *Recursos) (map[string]enriquecer.DetalheMR, string) {
	if len(us.MRs) == 0 {
		return nil, "a US não tem MR ligada (orq mr add)"
	}
	if len(rec.MRsEncerradas) > 0 {
		nova := false
		for _, m := range us.MRs {
			if !contemStr(rec.MRsEncerradas, m.URL) {
				nova = true
			}
		}
		if !nova {
			return nil, "a US foi reaberta e ainda não tem MR nova ligada (orq mr add)"
		}
	}
	porBranch := map[string]enriquecer.DetalheMR{}
	var faltam []string
	for _, m := range us.MRs {
		d, err := l.F.ConsultarMRDetalhe(ctx, m.URL)
		// As MRs encerradas (de antes do `reabrir`) já passaram pelo gate na geração delas, quando
		// o `reabrir` as conferiu, e o arquivo com os aprovados dela pode ter sido apagado pela
		// limpeza completa: elas só ajudam a achar a branch, sem gate. A regra vale para as MRs
		// da geração atual, e uma MR nova sem aprovação continua recusando.
		if contemStr(rec.MRsEncerradas, m.URL) {
			if err == nil && d.Estado == "mesclada" {
				porBranch[d.Origem] = d
			}
			continue
		}
		if err != nil {
			return nil, fmt.Sprintf("não consegui conferir a MR %s: %v", m.URL, err)
		}
		if d.Estado != "mesclada" {
			faltam = append(faltam, fmt.Sprintf("%s (%s)", m.URL, d.Estado))
			continue
		}
		// Conteúdo integrado não prova aprovação: sem o head aprovado registrado, preserva.
		ap, ok := rec.Aprovados[m.URL]
		if !ok {
			return nil, fmt.Sprintf("sem head aprovado registrado para a MR %s (orq recursos gravar --aprovado %s=<sha>)", m.URL, m.URL)
		}
		if d.Head == "" || !strings.HasPrefix(d.Head, ap) {
			return nil, fmt.Sprintf("o head mesclado da MR %s (%s) não é o aprovado (%s)", m.URL, d.Head, ap)
		}
		porBranch[d.Origem] = d
	}
	if len(faltam) > 0 {
		return nil, "MR não mesclada: " + strings.Join(faltam, ", ")
	}
	return porBranch, ""
}

// planejar monta os itens, nesta ordem: panes, git (worktree, branch local, branch remota),
// pasta temporária, Docker e, por último, a pasta de trabalho. Tudo depende dos panes, e a pasta
// de trabalho (onde mora o recursos.json) só some se todo o resto foi resolvido: senão o daemon
// esqueceria o que ficou.
func (l *Limpador) planejar(ctx context.Context, u refs.US, rec *Recursos, mrs map[string]enriquecer.DetalheMR) []*Item {
	panes := l.planejarPanes(ctx, rec)
	var itens []*Item
	itens = append(itens, panes...)
	for _, r := range rec.Repos {
		for _, i := range l.planejarRepo(ctx, r, mrs) {
			i.deps = append(i.deps, panes...)
			itens = append(itens, i)
		}
	}
	tmp := l.itemPasta("pasta_tmp", l.Raizes.Tmp, l.Raizes.PastaTmp(u), rec.Tmp, u)
	tmp.deps = panes
	itens = append(itens, tmp)
	for _, i := range l.planejarDocker(ctx, "orq.us", RotuloUS(u)) {
		i.deps = append(i.deps, panes...)
		itens = append(itens, i)
	}
	trabalho := l.itemPasta("pasta_trabalho", l.Raizes.Trabalho, l.Raizes.PastaTrabalho(u), rec.Trabalho, u)
	trabalho.deps = append([]*Item(nil), itens...)
	itens = append(itens, trabalho)
	propagar(itens)
	return itens
}

// propagar marca como Preservado, já no plano, o item que depende de outro preservado: o
// dry-run mostra o que de fato seria removido.
func propagar(itens []*Item) {
	for _, i := range itens {
		if i.Estado != Remover {
			continue
		}
		for _, d := range i.deps {
			if d.Estado == Preservado || d.Estado == Falhou {
				i.Estado, i.Motivo = Preservado, fmt.Sprintf("%s %s ficou preservado", d.Tipo, d.Alvo)
				break
			}
		}
	}
}

func urlsDasMRs(us central.US) string {
	var ps []string
	for _, m := range us.MRs {
		ps = append(ps, m.URL)
	}
	sort.Strings(ps)
	return strings.Join(ps, "\n")
}

// geracao devolve a guarda de uma execução: o plano vale para o recursos.json e o conjunto de MRs
// que foram conferidos. Se o coordenador reabriu a US depois (regravou os recursos, ligou MR
// nova), o plano está velho e nada mais é removido.
func (l *Limpador) geracao(coord *central.Coordenador, u refs.US, hash, mrs string) func(context.Context) error {
	return func(ctx context.Context) error {
		if _, atual, err := lerRecursosHash(l.Raizes.Arquivo(u), u); err != nil || atual != hash {
			return fmt.Errorf("o recursos.json mudou depois do plano (a US foi reaberta?)")
		}
		_, us, err := l.St.LocalizarUS(ctx, coord, u)
		if err != nil {
			return fmt.Errorf("não consegui reler a US: %v", err)
		}
		if urlsDasMRs(us) != mrs {
			return fmt.Errorf("as MRs da US mudaram depois do plano (a US foi reaberta?)")
		}
		return nil
	}
}

// executar roda os itens em Remover, na ordem. Um item só roda se todas as dependências
// terminaram bem (Removido ou Ausente) e se a guarda de geração, quando há, ainda passa; senão
// fica Preservado.
func (l *Limpador) executar(ctx context.Context, itens []*Item, guarda func(context.Context) error) {
	for _, i := range itens {
		if i.Estado != Remover {
			continue
		}
		bloqueio := ""
		for _, d := range i.deps {
			if d.Estado != Removido && d.Estado != Ausente {
				bloqueio = fmt.Sprintf("%s %s ficou %s", d.Tipo, d.Alvo, d.Estado)
				break
			}
		}
		if bloqueio != "" {
			i.Estado, i.Motivo = Preservado, bloqueio
			continue
		}
		if l.antesDoItem != nil {
			l.antesDoItem(i)
		}
		if guarda != nil {
			if err := guarda(ctx); err != nil {
				i.Estado, i.Motivo = Preservado, err.Error()
				continue
			}
		}
		err := i.exec(ctx)
		var p preservar
		switch {
		case err == nil:
			i.Estado = Removido
		case errors.As(err, &p):
			i.Estado, i.Motivo = Preservado, string(p)
		default:
			i.Estado, i.Motivo = Falhou, err.Error()
		}
	}
}

// itemPasta planeja a remoção de uma pasta derivada da raiz. declarado é o que o recursos.json
// diz; diferente do derivado, a pasta é preservada.
func (l *Limpador) itemPasta(tipo, raiz, caminho, declarado string, u refs.US) *Item {
	it := &Item{Tipo: tipo, Alvo: caminho}
	if declarado != "" && filepath.Clean(declarado) != caminho {
		it.Estado, it.Motivo = Preservado, fmt.Sprintf("o recursos.json declara %s, fora do caminho derivado da raiz", declarado)
		return it
	}
	existe, motivo := pastaSegura(raiz, caminho, u)
	switch {
	case motivo != "":
		it.Estado, it.Motivo = Preservado, motivo
	case !existe:
		it.Estado = Ausente
	default:
		it.Estado = Remover
		it.exec = func(context.Context) error {
			// Confere de novo na hora: o plano pode ter ficado velho.
			if _, motivo := pastaSegura(raiz, caminho, u); motivo != "" {
				return preservar(motivo)
			}
			return removerArvore(caminho)
		}
	}
	return it
}

// pastaSegura confere que caminho é <raiz>/<projeto>/TG-<ref> de verdade: não é symlink, e depois
// de resolver os links do caminho e da raiz continua exatamente ali, abaixo da raiz.
func pastaSegura(raiz, caminho string, u refs.US) (existe bool, motivo string) {
	if raiz == "" || caminho == "" || !filepath.IsAbs(raiz) || !filepath.IsAbs(caminho) {
		return false, "caminho vazio ou relativo"
	}
	st, err := os.Lstat(caminho)
	if errors.Is(err, os.ErrNotExist) {
		return false, ""
	}
	if err != nil {
		return false, "não consegui ler " + caminho + ": " + err.Error()
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return false, caminho + " é um link simbólico"
	}
	if !st.IsDir() {
		return false, caminho + " não é uma pasta"
	}
	rr, err := filepath.EvalSymlinks(raiz)
	if err != nil {
		return false, "não consegui resolver a raiz " + raiz + ": " + err.Error()
	}
	if rr == "/" || rr == filepath.Clean(os.Getenv("HOME")) {
		return false, "a raiz " + raiz + " é " + rr
	}
	rc, err := filepath.EvalSymlinks(caminho)
	if err != nil {
		return false, "não consegui resolver " + caminho + ": " + err.Error()
	}
	esperado := filepath.Join(rr, u.Projeto, refs.Exibir(u.Ref))
	if rc != esperado || !dentro(rr, rc) {
		return false, fmt.Sprintf("%s resolve para %s, fora de %s", caminho, rc, esperado)
	}
	return true, ""
}

// removerArvore apaga a pasta e o que há nela. Pastas sem permissão de escrita (o cache de
// módulos do Go, por exemplo) ganham u+rwx antes. Links simbólicos são apagados, nunca seguidos.
func removerArvore(caminho string) error {
	err := os.RemoveAll(caminho)
	if err == nil {
		return nil
	}
	_ = filepath.WalkDir(caminho, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
	return os.RemoveAll(caminho)
}

func erroUso(cause string) error {
	return &output.Error{Code: "uso", Cause: cause, Exit: output.ExitUsage}
}

func contemStr(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// EsperaTrava é quanto o `recursos gravar` e o `recursos reabrir` esperam uma limpeza em curso.
const EsperaTrava = 2 * time.Minute

// Reabrir abre uma rodada de correção da US depois do teste. Sob a trava da US (espera uma
// limpeza em andamento terminar), sobe a geração do recursos.json e guarda as MRs de agora como
// encerradas; daí em diante a limpeza recusa até haver uma MR nova ligada, e o coordenador pode
// recriar o worktree e os panes sem que um `limpar` velho os alcance. Só depois deste comando
// se recria o worktree, preferindo um caminho novo (sufixo da rodada).
func (l *Limpador) Reabrir(ctx context.Context, coord *central.Coordenador, ref refs.US) (*Recursos, string, error) {
	loc, _, err := l.St.LocalizarUS(ctx, coord, ref)
	if err != nil {
		return nil, "", err
	}
	u := loc.US
	soltar, err := l.Raizes.TravarUS(u, EsperaTrava)
	if err != nil {
		return nil, "", err
	}
	defer soltar()
	_, us, err := l.St.LocalizarUS(ctx, coord, ref)
	if err != nil {
		return nil, "", err
	}
	if len(us.MRs) == 0 {
		return nil, "", erroUso(u.Chave() + " não tem MR ligada: não há o que reabrir")
	}
	rec, _, err := lerRecursosHash(l.Raizes.Arquivo(u), u)
	semArquivo := errors.Is(err, ErrSemRecursos)
	if semArquivo {
		// A limpeza completa apaga o arquivo: o que ele aprovou já passou pelo gate lá.
		rec, err = &Recursos{}, nil
	}
	if err != nil {
		return nil, "", &output.Error{Code: "recursos_invalido", Cause: err.Error(), Recovery: "corrija ou apague o arquivo", Exit: output.ExitConflict}
	}
	// Só se reabre depois do merge: cada MR ainda não encerrada precisa estar mesclada (com o head
	// aprovado registrado, se o arquivo existe) ou fechada, senão uma MR sem aprovação viraria
	// "encerrada" e escaparia do gate da limpeza.
	for _, m := range us.MRs {
		if contemStr(rec.MRsEncerradas, m.URL) {
			continue
		}
		d, err := l.F.ConsultarMRDetalhe(ctx, m.URL)
		if err != nil {
			return nil, "", &output.Error{Code: "mr_nao_conferida", Cause: fmt.Sprintf("não consegui conferir a MR %s: %v", m.URL, err), Recovery: "repita quando a forja responder", Exit: output.ExitConflict}
		}
		switch d.Estado {
		case "fechada":
		case "mesclada":
			if ap, ok := rec.Aprovados[m.URL]; !semArquivo && (!ok || d.Head == "" || !strings.HasPrefix(d.Head, ap)) {
				return nil, "", &output.Error{Code: "reabrir_recusado", Cause: fmt.Sprintf("a MR %s foi mesclada sem o head aprovado registrado (ou diferente dele)", m.URL), Recovery: "registre com orq recursos gravar --aprovado " + m.URL + "=<sha> se a aprovação existiu", Exit: output.ExitConflict}
			}
		default:
			return nil, "", &output.Error{Code: "reabrir_recusado", Cause: fmt.Sprintf("a MR %s está %s: só se reabre depois do merge", m.URL, d.Estado), Recovery: "espere o merge ou feche a MR", Exit: output.ExitConflict}
		}
	}
	rec.Geracao++
	rec.MRsEncerradas = nil
	for _, m := range us.MRs {
		rec.MRsEncerradas = append(rec.MRsEncerradas, m.URL)
	}
	sort.Strings(rec.MRsEncerradas)
	arquivo, err := rec.Gravar(l.Raizes, u)
	if err != nil {
		return nil, "", erroUso(err.Error())
	}
	err = l.St.RegistrarExternos(ctx, coord, u, []central.EventoExterno{{Tipo: "recursos.reabrir", Detalhe: map[string]any{"geracao": rec.Geracao, "mrs_encerradas": rec.MRsEncerradas}}})
	return rec, arquivo, err
}
