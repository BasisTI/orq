package herdrcfg

import (
	"fmt"
	"strings"
)

// O editor mexe no texto do config.toml, não num documento reserializado: comentários, ordem e
// formatação do usuário ficam como estão. O lexer abaixo só acha o que o editor precisa: os
// cabeçalhos de tabela e, em cada atribuição, a chave e onde o valor começa e termina. A
// validação de verdade é o go-toml (antes e depois da edição) e o herdr config check.

// cabecalho é uma linha [tabela] ou [[tabela]].
type cabecalho struct {
	nome        string // normalizado: "keys.command"
	vetor       bool   // [[...]]
	inicio, fim int    // a linha inteira, com o \n
}

// atribuicao é chave = valor no nível de cima (fora de array e de tabela inline).
type atribuicao struct {
	tabela        string // nome normalizado da tabela em que está ("" na raiz)
	cab           int    // índice do cabeçalho em que está; -1 na raiz
	chave         string // normalizada, relativa à tabela
	inicio, fim   int    // a linha, do primeiro caractere ao \n inclusive
	valIni, valFi int    // o valor
}

type doc struct {
	texto string
	cabs  []cabecalho
	atrs  []atribuicao
}

func (d *doc) caminho(a atribuicao) string {
	if a.tabela == "" {
		return a.chave
	}
	return a.tabela + "." + a.chave
}

func analisar(texto string) (*doc, error) {
	d := &doc{texto: texto}
	s := texto
	i := 0
	tabela, cab := "", -1
	for i < len(s) {
		ini := i
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		switch {
		case i >= len(s):
		case s[i] == '\n' || s[i] == '\r':
			i++
			continue
		case s[i] == '#':
			i = fimLinha(s, i)
			continue
		case s[i] == '[':
			vetor := strings.HasPrefix(s[i:], "[[")
			j := i + 1
			if vetor {
				j++
			}
			k, err := lerChave(s, j, ']')
			if err != nil {
				return nil, err
			}
			nome := normalizarChave(s[j:k])
			k++
			if vetor {
				if k >= len(s) || s[k] != ']' {
					return nil, fmt.Errorf("cabeçalho [[%s] sem ]]", nome)
				}
				k++
			}
			fim := fimLinha(s, k)
			d.cabs = append(d.cabs, cabecalho{nome: nome, vetor: vetor, inicio: ini, fim: fim})
			tabela, cab = nome, len(d.cabs)-1
			i = fim
			continue
		default:
			k, err := lerChave(s, i, '=')
			if err != nil {
				return nil, err
			}
			chave := normalizarChave(s[i:k])
			v := k + 1
			for v < len(s) && (s[v] == ' ' || s[v] == '\t') {
				v++
			}
			vf, err := fimValor(s, v)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", chave, err)
			}
			fim := fimLinha(s, vf)
			d.atrs = append(d.atrs, atribuicao{tabela: tabela, cab: cab, chave: chave, inicio: ini, fim: fim, valIni: v, valFi: vf})
			i = fim
			continue
		}
		i++
	}
	return d, nil
}

// fimLinha devolve o índice logo depois do \n da linha de i (ou o fim do texto).
func fimLinha(s string, i int) int {
	if j := strings.IndexByte(s[i:], '\n'); j >= 0 {
		return i + j + 1
	}
	return len(s)
}

// lerChave anda de i até o delimitador (fora de aspas) e devolve a posição dele.
func lerChave(s string, i int, delim byte) (int, error) {
	for i < len(s) {
		switch c := s[i]; {
		case c == delim:
			return i, nil
		case c == '"' || c == '\'':
			j, err := fimString(s, i)
			if err != nil {
				return 0, err
			}
			i = j
			continue
		case c == '\n':
			return 0, fmt.Errorf("linha sem %q: %q", delim, strings.TrimSpace(s[max(0, i-40):i]))
		}
		i++
	}
	return 0, fmt.Errorf("fim do arquivo procurando %q", delim)
}

// normalizarChave tira espaços em volta dos pontos e as aspas de partes simples.
func normalizarChave(k string) string {
	var partes []string
	for _, p := range dividirChave(k) {
		p = strings.TrimSpace(p)
		if len(p) >= 2 && (p[0] == '"' || p[0] == '\'') && p[len(p)-1] == p[0] {
			p = p[1 : len(p)-1]
		}
		partes = append(partes, p)
	}
	return strings.Join(partes, ".")
}

func dividirChave(k string) []string {
	var partes []string
	ini := 0
	for i := 0; i < len(k); i++ {
		switch k[i] {
		case '"', '\'':
			if j, err := fimString(k, i); err == nil {
				i = j - 1
			}
		case '.':
			partes = append(partes, k[ini:i])
			ini = i + 1
		}
	}
	return append(partes, k[ini:])
}

// fimString devolve o índice depois do fechamento da string que abre em i.
func fimString(s string, i int) (int, error) {
	q := s[i]
	if strings.HasPrefix(s[i:], strings.Repeat(string(q), 3)) {
		j := strings.Index(s[i+3:], strings.Repeat(string(q), 3))
		if j < 0 {
			return 0, fmt.Errorf("string multilinha sem fim")
		}
		k := i + 3 + j + 3
		for k < len(s) && s[k] == q { // até duas aspas a mais fazem parte do conteúdo
			k++
		}
		return k, nil
	}
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			if q == '"' {
				j++
			}
		case q:
			return j + 1, nil
		case '\n':
			return 0, fmt.Errorf("string sem fim na linha")
		}
	}
	return 0, fmt.Errorf("string sem fim")
}

// fimValor devolve o índice logo depois do valor que começa em i.
func fimValor(s string, i int) (int, error) {
	if i >= len(s) {
		return 0, fmt.Errorf("valor ausente")
	}
	switch s[i] {
	case '"', '\'':
		return fimString(s, i)
	case '[', '{':
		prof := 0
		for j := i; j < len(s); j++ {
			switch s[j] {
			case '"', '\'':
				k, err := fimString(s, j)
				if err != nil {
					return 0, err
				}
				j = k - 1
			case '#':
				j = fimLinha(s, j) - 1
			case '[', '{':
				prof++
			case ']', '}':
				prof--
				if prof == 0 {
					return j + 1, nil
				}
			}
		}
		return 0, fmt.Errorf("array ou tabela sem fechamento")
	default:
		j := i
		for j < len(s) && s[j] != '\n' && s[j] != '\r' && s[j] != '#' {
			j++
		}
		for j > i && (s[j-1] == ' ' || s[j-1] == '\t') {
			j--
		}
		return j, nil
	}
}

// elemento é um item de um array TOML, com posições relativas ao texto do array.
type elemento struct {
	ini, fim int // o valor do item
	virgula  int // posição da vírgula que vem depois dele; -1 sem vírgula
}

// elementos separa os itens do array em v (que começa em '[' e termina no ']' dele) e devolve
// também a posição do ']' final.
func elementos(v string) ([]elemento, int, error) {
	var es []elemento
	i := 1
	for {
		i = pularBranco(v, i)
		if i >= len(v) {
			return nil, 0, fmt.Errorf("array sem fechamento")
		}
		if v[i] == ']' {
			return es, i, nil
		}
		ini := i
		var fim int
		switch v[i] {
		case '[', '{', '"', '\'':
			f, err := fimValor(v, i)
			if err != nil {
				return nil, 0, err
			}
			fim = f
		default:
			fim = i
			for fim < len(v) && !strings.ContainsRune(",]\n\r#", rune(v[fim])) {
				fim++
			}
			for fim > i && (v[fim-1] == ' ' || v[fim-1] == '\t') {
				fim--
			}
		}
		e := elemento{ini: ini, fim: fim, virgula: -1}
		i = pularBranco(v, fim)
		if i < len(v) && v[i] == ',' {
			e.virgula = i
			i++
		}
		es = append(es, e)
	}
}

// pularBranco anda sobre espaços, quebras de linha e comentários.
func pularBranco(v string, i int) int {
	for i < len(v) {
		switch v[i] {
		case ' ', '\t', '\n', '\r':
			i++
		case '#':
			i = fimLinha(v, i)
		default:
			return i
		}
	}
	return i
}

// inicioLinha e fimLinhaSem dão os limites da linha de i, sem o \n.
func inicioLinha(s string, i int) int { return strings.LastIndexByte(s[:i], '\n') + 1 }

func fimLinhaSem(s string, i int) int {
	if j := strings.IndexByte(s[i:], '\n'); j >= 0 {
		return i + j
	}
	return len(s)
}

// soBrancoAntes diz se, na linha de i, só há espaço antes de i.
func soBrancoAntes(s string, i int) bool {
	return strings.TrimLeft(s[inicioLinha(s, i):i], " \t") == ""
}

// inserirItem acrescenta item ao fim do array v sem tocar no resto do texto: na mesma linha
// num array de uma linha; numa linha própria, com a indentação do último item e respeitando a
// vírgula final, num array de várias linhas.
func inserirItem(v, item string) (string, error) {
	es, fecha, err := elementos(v)
	if err != nil {
		return "", err
	}
	if len(es) == 0 {
		return v[:fecha] + item + v[fecha:], nil
	}
	u := es[len(es)-1]
	ultimo := u.fim
	if u.virgula >= 0 {
		ultimo = u.virgula
	}
	if !strings.Contains(v[ultimo:fecha], "\n") {
		// O ']' está na mesma linha do último item (inclusive o array de uma linha só).
		if u.virgula >= 0 {
			return v[:u.virgula+1] + " " + item + v[u.virgula+1:], nil
		}
		return v[:u.fim] + ", " + item + v[u.fim:], nil
	}
	ini := inicioLinha(v, u.ini)
	recuo := v[ini:u.ini]
	if strings.TrimLeft(recuo, " \t") != "" {
		recuo = "  "
	}
	fl := fimLinhaSem(v, ultimo) + 1 // depois do \n da linha do último item
	if u.virgula >= 0 {
		return v[:fl] + recuo + item + ",\n" + v[fl:], nil
	}
	return v[:u.fim] + "," + v[u.fim:fl] + recuo + item + "\n" + v[fl:], nil
}

// removerItem tira o item e do array v: a linha inteira quando ele está sozinho nela, senão o
// item com a vírgula que o separa do anterior (ou do seguinte, se for o primeiro).
func removerItem(v string, es []elemento, n int) string {
	e := es[n]
	fimItem := e.fim
	if e.virgula >= 0 {
		fimItem = e.virgula + 1
	}
	if soBrancoAntes(v, e.ini) && strings.TrimSpace(stripComentario(v[fimItem:fimLinhaSem(v, fimItem)])) == "" {
		ini, fim := inicioLinha(v, e.ini), fimLinhaSem(v, fimItem)+1
		if fim > len(v) {
			fim = len(v)
		}
		out := v[:ini] + v[fim:]
		// Era o último item, sem vírgula: a vírgula do anterior foi a que o orq pôs.
		if e.virgula < 0 && n > 0 && es[n-1].virgula >= 0 {
			c := es[n-1].virgula
			out = out[:c] + out[c+1:]
		}
		return out
	}
	if n > 0 && es[n-1].virgula >= 0 {
		return v[:es[n-1].virgula] + v[e.fim:]
	}
	fim := fimItem
	for fim < len(v) && (v[fim] == ' ' || v[fim] == '\t') {
		fim++
	}
	return v[:e.ini] + v[fim:]
}

func stripComentario(s string) string {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		return s[:i]
	}
	return s
}
