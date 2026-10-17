package cli

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/output"
)

// A grade da Central tem duas colunas e é preenchida por linha: o pane de posição i fica na
// linha i/2, coluna i%2. A posição é a ordem de criação e a grade é uma espinha de linhas
// (`down(linha1, down(linha2, ...))`), cada linha com um ou dois panes (`right(a, b)`).
//
// O `herdr pane split` e o `herdr pane move --split` só dividem um pane, e sem --target-pane o
// pane focado: não há como dividir a aba inteira. A linha de largura total sai de dividir para
// baixo o pane que está sozinho na linha de baixo, e por isso, antes de abrir uma linha, o
// pane da direita da linha de baixo vai para uma aba temporária (`pane move --new-tab`) e volta
// à direita do da esquerda (`pane move --tab --target-pane --split right`). O pane move dentro
// da mesma aba responde same_tab, então a aba temporária é o único caminho. Os processos não
// reiniciam ao mover.

type retangulo struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"width"`
	H int `json:"height"`
}

type layoutHerdr struct {
	Tab   string    `json:"tab_id"`
	Area  retangulo `json:"area"`
	Panes []struct {
		ID   string    `json:"pane_id"`
		Rect retangulo `json:"rect"`
	} `json:"panes"`
	Splits []struct {
		Direcao string    `json:"direction"`
		Ratio   float64   `json:"ratio"`
		Rect    retangulo `json:"rect"`
	} `json:"splits"`
}

// lerLayout devolve a árvore de divisões da aba do pane.
func lerLayout(ctx context.Context, f enriquecer.Ferramentas, pane string) (layoutHerdr, error) {
	var r struct {
		Layout layoutHerdr `json:"layout"`
	}
	err := herdrJSON(ctx, f, &r, "pane", "layout", "--pane", pane)
	return r.Layout, err
}

type grade struct {
	f        enriquecer.Ferramentas
	ws, tab  string
	flagsEnv []string
	panes    []string // pane de cada posição; só os panes com o rótulo da Central
}

func gradeErro(cause, recovery string) error {
	return &output.Error{Code: "herdr_falhou", Cause: cause, Recovery: recovery, Exit: output.ExitUnexpected}
}

// paneMovido lê o pane que um pane split ou pane move devolveu.
type paneMovido struct {
	Pane struct {
		ID string `json:"pane_id"`
	} `json:"pane"`
	Mover struct {
		Pane struct {
			ID string `json:"pane_id"`
		} `json:"pane"`
	} `json:"move_result"`
}

func (p paneMovido) id() string {
	if p.Mover.Pane.ID != "" {
		return p.Mover.Pane.ID
	}
	return p.Pane.ID
}

// tirar leva o pane para uma aba nova, sem foco.
func (g *grade) tirar(ctx context.Context, pane string) error {
	return herdrJSON(ctx, g.f, nil, "pane", "move", pane, "--new-tab", "--workspace", g.ws, "--no-focus")
}

// mover traz um pane de fora da aba da grade para o lado (right) ou para baixo (down) do alvo.
func (g *grade) mover(ctx context.Context, pane, alvo, dir string) (string, error) {
	var r paneMovido
	if err := herdrJSON(ctx, g.f, &r, "pane", "move", pane, "--tab", g.tab, "--target-pane", alvo, "--split", dir, "--no-focus"); err != nil {
		return "", err
	}
	if id := r.id(); id != "" {
		return id, nil
	}
	return pane, nil
}

// colocar põe um pane ao lado ou embaixo do alvo: um pane novo (existente vazio) ou um que já existe.
func (g *grade) colocar(ctx context.Context, existente, alvo, dir string) (string, error) {
	if existente != "" {
		return g.mover(ctx, existente, alvo, dir)
	}
	var r paneMovido
	args := append([]string{"pane", "split", alvo, "--direction", dir, "--no-focus"}, g.flagsEnv...)
	if err := herdrJSON(ctx, g.f, &r, args...); err != nil {
		return "", err
	}
	return r.id(), nil
}

// acrescentar põe um pane no fim da grade: o ímpar (posição par, a partir da 3ª) abre uma linha
// de largura total embaixo de tudo; o par entra à direita do anterior. Com existente vazio o
// pane é criado; senão é o pane que já existe e roda um processo. Devolve o id do pane.
func (g *grade) acrescentar(ctx context.Context, existente string) (string, error) {
	i := len(g.panes)
	if i == 0 {
		return "", gradeErro("grade sem pane de partida", "rode orq central abrir de novo")
	}
	var pane string
	var err error
	if i%2 == 1 {
		pane, err = g.colocar(ctx, existente, g.panes[i-1], "right")
	} else {
		parceiro, acima := g.panes[i-1], g.panes[i-2]
		if err := g.tirar(ctx, parceiro); err != nil {
			return "", err
		}
		pane, err = g.colocar(ctx, existente, acima, "down")
		// O parceiro volta mesmo que a criação tenha falhado: não deve ficar na aba temporária.
		if _, errV := g.mover(ctx, parceiro, acima, "right"); errV != nil {
			return "", gradeErro(
				fmt.Sprintf("o pane %s ficou numa aba temporária: %v", parceiro, errV),
				fmt.Sprintf("traga de volta com herdr pane move %s --tab %s --target-pane %s --split right e rode orq central abrir", parceiro, g.tab, acima))
		}
	}
	if err != nil {
		return "", err
	}
	g.panes = append(g.panes, pane)
	return pane, nil
}

// remover tira o pane da grade (fechado).
func (g *grade) remover(pane string) {
	for i, p := range g.panes {
		if p == pane {
			g.panes = append(g.panes[:i:i], g.panes[i+1:]...)
			return
		}
	}
}

// conferir lê a aba, ordena a grade pela posição real (linha, coluna) e a refaz se ela tem
// buraco ou foge do desenho. Com mudou (panes fechados ou refeitos), iguala as alturas.
func (g *grade) conferir(ctx context.Context, mudou bool) error {
	if len(g.panes) == 0 {
		return nil
	}
	l, err := lerLayout(ctx, g.f, g.panes[0])
	if err != nil {
		return err
	}
	rects := g.ordenar(l)
	if len(g.panes) < 2 {
		if mudou {
			return g.igualar(ctx)
		}
		return nil
	}
	k := prefixoCerto(l, rects, g.estranhos(l))
	if k < len(g.panes) {
		if err := g.refazer(ctx, k); err != nil {
			return err
		}
		mudou = true
	}
	if mudou {
		return g.igualar(ctx)
	}
	return nil
}

// ordenar põe g.panes em ordem de linha e coluna, a de leitura da grade, e devolve os retângulos.
func (g *grade) ordenar(l layoutHerdr) []retangulo {
	rect := map[string]retangulo{}
	for _, p := range l.Panes {
		rect[p.ID] = p.Rect
	}
	var vis []string
	for _, p := range g.panes {
		if _, ok := rect[p]; ok {
			vis = append(vis, p)
		}
	}
	sort.SliceStable(vis, func(a, b int) bool {
		ra, rb := rect[vis[a]], rect[vis[b]]
		if ra.Y != rb.Y {
			return ra.Y < rb.Y
		}
		return ra.X < rb.X
	})
	g.panes = vis
	rs := make([]retangulo, len(vis))
	for i, p := range vis {
		rs[i] = rect[p]
	}
	return rs
}

// estranhos diz se a aba tem pane fora da grade (de usuário): a árvore não é só dela.
func (g *grade) estranhos(l layoutHerdr) bool {
	return len(l.Panes) != len(g.panes)
}

// prefixoCerto devolve quantos panes do começo da grade já estão onde devem; len(rs) quando a
// grade toda está certa. Uma grade com a árvore fora da espinha (ex. colunas em vez de linhas)
// refaz de tudo, menos o primeiro pane.
func prefixoCerto(l layoutHerdr, rs []retangulo, estranhos bool) int {
	n := len(rs)
	if !estranhos && n > 2 && larguraTotal(l)+1 != (n+1)/2 {
		return 1
	}
	for k := n; k > 1; k-- {
		if gradeCerta(rs[:k], k == n) {
			return k
		}
	}
	return 1
}

// larguraTotal conta as divisões para baixo que cortam a aba de ponta a ponta: uma por linha menos a última.
func larguraTotal(l layoutHerdr) int {
	n := 0
	for _, s := range l.Splits {
		if s.Direcao == "down" && s.Rect.X == l.Area.X && s.Rect.W == l.Area.W {
			n++
		}
	}
	return n
}

// gradeCerta confere a geometria de rs, na ordem de leitura: cada linha com dois panes lado a
// lado e as linhas empilhadas com a mesma largura. Com completo, o pane sozinho do fim tem a
// largura da linha de cima; sem ele (prefixo de uma grade maior) a largura não conta.
func gradeCerta(rs []retangulo, completo bool) bool {
	perto := func(a, b int) bool { return a-b <= 1 && b-a <= 1 }
	n := len(rs)
	for i := 0; i < n; i += 2 {
		a := rs[i]
		if i > 0 && (a.Y <= rs[i-2].Y || !perto(a.X, rs[0].X)) {
			return false
		}
		if i+1 < n {
			b := rs[i+1]
			if b.Y != a.Y || b.X <= a.X {
				return false
			}
			if i > 0 && (!perto(b.X, rs[1].X) || !perto(b.X+b.W, rs[1].X+rs[1].W)) {
				return false
			}
		} else if completo && n > 1 && !perto(a.X+a.W, rs[1].X+rs[1].W) {
			return false
		}
	}
	return true
}

// refazer leva os panes da posição k em diante para abas temporárias e os recoloca em ordem.
func (g *grade) refazer(ctx context.Context, k int) error {
	fora := append([]string(nil), g.panes[k:]...)
	g.panes = g.panes[:k]
	for i, p := range fora {
		if err := g.tirar(ctx, p); err != nil {
			return g.restantes(err, fora[i:])
		}
	}
	for i, p := range fora {
		if _, err := g.acrescentar(ctx, p); err != nil {
			return g.restantes(err, fora[i:])
		}
	}
	return nil
}

func (g *grade) restantes(err error, panes []string) error {
	return gradeErro(
		fmt.Sprintf("a grade ficou pela metade; panes possivelmente em abas temporárias: %s (%v)", strings.Join(panes, ", "), err),
		fmt.Sprintf("traga cada um de volta com herdr pane move <pane> --tab %s --target-pane <pane da grade> --split right", g.tab))
}

// igualar deixa as linhas com a mesma altura: a divisão j de m linhas fica com 1/(m-j) da área
// que sobra. O pane resize soma ao ratio da divisão (up subtrai), a partir do pane do canto de cima.
func (g *grade) igualar(ctx context.Context) error {
	if len(g.panes) < 3 {
		return nil
	}
	l, err := lerLayout(ctx, g.f, g.panes[0])
	if err != nil {
		return err
	}
	type linha struct {
		ratio float64
		r     retangulo
	}
	// Só as divisões dentro da região dos panes da grade: a que reserva espaço a um pane de fora
	// (um shell acima, por exemplo) não é da grade.
	noGrade := map[string]bool{}
	for _, p := range g.panes {
		noGrade[p] = true
	}
	var reg retangulo
	primeiro := true
	for _, p := range l.Panes {
		if !noGrade[p.ID] {
			continue
		}
		r := p.Rect
		if primeiro {
			reg, primeiro = r, false
			continue
		}
		x0, y0 := min(reg.X, r.X), min(reg.Y, r.Y)
		x1, y1 := max(reg.X+reg.W, r.X+r.W), max(reg.Y+reg.H, r.Y+r.H)
		reg = retangulo{x0, y0, x1 - x0, y1 - y0}
	}
	var ls []linha
	for _, s := range l.Splits {
		r := s.Rect
		if s.Direcao == "down" && r.X == reg.X && r.W == reg.W && r.Y >= reg.Y && r.Y+r.H <= reg.Y+reg.H {
			ls = append(ls, linha{s.Ratio, s.Rect})
		}
	}
	sort.Slice(ls, func(a, b int) bool { return ls[a].r.Y < ls[b].r.Y })
	m := len(ls) + 1
	for j, s := range ls {
		delta := 1/float64(m-j) - s.ratio
		if math.Abs(delta) < 0.01 {
			continue
		}
		pane := ""
		for _, p := range l.Panes {
			if noGrade[p.ID] && p.Rect.X == s.r.X && p.Rect.Y == s.r.Y {
				pane = p.ID
			}
		}
		if pane == "" {
			continue
		}
		dir := "down"
		if delta < 0 {
			dir, delta = "up", -delta
		}
		if err := herdrJSON(ctx, g.f, nil, "pane", "resize", "--pane", pane, "--direction", dir, "--amount", strconv.FormatFloat(delta, 'f', 4, 64)); err != nil {
			return err
		}
	}
	return nil
}
