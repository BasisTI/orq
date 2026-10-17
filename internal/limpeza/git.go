package limpeza

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BasisTI/orq/internal/enriquecer"
)

// branches que a limpeza nunca apaga, qualquer que seja a MR.
var branchesProtegidas = map[string]bool{"main": true, "master": true, "develop": true, "trunk": true, "HEAD": true}

// git roda o git em dir. O caminho do binário vem de Ferramentas (ORQ_GIT_BIN).
func (l *Limpador) git(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := l.F.RodarComEnv(ctx, []string{"GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}, "git", append([]string{"-C", dir}, args...)...)
	return strings.TrimSpace(string(out)), err
}

func mensagem(err error) string { return enriquecer.ComoFalha(err).Mensagem }

// planejarRepo monta os três itens de um repositório: worktree, branch local e branch remota.
// Cada um só sai Remover se o conteúdo da branch já está na branch de destino (ou é o head
// mesclado da MR); senão fica Preservado, com o motivo.
func (l *Limpador) planejarRepo(ctx context.Context, r Repo, mrs map[string]enriquecer.DetalheMR) []*Item {
	wt := &Item{Tipo: "worktree", Alvo: r.Worktree}
	bl := &Item{Tipo: "branch_local", Alvo: r.Repo + " " + r.Branch}
	br := &Item{Tipo: "branch_remota", Alvo: remotoDe(r) + "/" + r.Branch + " em " + r.Repo}
	todos := []*Item{wt, bl, br}
	preservarTodos := func(motivo string) []*Item {
		for _, i := range todos {
			i.Estado, i.Motivo = Preservado, motivo
		}
		return todos
	}
	mr, ok := mrs[r.Branch]
	if !ok {
		return preservarTodos(fmt.Sprintf("nenhuma MR mesclada da US tem a branch %s como origem", r.Branch))
	}
	if branchesProtegidas[r.Branch] || r.Branch == mr.Alvo {
		return preservarTodos("branch protegida: " + r.Branch)
	}
	remoto := remotoDe(r)
	if _, err := l.git(ctx, r.Repo, "rev-parse", "--git-dir"); err != nil {
		return preservarTodos("repositório " + r.Repo + " inacessível: " + mensagem(err))
	}
	if _, err := l.git(ctx, r.Repo, "fetch", "--prune", remoto); err != nil {
		return preservarTodos("não consegui atualizar o remoto " + remoto + ": " + mensagem(err))
	}
	base := remoto + "/" + mr.Alvo
	if _, err := l.git(ctx, r.Repo, "rev-parse", "--verify", "-q", "refs/remotes/"+base+"^{commit}"); err != nil {
		return preservarTodos(base + " não existe no repositório")
	}

	// Worktree.
	l.planejarWorktree(ctx, wt, r, mr, base)
	// Branch local.
	bl.deps = []*Item{wt}
	switch tip, err := l.git(ctx, r.Repo, "rev-parse", "--verify", "-q", "refs/heads/"+r.Branch+"^{commit}"); {
	case err != nil:
		bl.Estado = Ausente
	default:
		if ok, motivo := l.integrado(ctx, r.Repo, tip, base, mr.Head); !ok {
			bl.Estado, bl.Motivo = Preservado, motivo
		} else {
			bl.Estado = Remover
			bl.exec = func(ctx context.Context) error {
				if atual, err := l.git(ctx, r.Repo, "rev-parse", "--verify", "-q", "refs/heads/"+r.Branch+"^{commit}"); err != nil || atual != tip {
					return preservar("a branch local mudou depois do plano")
				}
				_, err := l.git(ctx, r.Repo, "branch", "-D", r.Branch)
				return err
			}
		}
	}
	// Branch remota.
	switch tip, err := l.git(ctx, r.Repo, "rev-parse", "--verify", "-q", "refs/remotes/"+remoto+"/"+r.Branch+"^{commit}"); {
	case err != nil:
		br.Estado = Ausente
	default:
		if ok, motivo := l.integrado(ctx, r.Repo, tip, base, mr.Head); !ok {
			br.Estado, br.Motivo = Preservado, "remota: "+motivo
		} else {
			br.Estado = Remover
			br.exec = func(ctx context.Context) error {
				// A trava de escrita do push só apaga se o remoto ainda está no commit conferido.
				_, err := l.git(ctx, r.Repo, "push", "--force-with-lease=refs/heads/"+r.Branch+":"+tip, remoto, "--delete", r.Branch)
				return err
			}
		}
	}
	return todos
}

func remotoDe(r Repo) string {
	if r.Remoto != "" {
		return r.Remoto
	}
	return "origin"
}

func (l *Limpador) planejarWorktree(ctx context.Context, it *Item, r Repo, mr enriquecer.DetalheMR, base string) {
	lista, err := l.git(ctx, r.Repo, "worktree", "list", "--porcelain")
	if err != nil {
		it.Estado, it.Motivo = Preservado, "não consegui listar os worktrees: "+mensagem(err)
		return
	}
	var registrados []string
	for _, linha := range strings.Split(lista, "\n") {
		if p, ok := strings.CutPrefix(linha, "worktree "); ok {
			if rp, err := resolver(p); err == nil {
				registrados = append(registrados, rp)
			}
		}
	}
	alvo, err := resolver(r.Worktree)
	if err != nil {
		it.Estado, it.Motivo = Preservado, "não consegui resolver o caminho: "+err.Error()
		return
	}
	registrado := -1
	for i, p := range registrados {
		if p == alvo {
			registrado = i
		}
	}
	_, errStat := os.Lstat(r.Worktree)
	switch {
	case registrado == 0:
		it.Estado, it.Motivo = Preservado, "é o checkout principal do repositório"
		return
	case errors.Is(errStat, os.ErrNotExist) && registrado < 0:
		it.Estado = Ausente
		return
	case registrado < 0:
		it.Estado, it.Motivo = Preservado, "não é um worktree registrado de "+r.Repo
		return
	case errStat != nil:
		it.Estado, it.Motivo = Preservado, "a pasta sumiu mas o worktree segue registrado (git worktree prune)"
		return
	}
	cwd := l.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if rc, err := filepath.EvalSymlinks(cwd); err == nil && (rc == alvo || dentro(alvo, rc)) {
		it.Estado, it.Motivo = Preservado, "o processo roda dentro do worktree"
		return
	}
	if atual, err := l.git(ctx, alvo, "symbolic-ref", "--short", "-q", "HEAD"); err != nil || atual != r.Branch {
		it.Estado, it.Motivo = Preservado, fmt.Sprintf("o worktree está em %q, não em %s", atual, r.Branch)
		return
	}
	st, err := l.git(ctx, alvo, "status", "--porcelain")
	if err != nil {
		it.Estado, it.Motivo = Preservado, "não consegui ler o status: "+mensagem(err)
		return
	}
	if st != "" {
		it.Estado, it.Motivo = Preservado, fmt.Sprintf("árvore suja (%d entrada(s) por commitar)", len(strings.Split(st, "\n")))
		return
	}
	tip, err := l.git(ctx, alvo, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		it.Estado, it.Motivo = Preservado, "não consegui ler o HEAD: "+mensagem(err)
		return
	}
	if ok, motivo := l.integrado(ctx, r.Repo, tip, base, mr.Head); !ok {
		it.Estado, it.Motivo = Preservado, motivo
		return
	}
	it.Estado = Remover
	it.exec = func(ctx context.Context) error {
		// Reconfere a identidade do worktree na hora: o plano pode ter ficado velho (o worktree
		// pode estar numa branch nova, com a US reaberta).
		if b, err := l.git(ctx, alvo, "symbolic-ref", "--short", "-q", "HEAD"); err != nil || b != r.Branch {
			return preservar(fmt.Sprintf("o worktree agora está em %q, não em %s", b, r.Branch))
		}
		if h, err := l.git(ctx, alvo, "rev-parse", "--verify", "HEAD^{commit}"); err != nil || h != tip {
			return preservar("o HEAD do worktree mudou depois do plano")
		}
		if st, err := l.git(ctx, alvo, "status", "--porcelain"); err != nil || st != "" {
			return preservar("o worktree não está mais limpo")
		}
		// Sem --force: o git recusa se a árvore sujou desde a conferência.
		_, err := l.git(ctx, r.Repo, "worktree", "remove", alvo)
		if err != nil {
			return preservar("git worktree remove recusou: " + mensagem(err))
		}
		return nil
	}
}

// integrado diz se o que a branch tem em sha já está na branch de destino: sha é o head
// mesclado da MR, ou mesclar sha em base não muda a árvore de base (cobre merge, fast-forward e
// squash, em que a conferência é por conteúdo). Senão devolve o motivo, que separa o commit que
// nunca saiu da máquina do conteúdo que só falta mesclar.
func (l *Limpador) integrado(ctx context.Context, repo, sha, base, headMesclado string) (bool, string) {
	if headMesclado != "" && sha == headMesclado {
		return true, ""
	}
	arvoreBase, err := l.git(ctx, repo, "rev-parse", "--verify", base+"^{tree}")
	if err != nil {
		return false, "não consegui ler " + base + ": " + mensagem(err)
	}
	out, err := l.git(ctx, repo, "merge-tree", "--write-tree", "--no-messages", base, sha)
	if err == nil {
		if linha := strings.SplitN(out, "\n", 2)[0]; linha == arvoreBase {
			return true, ""
		}
	} else if enriquecer.ComoFalha(err).Status != 1 {
		return false, "não consegui comparar com " + base + ": " + mensagem(err)
	}
	n, err := l.git(ctx, repo, "rev-list", "--count", sha, "--not", "--remotes")
	if err == nil && n != "0" {
		return false, fmt.Sprintf("%s commit(s) não enviado(s) ao remoto e com conteúdo que %s não tem (%s)", n, base, abreviar(sha))
	}
	return false, fmt.Sprintf("o conteúdo de %s não está em %s", abreviar(sha), base)
}

func abreviar(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}
