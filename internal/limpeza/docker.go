package limpeza

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/BasisTI/orq/internal/enriquecer"
)

// Rótulos Docker que os workers aplicam ao que criam.
const (
	RotuloChaveUS   = "orq.us"
	RotuloChaveOnda = "orq.onda"
)

var (
	idDockerRe   = regexp.MustCompile(`^(sha256:)?[0-9a-f]{12,64}$`)
	nomeDockerRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	valorRotulo  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./-]*$`)
)

// recursoDocker descreve como listar, conferir e remover cada tipo de recurso Docker.
type recursoDocker struct {
	tipo    string
	listar  []string // argumentos até o filtro; o filtro entra no fim
	inspect string   // subcomando de inspect
	rotulo  string   // template do rótulo no inspect
	remover []string
	valido  *regexp.Regexp
}

// Ordem de remoção: contêineres, redes, volumes e imagens (as imagens só soltam depois dos
// contêineres que as usam).
var recursosDocker = []recursoDocker{
	{"docker_container", []string{"ps", "-a", "--no-trunc", "--format", "{{.ID}} {{.Names}}"}, "container", `{{index .Config.Labels "%s"}}`, []string{"rm", "-f", "-v"}, idDockerRe},
	{"docker_rede", []string{"network", "ls", "--no-trunc", "--format", "{{.ID}} {{.Name}}"}, "network", `{{index .Labels "%s"}}`, []string{"network", "rm"}, idDockerRe},
	{"docker_volume", []string{"volume", "ls", "--format", "{{.Name}}"}, "volume", `{{index .Labels "%s"}}`, []string{"volume", "rm"}, nomeDockerRe},
	{"docker_imagem", []string{"image", "ls", "--no-trunc", "--format", "{{.ID}}"}, "image", `{{index .Config.Labels "%s"}}`, []string{"rmi"}, idDockerRe},
}

// planejarDocker lista, por rótulo exato chave=valor, os contêineres, redes, volumes e imagens a
// remover. Nunca usa prune: o que sai daqui são ids listados e conferidos um a um.
func (l *Limpador) planejarDocker(ctx context.Context, chave, valor string) []*Item {
	if !valorRotulo.MatchString(valor) {
		return []*Item{{Tipo: "docker", Alvo: chave + "=" + valor, Estado: Preservado, Motivo: "valor de rótulo inválido"}}
	}
	filtro := "label=" + chave + "=" + valor
	var itens []*Item
	for _, rd := range recursosDocker {
		args := append(append([]string(nil), rd.listar...), "--filter", filtro)
		out, err := l.F.Rodar(ctx, "docker", args...)
		if err != nil {
			f := enriquecer.ComoFalha(err)
			if f.Codigo == enriquecer.ErrBinarioAusente {
				return []*Item{{Tipo: "docker", Alvo: chave + "=" + valor, Estado: Ausente, Motivo: "docker não encontrado"}}
			}
			return append(itens, &Item{Tipo: "docker", Alvo: chave + "=" + valor, Estado: Preservado, Motivo: "Docker indisponível: " + f.Mensagem})
		}
		vistos := map[string]bool{}
		for _, linha := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			campos := strings.Fields(linha)
			if len(campos) == 0 {
				continue
			}
			id := campos[0]
			nome := ""
			if len(campos) > 1 {
				nome = campos[1]
			}
			if vistos[id] {
				continue
			}
			vistos[id] = true
			alvo := id
			if nome != "" {
				alvo = fmt.Sprintf("%s (%s)", id, nome)
			}
			it := &Item{Tipo: rd.tipo, Alvo: alvo}
			if !rd.valido.MatchString(id) {
				it.Estado, it.Motivo = Preservado, fmt.Sprintf("id %q fora do formato esperado", id)
			} else {
				it.Estado = Remover
				rd, id := rd, id
				it.exec = func(ctx context.Context) error { return l.removerDocker(ctx, rd, id, chave, valor) }
			}
			itens = append(itens, it)
		}
	}
	return itens
}

// removerDocker confere o rótulo do recurso logo antes de remover: o id vem de uma listagem
// que pode ter ficado velha, e o filtro do Docker é a única proteção contra o recurso de outro.
func (l *Limpador) removerDocker(ctx context.Context, rd recursoDocker, id, chave, valor string) error {
	out, err := l.F.Rodar(ctx, "docker", rd.inspect, "inspect", "--format", fmt.Sprintf(rd.rotulo, chave), id)
	if err != nil {
		f := enriquecer.ComoFalha(err)
		if strings.Contains(strings.ToLower(f.Mensagem), "no such") {
			return nil // já se foi
		}
		return preservar("não consegui conferir o rótulo: " + f.Mensagem)
	}
	if atual := strings.TrimSpace(string(out)); atual != valor {
		return preservar(fmt.Sprintf("o rótulo %s é %q, não %q", chave, atual, valor))
	}
	_, err = l.F.Rodar(ctx, "docker", append(append([]string(nil), rd.remover...), id)...)
	return err
}

// LimparOnda remove os recursos Docker com o rótulo orq.onda=<id>. Só o Docker: panes, git e
// pastas são das US.
func (l *Limpador) LimparOnda(ctx context.Context, ondaID int64, dry bool) (Resultado, error) {
	valor := fmt.Sprint(ondaID)
	res := Resultado{US: "onda " + valor, DryRun: dry, Itens: l.planejarDocker(ctx, RotuloChaveOnda, valor)}
	if !dry {
		l.executar(ctx, res.Itens)
	}
	res.Agiu, res.Completa = resumo(res.Itens)
	if !dry {
		if err := l.St.RegistrarExternosOnda(ctx, ondaID, eventos(res)); err != nil {
			return res, err
		}
	}
	return res, nil
}
