// Package herdrcfg acrescenta ao config.toml do Herdr o que o orq usa (as linhas de sidebar com
// os tokens $orq e $orq_mrs e as teclas das ações do plugin) e tira de novo, mexendo só no que é
// do orq. Aplicar faz a parte do arquivo: backup datado, escrita atômica e herdr config check,
// com o backup restaurado quando o check falha.
package herdrcfg

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// Tokens e ações do orq no config do Herdr.
const (
	TokenAgentes       = "$orq"     // [ui.sidebar.agents] rows
	TokenEspacos       = "$orq_mrs" // [ui.sidebar.spaces] rows
	AcaoPopup          = "orq.popup-todos"
	AcaoCentral        = "orq.abrir-central"
	TeclaPadrao        = "prefix+u"
	TeclaCentralPadrao = "prefix+shift+u"
	marcaComentario    = "# orq:"
)

// linhaSidebar é uma tabela de linhas da sidebar e o token que o orq põe nela.
type linhaSidebar struct {
	caminho string // "ui.sidebar.agents.rows"
	tabela  string
	token   string
	padrao  []any // rows padrão do Herdr 0.9.3, usadas quando o arquivo não tem rows
}

var linhas = []linhaSidebar{
	{"ui.sidebar.agents.rows", "ui.sidebar.agents", TokenAgentes, []any{[]any{"state_icon", "machine", "workspace", "tab"}, []any{"agent"}}},
	{"ui.sidebar.spaces.rows", "ui.sidebar.spaces", TokenEspacos, []any{[]any{"state_icon", "workspace"}, []any{"branch", "git_status"}}},
}

// teclasPadrao são as teclas que o Herdr 0.9.3 já usa (herdr --default-config): ação → tecla.
var teclasPadrao = map[string]string{
	"help": "prefix+?", "settings": "prefix+s", "detach": "prefix+q", "reload_config": "prefix+shift+r",
	"open_notification_target": "prefix+o", "workspace_picker": "prefix+w", "goto": "prefix+g",
	"new_workspace": "prefix+shift+n", "new_worktree": "prefix+shift+g", "rename_workspace": "prefix+shift+w",
	"close_workspace": "prefix+shift+d", "new_tab": "prefix+c", "rename_tab": "prefix+shift+t",
	"previous_tab": "prefix+p", "next_tab": "prefix+n", "close_tab": "prefix+shift+x",
	"rename_pane": "prefix+shift+p", "edit_scrollback": "prefix+e", "focus_pane_left": "prefix+h",
	"focus_pane_down": "prefix+j", "focus_pane_up": "prefix+k", "focus_pane_right": "prefix+l",
	"cycle_pane_next": "prefix+tab", "cycle_pane_previous": "prefix+shift+tab", "split_vertical": "prefix+v",
	"split_horizontal": "prefix+minus", "close_pane": "prefix+x", "zoom": "prefix+z", "resize_mode": "prefix+r",
	"toggle_sidebar": "prefix+b",
}

// Opcoes de Configurar. Tecla vazia não acrescenta aquela tecla.
type Opcoes struct {
	Tecla        string // popup-todos
	TeclaCentral string // abrir-central
}

// Erro é uma recusa do editor, com o que fazer.
type Erro struct {
	Codigo, Causa, Recuperacao string
}

func (e *Erro) Error() string { return e.Codigo + ": " + e.Causa }

func naoSuportado(causa string) error {
	return &Erro{Codigo: "config_nao_suportada", Causa: causa,
		Recuperacao: "edite o config.toml à mão: acrescente [{ token = \"$orq\" }] em [ui.sidebar.agents] rows, [{ token = \"$orq_mrs\" }] em [ui.sidebar.spaces] rows e um [[keys.command]] com type = \"plugin_action\" e command = \"orq.popup-todos\""}
}

// Situacao é o que o orq encontra no config (orq plugin doctor).
type Situacao struct {
	LinhaAgentes bool              `json:"linha_agentes"` // $orq em [ui.sidebar.agents] rows
	LinhaEspacos bool              `json:"linha_espacos"` // $orq_mrs em [ui.sidebar.spaces] rows
	Teclas       map[string]string `json:"teclas"`        // ação do orq → tecla
	Avisos       []string          `json:"avisos"`
}

// Ler devolve a situação do config.
func Ler(texto []byte) (Situacao, error) {
	m, err := decodificar(texto)
	if err != nil {
		return Situacao{}, err
	}
	s := Situacao{Teclas: map[string]string{}, Avisos: avisos(m)}
	for _, l := range linhas {
		rows, _ := pegar(m, l.caminho).([]any)
		tem := temToken(rows, l.token)
		if l.token == TokenAgentes {
			s.LinhaAgentes = tem
		} else {
			s.LinhaEspacos = tem
		}
	}
	for _, k := range comandos(m) {
		if c, _ := k["command"].(string); strings.HasPrefix(c, "orq.") {
			tecla, _ := k["key"].(string)
			s.Teclas[c] = tecla
		}
	}
	return s, nil
}

// avisos aponta o que esconde o token: rows_by_agent substitui rows para aquele agente.
func avisos(m map[string]any) []string {
	var av []string
	por, _ := pegar(m, "ui.sidebar.agents.rows_by_agent").(map[string]any)
	var nomes []string
	for k := range por {
		nomes = append(nomes, k)
	}
	sort.Strings(nomes)
	for _, k := range nomes {
		rows, _ := por[k].([]any)
		if !temToken(rows, TokenAgentes) {
			av = append(av, fmt.Sprintf("[ui.sidebar.agents.rows_by_agent] %s substitui rows e não tem %s: o resumo do orq não aparece nos panes %s", k, TokenAgentes, k))
		}
	}
	return av
}

// Configurar acrescenta as linhas e as teclas que faltam. Sem nada a fazer, devolve o texto
// igual e mudancas vazio.
func Configurar(texto []byte, o Opcoes) ([]byte, []string, error) {
	m, err := decodificar(texto)
	if err != nil {
		return nil, nil, err
	}
	d, err := analisar(string(texto))
	if err != nil {
		return nil, nil, naoSuportado("não consegui ler a estrutura do arquivo: " + err.Error())
	}
	var mud []string
	novo := string(texto)
	// As edições vão do fim para o começo, para os índices do lexer continuarem valendo.
	type edicao struct {
		ini, fim int
		texto    string
	}
	var eds []edicao
	var fimArquivo []string
	for _, l := range linhas {
		rows, tem := pegar(m, l.caminho).([]any)
		if tem && temToken(rows, l.token) {
			continue
		}
		linhaNova := []any{map[string]any{"token": l.token}}
		if tem {
			a, ok := achar(d, l.caminho)
			if !ok {
				return nil, nil, naoSuportado(l.caminho + " está numa forma que o orq não edita (tabela inline?)")
			}
			// A linha entra no texto do valor, antes do ']' final: comentários, quebras e a
			// forma do usuário ficam.
			v, err := inserirItem(d.texto[a.valIni:a.valFi], serializar(linhaNova))
			if err != nil {
				return nil, nil, naoSuportado(l.caminho + ": " + err.Error())
			}
			eds = append(eds, edicao{a.valIni, a.valFi, v})
		} else {
			val := "rows = " + serializar(append(append([]any(nil), l.padrao...), linhaNova)) + "\n"
			if c, ok := acharTabela(d, l.tabela); ok {
				eds = append(eds, edicao{c.fim, c.fim, val})
			} else {
				fimArquivo = append(fimArquivo, "["+l.tabela+"]\n"+val)
			}
		}
		mud = append(mud, fmt.Sprintf("%s: + [{ token = %q }]", l.caminho, l.token))
	}
	teclas := []struct{ acao, tecla, desc string }{
		{AcaoPopup, o.Tecla, "orq: Central de todos os coordenadores"},
		{AcaoCentral, o.TeclaCentral, "orq: abrir o workspace Central"},
	}
	cmds := comandos(m)
	for _, t := range teclas {
		if t.tecla == "" {
			continue
		}
		if k, ok := acharComando(cmds, t.acao); ok {
			_ = k // já configurada, com a tecla que o usuário deixou
			continue
		}
		if dono := donoTecla(m, cmds, t.tecla); dono != "" {
			return nil, nil, &Erro{Codigo: "tecla_ocupada", Causa: fmt.Sprintf("a tecla %s já é de %s", t.tecla, dono),
				Recuperacao: "escolha outra com --tecla ou --tecla-central (ex. prefix+alt+u), ou passe \"\" para não criar a tecla"}
		}
		fimArquivo = append(fimArquivo, fmt.Sprintf("%s tecla de %s (orq plugin configurar; orq plugin desconfigurar tira)\n[[keys.command]]\nkey = %s\ntype = \"plugin_action\"\ncommand = %s\ndescription = %s\n",
			marcaComentario, t.acao, quote(t.tecla), quote(t.acao), quote(t.desc)))
		mud = append(mud, fmt.Sprintf("keys.command: %s → %s", t.tecla, t.acao))
	}
	if len(mud) == 0 {
		return texto, nil, nil
	}
	sort.Slice(eds, func(i, j int) bool { return eds[i].ini > eds[j].ini })
	for _, e := range eds {
		novo = novo[:e.ini] + e.texto + novo[e.fim:]
	}
	if len(fimArquivo) > 0 {
		if novo != "" && !strings.HasSuffix(novo, "\n") {
			novo += "\n"
		}
		for _, b := range fimArquivo {
			if novo != "" {
				novo += "\n"
			}
			novo += b
		}
	}
	if err := conferir([]byte(novo), o); err != nil {
		return nil, nil, err
	}
	return []byte(novo), mud, nil
}

// Desconfigurar tira os tokens do orq das linhas da sidebar (e a linha que ficar vazia) e os
// blocos [[keys.command]] das ações orq.*, com o comentário "# orq:" logo acima.
func Desconfigurar(texto []byte) ([]byte, []string, error) {
	m, err := decodificar(texto)
	if err != nil {
		return nil, nil, err
	}
	d, err := analisar(string(texto))
	if err != nil {
		return nil, nil, naoSuportado("não consegui ler a estrutura do arquivo: " + err.Error())
	}
	type corte struct {
		ini, fim int
		texto    string
	}
	var cortes []corte
	var mud []string
	for _, l := range linhas {
		rows, tem := pegar(m, l.caminho).([]any)
		if !tem || !temToken(rows, l.token) {
			continue
		}
		a, ok := achar(d, l.caminho)
		if !ok {
			return nil, nil, naoSuportado(l.caminho + " está numa forma que o orq não edita (tabela inline?)")
		}
		// Sai só a linha que o configurar acrescenta, [{ token = "..." }], tirada do texto; o
		// token que o usuário pôs numa linha dele, ou noutra forma, fica.
		v := d.texto[a.valIni:a.valFi]
		tirou := false
		for {
			es, _, err := elementos(v)
			if err != nil {
				return nil, nil, naoSuportado(l.caminho + ": " + err.Error())
			}
			n := -1
			for i, e := range es {
				if linhaDoOrqTexto(v[e.ini:e.fim], l.token) {
					n = i
					break
				}
			}
			if n < 0 {
				break
			}
			v, tirou = removerItem(v, es, n), true
		}
		if !tirou {
			continue
		}
		cortes = append(cortes, corte{a.valIni, a.valFi, v})
		mud = append(mud, fmt.Sprintf("%s: - [{ token = %q }]", l.caminho, l.token))
	}
	// Os blocos [[keys.command]] do arquivo, na ordem, casam com o array decodificado.
	cmds := comandos(m)
	var blocos []int
	for i, c := range d.cabs {
		if c.vetor && c.nome == "keys.command" {
			blocos = append(blocos, i)
		}
	}
	temOrq := false
	for _, k := range cmds {
		if c, _ := k["command"].(string); strings.HasPrefix(c, "orq.") {
			temOrq = true
		}
	}
	if temOrq && len(blocos) != len(cmds) {
		return nil, nil, naoSuportado("keys.command está numa forma que o orq não edita (array inline?)")
	}
	manuais := 0
	for n, ci := range blocos {
		c, _ := cmds[n]["command"].(string)
		if !strings.HasPrefix(c, "orq.") {
			continue
		}
		ini, fim := d.cabs[ci].inicio, d.cabs[ci].fim
		for _, a := range d.atrs {
			if a.cab == ci && a.fim > fim {
				fim = a.fim
			}
		}
		// Só o bloco com a marca "# orq:" logo acima é do configurar; sem ela, a tecla é do
		// usuário, mesmo apontando uma ação do orq.
		comMarca := recuarComentario(d.texto, ini)
		if comMarca == ini {
			manuais++
			continue
		}
		cortes = append(cortes, corte{comMarca, fim, ""})
		tecla, _ := cmds[n]["key"].(string)
		mud = append(mud, fmt.Sprintf("keys.command: - %s → %s", tecla, c))
	}
	if len(mud) == 0 {
		return texto, nil, nil
	}
	sort.Slice(cortes, func(i, j int) bool { return cortes[i].ini > cortes[j].ini })
	novo := string(texto)
	for _, c := range cortes {
		novo = novo[:c.ini] + c.texto + novo[c.fim:]
	}
	if err := conferirSem([]byte(novo), manuais); err != nil {
		return nil, nil, err
	}
	return []byte(novo), mud, nil
}

// linhaDoOrqTexto diz se o texto de um item de rows é a linha que o configurar acrescenta:
// um array com um único { token = "<token>" }, sem estilo.
func linhaDoOrqTexto(item, token string) bool {
	var m map[string]any
	if toml.Unmarshal([]byte("x = "+item), &m) != nil {
		return false
	}
	return linhaDoOrq(m["x"], token)
}

func linhaDoOrq(r any, token string) bool {
	rr, ok := r.([]any)
	if !ok || len(rr) != 1 {
		return false
	}
	t, ok := rr[0].(map[string]any)
	if !ok || len(t) != 1 {
		return false
	}
	v, _ := t["token"].(string)
	return v == token
}

// conferirSem confere o resultado do desconfigurar: TOML válido, nenhuma linha do orq nas rows
// e só as teclas orq.* do usuário (sem a marca) restantes.
func conferirSem(texto []byte, manuais int) error {
	m, err := decodificar(texto)
	if err != nil {
		return naoSuportado("a edição gerou um TOML inválido (" + err.Error() + ")")
	}
	for _, l := range linhas {
		rows, _ := pegar(m, l.caminho).([]any)
		for _, r := range rows {
			if linhaDoOrq(r, l.token) {
				return naoSuportado("a edição não tirou a linha do orq de " + l.caminho)
			}
		}
	}
	n := 0
	for _, k := range comandos(m) {
		if c, _ := k["command"].(string); strings.HasPrefix(c, "orq.") {
			n++
		}
	}
	if n != manuais {
		return naoSuportado("a edição não deixou as teclas do orq como devia")
	}
	return nil
}

// recuarComentario inclui no corte as linhas "# orq:" logo acima do bloco e uma linha em branco
// antes delas.
func recuarComentario(s string, ini int) int {
	achou := false
	for ini > 0 {
		p := strings.LastIndexByte(s[:ini-1], '\n') + 1
		l := strings.TrimSpace(s[p : ini-1])
		switch {
		case strings.HasPrefix(l, marcaComentario):
			ini, achou = p, true
			continue
		case l == "" && achou:
			return p
		}
		return ini
	}
	return ini
}

// conferir decodifica o resultado e confere que a edição fez o que devia; o lexer pode errar
// num arquivo exótico, e um config quebrado não pode ir para o disco.
func conferir(texto []byte, o Opcoes) error {
	m, err := decodificar(texto)
	if err != nil {
		return naoSuportado("a edição gerou um TOML inválido (" + err.Error() + ")")
	}
	for _, l := range linhas {
		rows, _ := pegar(m, l.caminho).([]any)
		if !temToken(rows, l.token) {
			return naoSuportado("a edição não deixou " + l.caminho + " como devia")
		}
	}
	cmds := comandos(m)
	for _, a := range []string{AcaoPopup, AcaoCentral} {
		_, tem := acharComando(cmds, a)
		quer := (a == AcaoPopup && o.Tecla != "") || (a == AcaoCentral && o.TeclaCentral != "")
		if quer && !tem {
			return naoSuportado("a edição não deixou a tecla de " + a + " como devia")
		}
	}
	return nil
}

func decodificar(texto []byte) (map[string]any, error) {
	m := map[string]any{}
	if err := toml.Unmarshal(texto, &m); err != nil {
		return nil, &Erro{Codigo: "config_invalida", Causa: "o config.toml não é TOML válido: " + err.Error(), Recuperacao: "corrija o arquivo (herdr config check) e rode de novo"}
	}
	return m, nil
}

func pegar(m map[string]any, caminho string) any {
	var v any = m
	for _, p := range strings.Split(caminho, ".") {
		mm, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = mm[p]
	}
	return v
}

func achar(d *doc, caminho string) (atribuicao, bool) {
	for _, a := range d.atrs {
		if d.caminho(a) == caminho {
			return a, true
		}
	}
	return atribuicao{}, false
}

func acharTabela(d *doc, nome string) (cabecalho, bool) {
	for _, c := range d.cabs {
		if !c.vetor && c.nome == nome {
			return c, true
		}
	}
	return cabecalho{}, false
}

func comandos(m map[string]any) []map[string]any {
	var r []map[string]any
	switch v := pegar(m, "keys.command").(type) {
	case []any:
		for _, x := range v {
			if k, ok := x.(map[string]any); ok {
				r = append(r, k)
			} else {
				r = append(r, map[string]any{})
			}
		}
	case []map[string]any:
		r = v
	}
	return r
}

func acharComando(cmds []map[string]any, acao string) (map[string]any, bool) {
	for _, k := range cmds {
		if c, _ := k["command"].(string); c == acao {
			return k, true
		}
	}
	return nil, false
}

// donoTecla diz quem já usa a tecla: um [[keys.command]], uma ação em [keys] ou uma tecla
// padrão do Herdr que o arquivo não trocou.
func donoTecla(m map[string]any, cmds []map[string]any, tecla string) string {
	for _, k := range cmds {
		if t, _ := k["key"].(string); t == tecla {
			c, _ := k["command"].(string)
			return "[[keys.command]] " + c
		}
	}
	keys, _ := m["keys"].(map[string]any)
	for acao, v := range keys {
		switch x := v.(type) {
		case string:
			if x == tecla {
				return "keys." + acao
			}
		case []any:
			for _, e := range x {
				if s, _ := e.(string); s == tecla {
					return "keys." + acao
				}
			}
		}
	}
	for acao, t := range teclasPadrao {
		if _, trocada := keys[acao]; t == tecla && !trocada {
			return "keys." + acao + " (padrão do Herdr)"
		}
	}
	return ""
}

func ehToken(el any, token string) bool {
	switch x := el.(type) {
	case string:
		return x == token
	case map[string]any:
		t, _ := x["token"].(string)
		return t == token
	}
	return false
}

func temToken(rows []any, token string) bool {
	for _, r := range rows {
		rr, _ := r.([]any)
		for _, el := range rr {
			if ehToken(el, token) {
				return true
			}
		}
	}
	return false
}

// serializar escreve o valor em TOML numa linha: arrays, tabelas inline (token primeiro, o
// resto em ordem alfabética), strings, booleanos e números.
func serializar(v any) string {
	switch x := v.(type) {
	case []any:
		var ps []string
		for _, e := range x {
			ps = append(ps, serializar(e))
		}
		return "[" + strings.Join(ps, ", ") + "]"
	case map[string]any:
		var ks []string
		for k := range x {
			if k != "token" {
				ks = append(ks, k)
			}
		}
		sort.Strings(ks)
		if _, ok := x["token"]; ok {
			ks = append([]string{"token"}, ks...)
		}
		var ps []string
		for _, k := range ks {
			ps = append(ps, chaveTOML(k)+" = "+serializar(x[k]))
		}
		return "{ " + strings.Join(ps, ", ") + " }"
	case string:
		return quote(x)
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	default:
		return quote(fmt.Sprint(reflect.ValueOf(v).Interface()))
	}
}

func chaveTOML(k string) string {
	for _, r := range k {
		if !(r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return quote(k)
		}
	}
	if k == "" {
		return `""`
	}
	return k
}

// quote escreve uma string básica do TOML.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
