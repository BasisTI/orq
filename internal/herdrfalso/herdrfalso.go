// Package herdrfalso é um herdr falso para os testes de orq central abrir: guarda a árvore de
// divisões de cada aba num JSON e responde aos comandos que o orq usa, com as regras do Herdr
// 0.9.3 que importam (pane split e pane move dividem o pane alvo, e sem alvo o primeiro da aba;
// pane move --new-tab tira o pane da árvore; a aba que fica vazia some; pane move dentro da
// mesma aba responde same_tab). Não entra no binário orq.
package herdrfalso

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	AreaLargura = 200
	AreaAltura  = 120
)

type No struct {
	Pane    string  `json:"pane,omitempty"`
	Direcao string  `json:"direcao,omitempty"` // right ou down
	Ratio   float64 `json:"ratio,omitempty"`
	A       *No     `json:"a,omitempty"`
	B       *No     `json:"b,omitempty"`
}

type Pane struct {
	Rotulo string   `json:"rotulo"`
	Argv   []string `json:"argv,omitempty"` // processo em primeiro plano; vazio é o shell
}

type Estado struct {
	Workspaces map[string]string `json:"workspaces"` // id → rótulo
	Abas       map[string]*No    `json:"abas"`       // aba → árvore
	AbaWS      map[string]string `json:"aba_ws"`
	Panes      map[string]*Pane  `json:"panes"`
	ProxPane   int               `json:"prox_pane"`
	ProxAba    int               `json:"prox_aba"`
	TUINaoSobe bool              `json:"tui_nao_sobe"`
}

func (e *Estado) Salvar(dir string) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "estado.json"), b, 0o644)
}

func Ler(dir string) (*Estado, error) {
	e := &Estado{Workspaces: map[string]string{}, Abas: map[string]*No{}, AbaWS: map[string]string{}, Panes: map[string]*Pane{}, ProxPane: 1, ProxAba: 1}
	b, err := os.ReadFile(filepath.Join(dir, "estado.json"))
	if os.IsNotExist(err) {
		return e, nil
	}
	if err != nil {
		return nil, err
	}
	return e, json.Unmarshal(b, e)
}

func (n *No) Folhas(out *[]string) {
	if n == nil {
		return
	}
	if n.Pane != "" {
		*out = append(*out, n.Pane)
		return
	}
	n.A.Folhas(out)
	n.B.Folhas(out)
}

// retirar tira a folha da árvore: o irmão ocupa o lugar do pai. Devolve a nova raiz.
func Retirar(n *No, pane string) *No {
	if n == nil {
		return nil
	}
	if n.Pane != "" {
		if n.Pane == pane {
			return nil
		}
		return n
	}
	a, b := Retirar(n.A, pane), Retirar(n.B, pane)
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	n.A, n.B = a, b
	return n
}

// dividir troca a folha alvo por uma divisão com o alvo em A e o novo em B.
func Dividir(n *No, alvo, novo, dir string, ratio float64) bool {
	if n.Pane != "" {
		if n.Pane != alvo {
			return false
		}
		a := &No{Pane: n.Pane}
		*n = No{Direcao: dir, Ratio: ratio, A: a, B: &No{Pane: novo}}
		return true
	}
	return Dividir(n.A, alvo, novo, dir, ratio) || Dividir(n.B, alvo, novo, dir, ratio)
}

func (e *Estado) AbaDe(pane string) string {
	for aba, raiz := range e.Abas {
		var fs []string
		raiz.Folhas(&fs)
		for _, f := range fs {
			if f == pane {
				return aba
			}
		}
	}
	return ""
}

type Ret struct{ X, Y, W, H int }

func (e *Estado) Rects(n *No, r Ret, panes map[string]Ret, splits *[]map[string]any) {
	if n.Pane != "" {
		panes[n.Pane] = r
		return
	}
	var ra, rb Ret
	if n.Direcao == "right" {
		w := int(float64(r.W)*n.Ratio + 0.5)
		ra, rb = Ret{r.X, r.Y, w, r.H}, Ret{r.X + w, r.Y, r.W - w, r.H}
	} else {
		h := int(float64(r.H)*n.Ratio + 0.5)
		ra, rb = Ret{r.X, r.Y, r.W, h}, Ret{r.X, r.Y + h, r.W, r.H - h}
	}
	*splits = append(*splits, map[string]any{"id": fmt.Sprintf("split_%d", len(*splits)), "direction": n.Direcao, "ratio": n.Ratio, "rect": rj(r)})
	e.Rects(n.A, ra, panes, splits)
	e.Rects(n.B, rb, panes, splits)
}

func rj(r Ret) map[string]int {
	return map[string]int{"x": r.X, "y": r.Y, "width": r.W, "height": r.H}
}

func (e *Estado) layout(aba string) map[string]any {
	panes := map[string]Ret{}
	splits := []map[string]any{}
	e.Rects(e.Abas[aba], Ret{0, 0, AreaLargura, AreaAltura}, panes, &splits)
	var ids []string
	e.Abas[aba].Folhas(&ids)
	lp := []map[string]any{}
	for _, id := range ids {
		lp = append(lp, map[string]any{"pane_id": id, "rect": rj(panes[id])})
	}
	return map[string]any{"tab_id": aba, "workspace_id": e.AbaWS[aba], "area": rj(Ret{0, 0, AreaLargura, AreaAltura}), "panes": lp, "splits": splits}
}

func (e *Estado) novoPane(ws string) string {
	e.ProxPane++
	return fmt.Sprintf("%s:p%d", ws, e.ProxPane)
}

func opcao(args []string, nome string) string {
	for i, a := range args {
		if a == nome && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// desaspar desfaz comandoShell: palavras entre aspas simples.
func desaspar(s string) []string {
	var ps []string
	var cur strings.Builder
	dentro := false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\'' && !dentro:
			dentro = true
		case s[i] == '\'' && dentro:
			dentro = false
		case s[i] == '\\' && !dentro && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
		case s[i] == ' ' && !dentro:
			if cur.Len() > 0 {
				ps = append(ps, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(s[i])
		}
	}
	if cur.Len() > 0 {
		ps = append(ps, cur.String())
	}
	return ps
}

func ratioDe(args []string) float64 {
	if v := opcao(args, "--ratio"); v != "" {
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	return 0.5
}

// Main executa uma chamada do herdr falso; devolve o código de saída.
// Main executa uma chamada do herdr falso, com o estado e o log em dir; devolve o código de saída.
func Main(args []string, dir string) int {
	f, _ := os.OpenFile(filepath.Join(dir, "log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	fmt.Fprintln(f, strings.Join(args, "|")+"|")
	f.Close()
	e, err := Ler(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 90
	}
	erro := func(code, msg string) int {
		b, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code, "message": msg}})
		fmt.Println(string(b))
		return 1
	}
	res := func(v any) int {
		if err := e.Salvar(dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 90
		}
		b, _ := json.Marshal(map[string]any{"result": v})
		fmt.Println(string(b))
		return 0
	}
	if len(args) < 2 {
		return res(map[string]any{})
	}
	rest := args[2:]
	switch args[0] + " " + args[1] {
	case "workspace list":
		var ws []map[string]string
		ids := make([]string, 0, len(e.Workspaces))
		for id := range e.Workspaces {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			ws = append(ws, map[string]string{"workspace_id": id, "label": e.Workspaces[id]})
		}
		return res(map[string]any{"workspaces": ws})
	case "workspace create":
		e.Workspaces["wT"] = opcao(rest, "--label")
		e.ProxPane = 1
		aba := "wT:t1"
		e.Abas[aba] = &No{Pane: "wT:p1"}
		e.AbaWS[aba] = "wT"
		e.ProxAba = 2
		e.Panes["wT:p1"] = &Pane{}
		return res(map[string]any{"workspace": map[string]string{"workspace_id": "wT"}, "tab": map[string]string{"tab_id": aba}, "root_pane": map[string]string{"pane_id": "wT:p1"}})
	case "workspace focus":
		return res(map[string]any{})
	case "pane list":
		ws := opcao(rest, "--workspace")
		var abas []string
		for aba := range e.Abas {
			if e.AbaWS[aba] == ws {
				abas = append(abas, aba)
			}
		}
		sort.Strings(abas)
		lp := []map[string]string{}
		for _, aba := range abas {
			var ids []string
			e.Abas[aba].Folhas(&ids)
			for _, id := range ids {
				lp = append(lp, map[string]string{"pane_id": id, "label": e.Panes[id].Rotulo, "tab_id": aba})
			}
		}
		return res(map[string]any{"panes": lp})
	case "pane layout":
		aba := e.AbaDe(opcao(rest, "--pane"))
		if aba == "" {
			return erro("pane_not_found", "pane")
		}
		return res(map[string]any{"layout": e.layout(aba), "type": "pane_layout"})
	case "pane split":
		alvo := rest[0]
		aba := e.AbaDe(alvo)
		if aba == "" {
			return erro("pane_not_found", alvo)
		}
		novo := e.novoPane(e.AbaWS[aba])
		Dividir(e.Abas[aba], alvo, novo, opcao(rest, "--direction"), ratioDe(rest))
		e.Panes[novo] = &Pane{}
		return res(map[string]any{"pane": map[string]string{"pane_id": novo}})
	case "pane move":
		pane := rest[0]
		de := e.AbaDe(pane)
		if de == "" {
			return erro("pane_not_found", pane)
		}
		ws := e.AbaWS[de]
		var destino string
		if contem(rest, "--new-tab") {
			e.ProxAba++
			destino = fmt.Sprintf("%s:t%d", ws, e.ProxAba)
		} else {
			destino = opcao(rest, "--tab")
			if destino == de {
				return erro("same_tab", "o pane já está nessa aba")
			}
			if e.Abas[destino] == nil {
				return erro("tab_not_found", destino)
			}
		}
		e.Abas[de] = Retirar(e.Abas[de], pane)
		if e.Abas[de] == nil {
			delete(e.Abas, de)
			delete(e.AbaWS, de)
		}
		if contem(rest, "--new-tab") {
			e.Abas[destino] = &No{Pane: pane}
			e.AbaWS[destino] = ws
		} else {
			alvo := opcao(rest, "--target-pane")
			if alvo == "" {
				var fs []string
				e.Abas[destino].Folhas(&fs)
				alvo = fs[0] // o pane focado: o primeiro
			}
			if !Dividir(e.Abas[destino], alvo, pane, opcao(rest, "--split"), ratioDe(rest)) {
				return erro("target_not_found", alvo)
			}
		}
		return res(map[string]any{"move_result": map[string]any{"changed": true, "pane": map[string]string{"pane_id": pane, "tab_id": destino}, "previous_pane_id": pane}})
	case "pane resize":
		pane := opcao(rest, "--pane")
		aba := e.AbaDe(pane)
		if aba == "" {
			return erro("pane_not_found", pane)
		}
		quantia, _ := strconv.ParseFloat(opcao(rest, "--amount"), 64)
		dir := opcao(rest, "--direction")
		eixo := "right"
		if dir == "up" || dir == "down" {
			eixo = "down"
		}
		// O ancestral mais próximo no eixo: a recursão volta das folhas para a raiz, e o primeiro que casa é o mais próximo.
		var no *No
		var acha func(n *No) bool
		acha = func(n *No) bool {
			if n.Pane != "" {
				return n.Pane == pane
			}
			if acha(n.A) || acha(n.B) {
				if no == nil && n.Direcao == eixo {
					no = n
				}
				return true
			}
			return false
		}
		acha(e.Abas[aba])
		if no == nil {
			return erro("cannot_resize", "sem divisão")
		}
		if dir == "up" || dir == "left" {
			quantia = -quantia
		}
		no.Ratio = max(0.1, min(0.9, no.Ratio+quantia))
		return res(map[string]any{})
	case "pane close":
		pane := rest[0]
		aba := e.AbaDe(pane)
		if aba == "" {
			return erro("pane_not_found", pane)
		}
		e.Abas[aba] = Retirar(e.Abas[aba], pane)
		if e.Abas[aba] == nil {
			delete(e.Abas, aba)
			delete(e.AbaWS, aba)
		}
		delete(e.Panes, pane)
		return res(map[string]any{})
	case "pane rename":
		e.Panes[rest[0]].Rotulo = rest[1]
		return res(map[string]any{})
	case "pane run":
		if !e.TUINaoSobe {
			e.Panes[rest[0]].Argv = desaspar(rest[1])
		}
		return res(map[string]any{})
	case "pane process-info":
		pane := opcao(rest, "--pane")
		p := e.Panes[pane]
		if p == nil {
			return erro("pane_not_found", pane)
		}
		grupo, pid, argv := 10, 10, p.Argv
		if len(argv) == 0 {
			argv = []string{"/usr/bin/zsh"}
		} else {
			grupo, pid = 20, 20
		}
		return res(map[string]any{"process_info": map[string]any{"shell_pid": 10, "foreground_process_group_id": grupo, "foreground_processes": []map[string]any{{"pid": pid, "argv": argv}}}})
	}
	return res(map[string]any{})
}

func contem(args []string, nome string) bool {
	for _, a := range args {
		if a == nome {
			return true
		}
	}
	return false
}
