// Package limpeza guarda os recursos que o coordenador criou para uma US (repositório,
// worktree, branch, panes, pastas) e remove tudo depois que as MRs são mescladas. É conservadora:
// o que não passa numa conferência fica preservado, com o motivo, e nunca se apaga caminho que
// não esteja dentro das raízes conhecidas.
package limpeza

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
)

// ArquivoRecursos fica na pasta de trabalho da US.
const ArquivoRecursos = "recursos.json"

// VersaoRecursos é a versão do formato de recursos.json.
const VersaoRecursos = 1

// Raizes são as duas árvores em que o orq apaga pastas: a de trabalho (pacotes, relatórios,
// recursos.json) e a dos temporários dos workers.
type Raizes struct {
	Trabalho string // ORQ_TRABALHO_DIR; padrão ~/tmp/orquestrador
	Tmp      string // ORQ_TMP_DIR; padrão /tmp/orq
}

// RaizesDe resolve as raízes pelo ambiente.
func RaizesDe(env func(string) string) (Raizes, error) {
	r := Raizes{Trabalho: env("ORQ_TRABALHO_DIR"), Tmp: env("ORQ_TMP_DIR")}
	if r.Trabalho == "" {
		home := env("HOME")
		if home == "" {
			h, err := os.UserHomeDir()
			if err != nil {
				return r, erroLimpeza("sem_home", "sem $HOME para achar ~/tmp/orquestrador", "defina ORQ_TRABALHO_DIR", output.ExitUnexpected)
			}
			home = h
		}
		r.Trabalho = filepath.Join(home, "tmp", "orquestrador")
	}
	if r.Tmp == "" {
		r.Tmp = "/tmp/orq"
	}
	for _, p := range []string{r.Trabalho, r.Tmp} {
		if !filepath.IsAbs(p) || filepath.Clean(p) == "/" {
			return r, erroLimpeza("raiz_invalida", fmt.Sprintf("raiz %q não é um caminho absoluto abaixo de /", p), "corrija ORQ_TRABALHO_DIR e ORQ_TMP_DIR", output.ExitUsage)
		}
	}
	return r, nil
}

// PastaTrabalho é ~/tmp/orquestrador/<projeto>/TG-<ref>.
func (r Raizes) PastaTrabalho(u refs.US) string {
	return filepath.Join(r.Trabalho, u.Projeto, refs.Exibir(u.Ref))
}

// PastaTmp é o TMPDIR do worker: /tmp/orq/<projeto>/TG-<ref>.
func (r Raizes) PastaTmp(u refs.US) string {
	return filepath.Join(r.Tmp, u.Projeto, refs.Exibir(u.Ref))
}

// Arquivo é o recursos.json da US.
func (r Raizes) Arquivo(u refs.US) string {
	return filepath.Join(r.PastaTrabalho(u), ArquivoRecursos)
}

// RotuloUS é o valor do rótulo Docker orq.us: <projeto>/<ref>.
func RotuloUS(u refs.US) string { return u.Projeto + "/" + strconv.Itoa(u.Ref) }

// Repo é um repositório em que a US tem worktree e branch.
type Repo struct {
	Repo     string `json:"repo"`     // checkout principal
	Worktree string `json:"worktree"` // worktree da US
	Branch   string `json:"branch"`
	Remoto   string `json:"remoto,omitempty"` // padrão origin
}

// Recursos é o que o coordenador criou para a US.
type Recursos struct {
	Versao  int      `json:"versao"`
	Projeto string   `json:"projeto"`
	Ref     int      `json:"ref"`
	Repos   []Repo   `json:"repos"`
	Panes   []string `json:"panes"` // nomes de agente
	// Trabalho e Tmp são informativos: o orq só apaga os caminhos que ele mesmo deriva das
	// raízes, e um valor diferente aqui vira "preservado".
	Trabalho string `json:"trabalho"`
	Tmp      string `json:"tmp"`
	// Aprovados guarda, por URL de MR, o head aprovado na revisão. Com ele, o head mesclado
	// diferente recusa a limpeza.
	Aprovados map[string]string `json:"aprovados,omitempty"`
}

var (
	branchRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	shaRe    = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
	nomeRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// Validar confere o conteúdo; ele vem de um arquivo que outro processo escreveu.
func (r *Recursos) Validar(u refs.US) error {
	if r.Versao != VersaoRecursos {
		return fmt.Errorf("versão %d de recursos.json não suportada (esperada %d)", r.Versao, VersaoRecursos)
	}
	if r.Projeto != u.Projeto || r.Ref != u.Ref {
		return fmt.Errorf("recursos.json é de %s/%d, não de %s", r.Projeto, r.Ref, u.Chave())
	}
	for i, p := range r.Repos {
		for nome, v := range map[string]string{"repo": p.Repo, "worktree": p.Worktree} {
			if !filepath.IsAbs(v) || strings.ContainsFunc(v, output.UnsafeRune) {
				return fmt.Errorf("repos[%d].%s %q não é um caminho absoluto", i, nome, v)
			}
		}
		if !branchRe.MatchString(p.Branch) || strings.Contains(p.Branch, "..") || strings.HasSuffix(p.Branch, ".lock") || strings.HasSuffix(p.Branch, "/") {
			return fmt.Errorf("repos[%d].branch %q inválida", i, p.Branch)
		}
		if p.Remoto != "" && !nomeRe.MatchString(p.Remoto) {
			return fmt.Errorf("repos[%d].remoto %q inválido", i, p.Remoto)
		}
	}
	for _, n := range r.Panes {
		if !nomeRe.MatchString(n) {
			return fmt.Errorf("pane %q inválido: use o nome do agente", n)
		}
	}
	for url, sha := range r.Aprovados {
		if _, err := refs.ParseMR(url); err != nil {
			return err
		}
		if !shaRe.MatchString(sha) {
			return fmt.Errorf("head aprovado %q de %s não é um sha", sha, url)
		}
	}
	return nil
}

// ErrSemRecursos é o recursos.json ausente.
var ErrSemRecursos = errors.New("sem recursos.json")

// LerRecursos lê e valida o arquivo da US.
func LerRecursos(arquivo string, u refs.US) (*Recursos, error) {
	b, err := os.ReadFile(arquivo)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrSemRecursos
	}
	if err != nil {
		return nil, err
	}
	var r Recursos
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", arquivo, err)
	}
	if err := r.Validar(u); err != nil {
		return nil, fmt.Errorf("%s: %w", arquivo, err)
	}
	return &r, nil
}

// Gravar escreve o arquivo (criando a pasta de trabalho) por rename, para o daemon nunca ler
// uma metade.
func (r *Recursos) Gravar(raizes Raizes, u refs.US) (string, error) {
	r.Versao, r.Projeto, r.Ref = VersaoRecursos, u.Projeto, u.Ref
	r.Trabalho, r.Tmp = raizes.PastaTrabalho(u), raizes.PastaTmp(u)
	if err := r.Validar(u); err != nil {
		return "", err
	}
	if r.Repos == nil {
		r.Repos = []Repo{}
	}
	if r.Panes == nil {
		r.Panes = []string{}
	}
	dir := raizes.PastaTrabalho(u)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".recursos-*.tmp")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, ArquivoRecursos)
	return dest, os.Rename(tmp.Name(), dest)
}

func erroLimpeza(code, cause, recovery string, exit int) error {
	return &output.Error{Code: code, Cause: cause, Recovery: recovery, Exit: exit}
}

// dentro diz se alvo está dentro de base (já resolvidos), sem ser a própria base.
func dentro(base, alvo string) bool {
	rel, err := filepath.Rel(base, alvo)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolver devolve o caminho com os symlinks resolvidos, mesmo quando o último trecho ainda não
// existe (resolve o pai).
func resolver(p string) (string, error) {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r, nil
	}
	pai, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return "", err
	}
	return filepath.Join(pai, filepath.Base(p)), nil
}
