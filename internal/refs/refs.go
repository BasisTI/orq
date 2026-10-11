// Package refs interpreta as referências que a CLI recebe: US (`projeto/312`, `TG-312`),
// pendência (`TG-312#P2`) e URL de merge request (GitLab ou GitHub).
package refs

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// US é a referência de uma User Story. Projeto vazio quer dizer que ele ainda precisa ser
// resolvido pela onda ou pelo projeto padrão do coordenador.
type US struct {
	Projeto string
	Ref     int
}

// Chave é a forma canônica `projeto/ref`; sem projeto, cai para `TG-ref`.
func (u US) Chave() string {
	if u.Projeto == "" {
		return Exibir(u.Ref)
	}
	return u.Projeto + "/" + strconv.Itoa(u.Ref)
}

// Exibir é a forma curta usada nas mensagens e nos ids de pendência: `TG-312`.
func Exibir(ref int) string { return "TG-" + strconv.Itoa(ref) }

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// ValidarProjeto confere o slug de um projeto do Taiga e o devolve em minúsculas.
func ValidarProjeto(s string) (string, error) {
	p := strings.ToLower(strings.TrimSpace(s))
	if !slugRe.MatchString(p) {
		return "", fmt.Errorf("projeto %q inválido: use o slug do Taiga, ex. infra-2025", s)
	}
	return p, nil
}

// ParseUS aceita `projeto/312`, `TG-312`, `#312` e `312`.
func ParseUS(s string) (US, error) {
	raw := strings.TrimSpace(s)
	projeto, num := "", raw
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		p, err := ValidarProjeto(raw[:i])
		if err != nil {
			return US{}, err
		}
		projeto, num = p, raw[i+1:]
	}
	switch {
	case len(num) > 3 && strings.EqualFold(num[:3], "TG-"):
		num = num[3:]
	case strings.HasPrefix(num, "#"):
		num = num[1:]
	}
	n, err := strconv.Atoi(num)
	if err != nil || n <= 0 || strings.HasPrefix(num, "+") {
		return US{}, fmt.Errorf("referência de US %q inválida: use projeto/312, TG-312 ou 312", s)
	}
	return US{Projeto: projeto, Ref: n}, nil
}

// Pend é a referência de uma pendência: a US e o número sequencial dela.
type Pend struct {
	US     US
	Numero int
}

// ParsePend aceita `TG-312#P2`, `projeto/312#P2` e `312#2`.
func ParsePend(s string) (Pend, error) {
	i := strings.LastIndex(s, "#")
	if i <= 0 {
		return Pend{}, fmt.Errorf("pendência %q inválida: use TG-312#P2", s)
	}
	u, err := ParseUS(s[:i])
	if err != nil {
		return Pend{}, err
	}
	num := s[i+1:]
	if len(num) > 1 && (num[0] == 'P' || num[0] == 'p') {
		num = num[1:]
	}
	n, err := strconv.Atoi(num)
	if err != nil || n <= 0 || strings.HasPrefix(num, "+") {
		return Pend{}, fmt.Errorf("pendência %q inválida: use TG-312#P2", s)
	}
	return Pend{US: u, Numero: n}, nil
}

// PendID é o id exibido de uma pendência: `TG-312#P2`.
func PendID(ref, numero int) string { return Exibir(ref) + "#P" + strconv.Itoa(numero) }

// MR é uma merge request (GitLab) ou pull request (GitHub) identificada pela URL.
type MR struct {
	URL         string // forma normalizada, sem query, fragmento ou subpágina
	ProjetoPath string // grupo/subgrupo/projeto ou dono/repositório
	IID         int
	IaC         bool // projeto sob basis/iac/
}

// ParseMR deriva projeto e número de `…/<path>/-/merge_requests/N` ou `…/<dono>/<repo>/pull/N`.
func ParseMR(raw string) (MR, error) {
	invalida := fmt.Errorf("URL de MR %q inválida: use https://<gitlab>/<grupo>/<projeto>/-/merge_requests/N ou https://github.com/<dono>/<repo>/pull/N", raw)
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return MR{}, invalida
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	var path, tail []string
	github := false
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == "-" && segs[i+1] == "merge_requests" {
			path, tail = segs[:i], segs[i+2:]
			break
		}
	}
	if path == nil && len(segs) >= 4 && segs[2] == "pull" {
		path, tail, github = segs[:2], segs[3:], true
	}
	if len(path) == 0 || len(tail) == 0 {
		return MR{}, invalida
	}
	for _, p := range path {
		if p == "" || p == "." || p == ".." || p == "-" {
			return MR{}, invalida
		}
	}
	n, err := strconv.Atoi(tail[0])
	if err != nil || n <= 0 || strings.HasPrefix(tail[0], "+") {
		return MR{}, invalida
	}
	pp := strings.Join(path, "/")
	norm := fmt.Sprintf("%s://%s/%s/-/merge_requests/%d", u.Scheme, strings.ToLower(u.Host), pp, n)
	if github {
		norm = fmt.Sprintf("%s://%s/%s/pull/%d", u.Scheme, strings.ToLower(u.Host), pp, n)
	}
	return MR{
		URL:         norm,
		ProjetoPath: pp,
		IID:         n,
		IaC:         strings.HasPrefix(strings.ToLower(pp), "basis/iac/"),
	}, nil
}
