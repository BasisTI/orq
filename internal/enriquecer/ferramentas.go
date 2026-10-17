// Package enriquecer consulta o Herdr, as forjas (GitLab e GitHub) e o Taiga, guarda o que
// leu no cache da Central e publica os tokens de sidebar do orq no Herdr. O daemon roda os
// enriquecedores em laço; orq atualizar roda uma vez.
//
// Toda ferramenta é chamada por caminho absoluto: o daemon nasce de um plugin do Herdr com PATH
// mínimo, sem ~/.local/bin. Falha de subprocesso vira um central.Erro no cache, nunca derruba
// o processo.
package enriquecer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BasisTI/orq/internal/central"
)

// Códigos de erro gravados no cache.
const (
	ErrBinarioAusente   = "binario_ausente"
	ErrAutenticacao     = "autenticacao"
	ErrNaoEncontrado    = "nao_encontrado"
	ErrTempoEsgotado    = "tempo_esgotado"
	ErrFalhaComando     = "falha_comando"
	ErrRespostaInvalida = "resposta_invalida"
)

// VariavelBin é a variável que aponta o caminho absoluto de cada ferramenta.
var VariavelBin = map[string]string{
	"herdr":  "ORQ_HERDR_BIN",
	"glab":   "ORQ_GLAB_BIN",
	"gh":     "ORQ_GH_BIN",
	"taiga":  "ORQ_TAIGA_BIN",
	"git":    "ORQ_GIT_BIN",
	"docker": "ORQ_DOCKER_BIN",
}

// Ferramentas resolve e roda os binários externos.
type Ferramentas struct {
	// Env é o ambiente usado para resolver os caminhos (os testes trocam).
	Env func(string) string
	// Timeout de cada chamada; zero usa 30 s.
	Timeout time.Duration
}

// Falha é o erro de uma chamada, já com o código do cache.
type Falha struct {
	Codigo   string
	Mensagem string
	Saida    []byte // stdout
	Erro     []byte // stderr
	Status   int    // código de saída do processo; 0 quando ele nem rodou ou foi morto
}

func (f *Falha) Error() string { return f.Codigo + ": " + f.Mensagem }

func (f *Falha) cache() *central.Erro { return &central.Erro{Codigo: f.Codigo, Mensagem: f.Mensagem} }

// Caminho resolve o binário: a variável ORQ_<NOME>_BIN, depois $HERDR_BIN_PATH (só o herdr) e,
// por fim, os diretórios de instalação conhecidos. O PATH do processo não entra: num pane ou
// numa ação de plugin ele não tem ~/.local/bin.
func (f Ferramentas) Caminho(nome string) (string, error) {
	env := f.env()
	variavel := VariavelBin[nome]
	if v := env(variavel); v != "" {
		if !filepath.IsAbs(v) {
			return "", &Falha{Codigo: ErrBinarioAusente, Mensagem: fmt.Sprintf("%s=%q não é caminho absoluto", variavel, v)}
		}
		if !executavel(v) {
			return "", &Falha{Codigo: ErrBinarioAusente, Mensagem: fmt.Sprintf("%s aponta %s, que não existe ou não é executável", variavel, v)}
		}
		return v, nil
	}
	if nome == "herdr" {
		if v := env("HERDR_BIN_PATH"); v != "" && filepath.IsAbs(v) && executavel(v) {
			return v, nil
		}
	}
	var dirs []string
	if home := env("HOME"); home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"), filepath.Join(home, "go", "bin"), filepath.Join(home, "bin"))
	}
	dirs = append(dirs, "/home/linuxbrew/.linuxbrew/bin", "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin")
	for _, d := range dirs {
		if p := filepath.Join(d, nome); executavel(p) {
			return p, nil
		}
	}
	return "", &Falha{Codigo: ErrBinarioAusente, Mensagem: fmt.Sprintf("%s não encontrado em %s; defina %s com o caminho absoluto", nome, strings.Join(dirs, ", "), variavel)}
}

// Rodar chama a ferramenta e devolve o stdout. Saída diferente de zero, binário ausente e tempo
// esgotado viram *Falha; quem chama refina o código (autenticação, não encontrado).
func (f Ferramentas) Rodar(ctx context.Context, nome string, args ...string) ([]byte, error) {
	return f.RodarComEnv(ctx, nil, nome, args...)
}

// RodarComEnv é Rodar com variáveis a mais (CHAVE=valor) sobre o ambiente do processo.
func (f Ferramentas) RodarComEnv(ctx context.Context, env []string, nome string, args ...string) ([]byte, error) {
	bin, err := f.Caminho(nome)
	if err != nil {
		return nil, err
	}
	timeout := f.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.WaitDelay = 2 * time.Second
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if err == nil {
		return stdout.Bytes(), nil
	}
	falha := &Falha{Codigo: ErrFalhaComando, Saida: stdout.Bytes(), Erro: stderr.Bytes()}
	var ee *exec.ExitError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		falha.Codigo, falha.Mensagem = ErrTempoEsgotado, fmt.Sprintf("%s não respondeu em %s", nome, timeout)
	case errors.As(err, &ee):
		falha.Status = ee.ExitCode()
		falha.Mensagem = fmt.Sprintf("%s saiu com %d: %s", nome, ee.ExitCode(), resumir(stderr.Bytes(), stdout.Bytes()))
	default:
		falha.Mensagem = fmt.Sprintf("%s: %v", nome, err)
	}
	return nil, falha
}

func (f Ferramentas) env() func(string) string {
	if f.Env != nil {
		return f.Env
	}
	return os.Getenv
}

func executavel(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0
}

// resumir escolhe a primeira saída não vazia e a corta numa linha curta.
func resumir(saidas ...[]byte) string {
	for _, s := range saidas {
		t := strings.Join(strings.Fields(string(s)), " ")
		if t == "" {
			continue
		}
		if r := []rune(t); len(r) > 300 {
			t = string(r[:300]) + "…"
		}
		return t
	}
	return "sem saída"
}

// comoFalha devolve err como *Falha.
func comoFalha(err error) *Falha {
	var f *Falha
	if errors.As(err, &f) {
		return f
	}
	return &Falha{Codigo: ErrFalhaComando, Mensagem: err.Error()}
}

// ComoFalha devolve err como *Falha, para quem chama de fora do pacote.
func ComoFalha(err error) *Falha { return comoFalha(err) }
