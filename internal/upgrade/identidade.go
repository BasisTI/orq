// Package upgrade atualiza de uma vez os binários do orq (o do go install e o do plugin do
// Herdr), reinicia o daemon e confere que tudo ficou na mesma versão. Não abre o banco: tem de
// funcionar justamente quando o orq do PATH recusa o banco (versao_do_banco).
package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Info é o que `orq version --output json` diz de um binário.
type Info struct {
	Versao string `json:"versao"`
	Commit string `json:"commit"`
	// Schema é a última migration que o binário embute; zero nos binários anteriores a este
	// campo, que só abrem um banco da mesma versão que a deles.
	Schema int `json:"schema"`
}

// semCommit casa o fim de uma versão que carrega o commit: o pseudo-versão do go install
// (0.0.0-20261019120000-abcdef123456), o git describe (0.3.0-5-gabcdef1) e o hash sozinho.
var semCommit = regexp.MustCompile(`(?i)(?:^|-g|-)([0-9a-f]{7,40})(?:-dirty)?$`)

// CommitDe tira da versão o commit que ela carrega, ou "" quando não carrega (ex.: "dev").
func CommitDe(versao string) string {
	if m := semCommit.FindStringSubmatch(strings.TrimSpace(versao)); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// MesmoCommit compara dois commits, cada um completo ou abreviado (pelo menos 7 caracteres).
func MesmoCommit(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	n := min(len(a), len(b))
	return n >= 7 && a[:n] == b[:n]
}

// Consultar roda `<caminho> version --output json`. Os binários anteriores só trazem a versão;
// o commit sai dela.
func Consultar(ctx context.Context, caminho string) (Info, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, caminho, "version", "--output", "json")
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return Info{}, fmt.Errorf("%s version: %w", caminho, err)
	}
	var i Info
	if err := json.Unmarshal(out, &i); err != nil || i.Versao == "" {
		return Info{}, fmt.Errorf("%s version: resposta inesperada %q", caminho, strings.TrimSpace(string(out)))
	}
	if i.Commit == "" {
		i.Commit = CommitDe(i.Versao)
	}
	return i, nil
}
