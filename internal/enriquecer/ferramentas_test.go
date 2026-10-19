package enriquecer

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

// exe cria um executável vazio em dir/nome.
func exe(t *testing.T, dir, nome string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, nome)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// ferramentasFalsas devolve Ferramentas com HOME, USER e a raiz do Nix num diretório temporário.
func ferramentasFalsas(t *testing.T, extra map[string]string) (f Ferramentas, home, raiz string) {
	t.Helper()
	tmp := t.TempDir()
	home, raiz = filepath.Join(tmp, "home"), filepath.Join(tmp, "raiz")
	env := map[string]string{"HOME": home, "USER": "ana"}
	for k, v := range extra {
		env[k] = v
	}
	return Ferramentas{Env: func(k string) string { return env[k] }, Raiz: raiz}, home, raiz
}

func TestCaminhoDiretoriosDoNix(t *testing.T) {
	for _, nome := range []string{"herdr", "glab", "gh", "taiga", "git", "docker", "go"} {
		cases := []struct {
			nome string
			dir  func(home, raiz string) string
		}{
			{"nix-profile", func(h, _ string) string { return filepath.Join(h, ".nix-profile", "bin") }},
			{"per-user", func(_, r string) string { return filepath.Join(r, "etc", "profiles", "per-user", "ana", "bin") }},
			{"perfil padrão", func(_, r string) string { return filepath.Join(r, "nix", "var", "nix", "profiles", "default", "bin") }},
			{"sistema", func(_, r string) string { return filepath.Join(r, "run", "current-system", "sw", "bin") }},
		}
		for _, c := range cases {
			t.Run(nome+"/"+c.nome, func(t *testing.T) {
				f, home, raiz := ferramentasFalsas(t, nil)
				quer := exe(t, c.dir(home, raiz), nome)
				r, err := f.Resolver(nome)
				if err != nil || r.Caminho != quer || r.Origem != OrigemDiretorio {
					t.Fatalf("Resolver(%s) = %+v, %v; quer %s", nome, r, err, quer)
				}
			})
		}
	}
}

// Sem USER, o diretório per-user vem do usuário do uid.
func TestCaminhoPerUserSemUSER(t *testing.T) {
	atual, err := user.Current()
	if err != nil {
		t.Skip("sem usuário do uid:", err)
	}
	f, _, raiz := ferramentasFalsas(t, nil)
	env := f.Env
	f.Env = func(k string) string {
		if k == "USER" {
			return ""
		}
		return env(k)
	}
	quer := exe(t, filepath.Join(raiz, "etc", "profiles", "per-user", atual.Username, "bin"), "glab")
	if got, err := f.Caminho("glab"); err != nil || got != quer {
		t.Fatalf("Caminho = %q, %v; quer %s", got, err, quer)
	}
}

// A ordem: ~/.local/bin, ~/go/bin, ~/bin, os do Nix na ordem da story e só então linuxbrew e
// /usr/local/bin. A mesma ferramenta nos quatro do Nix e em ~/bin: vence cada um por vez.
func TestCaminhoOrdemDeBusca(t *testing.T) {
	f, home, raiz := ferramentasFalsas(t, nil)
	ordem := []string{
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "go", "bin"),
		filepath.Join(home, "bin"),
		filepath.Join(home, ".nix-profile", "bin"),
		filepath.Join(raiz, "etc", "profiles", "per-user", "ana", "bin"),
		filepath.Join(raiz, "nix", "var", "nix", "profiles", "default", "bin"),
		filepath.Join(raiz, "run", "current-system", "sw", "bin"),
	}
	dirs := f.Diretorios("glab")
	if len(dirs) < len(ordem)+1 {
		t.Fatalf("dirs = %v", dirs)
	}
	for i, d := range ordem {
		if dirs[i] != d {
			t.Fatalf("dirs[%d] = %s; quer %s\n%v", i, dirs[i], d, dirs)
		}
	}
	if dirs[len(ordem)] != "/home/linuxbrew/.linuxbrew/bin" {
		t.Fatalf("depois do Nix vem %s, não o linuxbrew", dirs[len(ordem)])
	}
	// Do último ao primeiro: cada novo executável, mais cedo na ordem, passa a vencer.
	for i := len(ordem) - 1; i >= 0; i-- {
		quer := exe(t, ordem[i], "glab")
		if got, err := f.Caminho("glab"); err != nil || got != quer {
			t.Fatalf("com %s: Caminho = %q, %v", ordem[i], got, err)
		}
	}
}

func TestCaminhoVariavelVenceDiretorio(t *testing.T) {
	f, home, _ := ferramentasFalsas(t, nil)
	exe(t, filepath.Join(home, ".nix-profile", "bin"), "glab")
	var fora = exe(t, t.TempDir(), "glab")
	env := f.Env
	f.Env = func(k string) string {
		if k == "ORQ_GLAB_BIN" {
			return fora
		}
		return env(k)
	}
	f.OrigemVar = func(k string) string {
		if k == "ORQ_GLAB_BIN" {
			return "orq.env"
		}
		return ""
	}
	r, err := f.Resolver("glab")
	if err != nil || r.Caminho != fora || r.Origem != OrigemOrqEnv {
		t.Fatalf("Resolver = %+v, %v", r, err)
	}
	f.OrigemVar = nil
	if r, _ := f.Resolver("glab"); r.Origem != OrigemVariavel {
		t.Fatalf("origem = %q", r.Origem)
	}
}

// A mensagem de binario_ausente lista os diretórios do Nix tentados.
func TestBinarioAusenteListaDiretoriosDoNix(t *testing.T) {
	f, home, raiz := ferramentasFalsas(t, nil)
	_, err := f.Caminho("orq-ferramenta-inexistente")
	if err == nil {
		t.Fatal("achou uma ferramenta que não existe")
	}
	f2 := comoFalha(err)
	if f2.Codigo != ErrBinarioAusente {
		t.Fatalf("código = %s", f2.Codigo)
	}
	for _, d := range []string{
		filepath.Join(home, ".nix-profile", "bin"),
		filepath.Join(raiz, "etc", "profiles", "per-user", "ana", "bin"),
		filepath.Join(raiz, "nix", "var", "nix", "profiles", "default", "bin"),
		filepath.Join(raiz, "run", "current-system", "sw", "bin"),
	} {
		if !strings.Contains(f2.Mensagem, d) {
			t.Errorf("mensagem sem %s:\n%s", d, f2.Mensagem)
		}
	}
}
