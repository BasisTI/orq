// Package pendencias lê o arquivo de pendência que o worker grava, importa-o para a Central
// (no daemon, a cada ciclo, e em orq pend importar) e avisa o coordenador da US.
package pendencias

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/output"
	"go.yaml.in/yaml/v3"
)

// folgaFrontmatter é quanto o arquivo pode passar do LimiteDetalhe: o limite vale para o
// markdown depois do frontmatter, e as opções moram no frontmatter.
const folgaFrontmatter = 32 << 10

// LimiteArquivo é o maior arquivo que o importador lê.
const LimiteArquivo = central.LimiteDetalhe + folgaFrontmatter

// Arquivo é a pendência lida de um arquivo válido.
type Arquivo struct {
	Tipo, Titulo, Autor string
	Opcoes              []central.Opcao
	// Detalhe é o markdown depois do frontmatter, com as seções do modelo.
	Detalhe string
}

// Problema é um motivo de recusa, com a linha do arquivo quando há uma.
type Problema struct {
	Linha  int
	Motivo string
}

func (p Problema) String() string {
	if p.Linha > 0 {
		return fmt.Sprintf("linha %d: %s", p.Linha, p.Motivo)
	}
	return p.Motivo
}

// ErroArquivo reúne os motivos pelos quais o arquivo não vira pendência.
type ErroArquivo struct{ Problemas []Problema }

func (e *ErroArquivo) Error() string {
	ss := make([]string, len(e.Problemas))
	for i, p := range e.Problemas {
		ss[i] = p.String()
	}
	return strings.Join(ss, "; ")
}

func recusa(linha int, formato string, args ...any) *ErroArquivo {
	return &ErroArquivo{Problemas: []Problema{{Linha: linha, Motivo: fmt.Sprintf(formato, args...)}}}
}

type frontmatter struct {
	Tipo   string `yaml:"tipo"`
	Titulo string `yaml:"titulo"`
	Autor  string `yaml:"autor"`
	Opcoes []struct {
		ID          int    `yaml:"id"`
		Titulo      string `yaml:"titulo"`
		Descricao   string `yaml:"descricao"`
		Recomendada bool   `yaml:"recomendada"`
	} `yaml:"opcoes"`
}

// secoes são as seções que todo arquivo traz, na forma normalizada (veja chaveSecao).
var secoes = []struct{ chave, titulo string }{
	{"contexto", "Contexto"},
	{"pergunta", "Pergunta"},
	{"o que fica bloqueado", "O que fica bloqueado"},
	{"evidencias", "Evidências"},
}

var linhaDoYAML = regexp.MustCompile(`line (\d+)`)

// Ler interpreta o conteúdo de um arquivo de pendência (frontmatter YAML e markdown) e confere
// tudo o que a Central exige. Os problemas voltam todos de uma vez, com a linha.
func Ler(conteudo []byte) (*Arquivo, error) {
	if len(conteudo) > LimiteArquivo {
		return nil, recusa(0, "arquivo com %d bytes; o máximo é %d", len(conteudo), LimiteArquivo)
	}
	if bytes.ContainsRune(conteudo, 0) {
		return nil, recusa(0, "arquivo com byte nulo")
	}
	texto := strings.ReplaceAll(string(conteudo), "\r\n", "\n")
	linhas := strings.Split(texto, "\n")
	if strings.TrimSpace(linhas[0]) != "---" {
		return nil, recusa(1, "o arquivo precisa começar com `---` e o frontmatter (tipo, titulo, autor, opcoes)")
	}
	fim := -1
	for i := 1; i < len(linhas); i++ {
		if strings.TrimRight(linhas[i], " \t") == "---" {
			fim = i
			break
		}
	}
	if fim < 0 {
		return nil, recusa(1, "frontmatter sem o `---` que o fecha")
	}
	var fm frontmatter
	dec := yaml.NewDecoder(strings.NewReader(strings.Join(linhas[1:fim], "\n")))
	dec.KnownFields(true)
	if err := dec.Decode(&fm); err != nil {
		linha := 1
		if m := linhaDoYAML.FindStringSubmatch(err.Error()); m != nil {
			n, _ := strconv.Atoi(m[1])
			linha = n + 1
		}
		motivo := strings.TrimPrefix(strings.ReplaceAll(err.Error(), "\n", "; "), "yaml: ")
		motivo = linhaDoYAML.ReplaceAllString(motivo, "linha $1 do frontmatter")
		return nil, recusa(linha, "frontmatter inválido: %s", motivo)
	}
	var ps []Problema
	add := func(linha int, formato string, args ...any) {
		ps = append(ps, Problema{Linha: linha, Motivo: fmt.Sprintf(formato, args...)})
	}
	achar := func(prefixo string) int {
		for i := 1; i < fim; i++ {
			if strings.HasPrefix(strings.TrimSpace(linhas[i]), prefixo) {
				return i + 1
			}
		}
		return 1
	}
	if !contem(central.TiposPendencia, fm.Tipo) {
		add(achar("tipo:"), "tipo %q inválido: use duvida, autorizacao ou decisao", fm.Tipo)
	}
	if p := linhaUnica("titulo", fm.Titulo, 200); p != "" {
		add(achar("titulo:"), "%s", p)
	}
	if p := linhaUnica("autor", fm.Autor, 100); p != "" {
		add(achar("autor:"), "%s", p)
	}
	opcoes := make([]central.Opcao, len(fm.Opcoes))
	for i, o := range fm.Opcoes {
		opcoes[i] = central.Opcao{ID: o.ID, Titulo: o.Titulo, Descricao: o.Descricao, Recomendada: o.Recomendada}
	}
	if contem(central.TiposPendencia, fm.Tipo) {
		if _, err := central.ValidarOpcoes(fm.Tipo, opcoes); err != nil {
			add(achar("opcoes:"), "%s", output.AsError(err).Cause)
		}
	}
	corpo := strings.Join(linhas[fim+1:], "\n")
	if len(corpo) > central.LimiteDetalhe {
		add(fim+2, "detalhe com %d bytes; o máximo é %d", len(corpo), central.LimiteDetalhe)
	}
	if strings.ContainsFunc(corpo, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) {
		add(fim+2, "o detalhe tem caractere de controle")
	}
	ps = append(ps, secoesFaltando(linhas, fim+1, fm.Tipo)...)
	if len(ps) > 0 {
		return nil, &ErroArquivo{Problemas: ps}
	}
	return &Arquivo{Tipo: fm.Tipo, Titulo: strings.TrimSpace(fm.Titulo), Autor: strings.TrimSpace(fm.Autor), Opcoes: opcoes, Detalhe: strings.TrimSpace(corpo) + "\n"}, nil
}

func contem(xs []string, x string) bool {
	for _, y := range xs {
		if x == y {
			return true
		}
	}
	return false
}

// linhaUnica confere um campo de uma linha; devolve o motivo ou "".
func linhaUnica(campo, v string, limite int) string {
	switch {
	case strings.TrimSpace(v) == "":
		return campo + " é obrigatório"
	case len([]rune(v)) > limite:
		return fmt.Sprintf("%s com mais de %d caracteres", campo, limite)
	case strings.ContainsFunc(v, output.UnsafeRune):
		return campo + " com caractere de controle"
	}
	return ""
}

var semAcento = strings.NewReplacer("á", "a", "à", "a", "â", "a", "ã", "a", "é", "e", "ê", "e", "í", "i", "ó", "o", "ô", "o", "õ", "o", "ú", "u", "ü", "u", "ç", "c")

// chaveSecao normaliza o título de uma seção: minúsculas, sem acento, sem o comentário entre
// parênteses do modelo ("Recomendação (obrigatória em decisão)").
func chaveSecao(t string) string {
	if i := strings.Index(t, "("); i >= 0 {
		t = t[:i]
	}
	return strings.Join(strings.Fields(semAcento.Replace(strings.ToLower(t))), " ")
}

// secoesFaltando devolve um problema por seção obrigatória ausente ou vazia. Títulos `## ` dentro
// de um bloco de código não contam.
func secoesFaltando(linhas []string, inicio int, tipo string) []Problema {
	type secao struct {
		linha int
		corpo []string
	}
	achadas := map[string]*secao{}
	var atual *secao
	cerca := ""
	for i := inicio; i < len(linhas); i++ {
		l := linhas[i]
		t := strings.TrimSpace(l)
		if c := marcaDeCerca(t); c != "" {
			switch {
			case cerca == "":
				cerca = c
			case strings.HasPrefix(t, cerca):
				cerca = ""
			}
		}
		if cerca == "" && strings.HasPrefix(l, "## ") {
			k := chaveSecao(l[3:])
			if _, dup := achadas[k]; !dup {
				atual = &secao{linha: i + 1}
				achadas[k] = atual
			} else {
				atual = nil
			}
			continue
		}
		if atual != nil {
			atual.corpo = append(atual.corpo, l)
		}
	}
	exigidas := secoes
	if tipo == "decisao" {
		exigidas = append(append([]struct{ chave, titulo string }{}, secoes[:2]...), append([]struct{ chave, titulo string }{{"recomendacao", "Recomendação"}}, secoes[2:]...)...)
	}
	var ps []Problema
	for _, e := range exigidas {
		s, ok := achadas[e.chave]
		switch {
		case !ok:
			ps = append(ps, Problema{Linha: len(linhas), Motivo: fmt.Sprintf("seção `## %s` ausente", e.titulo)})
		case strings.TrimSpace(strings.Join(s.corpo, "\n")) == "":
			ps = append(ps, Problema{Linha: s.linha, Motivo: fmt.Sprintf("seção `## %s` vazia", e.titulo)})
		}
	}
	return ps
}

func marcaDeCerca(t string) string {
	for _, c := range []string{"```", "~~~"} {
		if strings.HasPrefix(t, c) {
			return c
		}
	}
	return ""
}
