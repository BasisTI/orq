package herdrcfg

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Editor transforma o texto do config; mudancas vazio quer dizer que não há o que gravar.
type Editor func(texto []byte) (novo []byte, mudancas []string, err error)

// Checador roda `herdr config check` sobre o arquivo dado e devolve a saída; erro quando o
// check reprova (ou não roda).
type Checador func(ctx context.Context, caminho string) (string, error)

// Resultado de Aplicar.
type Resultado struct {
	Config   string   `json:"config"`
	Mudancas []string `json:"mudancas"`
	Backup   *string  `json:"backup"` // cópia do arquivo antes da escrita; nulo sem escrita ou sem arquivo anterior
	Check    *string  `json:"check"`  // saída do herdr config check depois da escrita
}

// CaminhoPadrao é o config.toml que o Herdr lê: $HERDR_CONFIG_PATH, depois
// $XDG_CONFIG_HOME/herdr/config.toml e, por fim, ~/.config/herdr/config.toml.
func CaminhoPadrao(env func(string) string) (string, error) {
	if p := env("HERDR_CONFIG_PATH"); p != "" {
		return p, nil
	}
	if x := env("XDG_CONFIG_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "herdr", "config.toml"), nil
	}
	home := env("HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", &Erro{Codigo: "config_indisponivel", Causa: "sem $HOME para achar o config.toml do Herdr", Recuperacao: "passe --config"}
		}
		home = h
	}
	return filepath.Join(home, ".config", "herdr", "config.toml"), nil
}

// Aplicar lê o arquivo, roda o editor e, havendo mudança: confere o arquivo atual com o check
// (um config que já reprova não é tocado), copia o atual para <arquivo>.orq-AAAAMMDD-HHMMSS.bak,
// grava o novo de forma atômica e roda o check. Check reprovado restaura o backup (ou apaga o
// arquivo, se ele não existia) e devolve config_check_falhou.
func Aplicar(ctx context.Context, caminho string, editar Editor, check Checador, agora time.Time) (Resultado, error) {
	r := Resultado{Config: caminho, Mudancas: []string{}}
	antes, err := os.ReadFile(caminho)
	existia := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return r, err
	}
	novo, mud, err := editar(antes)
	if err != nil {
		return r, err
	}
	if len(mud) == 0 {
		return r, nil
	}
	r.Mudancas = mud
	modo := fs.FileMode(0o644)
	if existia {
		if saida, err := check(ctx, caminho); err != nil {
			return r, &Erro{Codigo: "config_invalida", Causa: "o herdr config check já reprova o arquivo atual, antes de qualquer mudança: " + resumo(saida, err),
				Recuperacao: "corrija o config.toml até o herdr config check passar e rode de novo; o orq não mexe num arquivo que já reprova"}
		}
		if st, err := os.Stat(caminho); err == nil {
			modo = st.Mode().Perm()
		}
		b := backupLivre(caminho, agora)
		if err := os.WriteFile(b, antes, modo); err != nil {
			return r, fmt.Errorf("backup %s: %w", b, err)
		}
		r.Backup = &b
	} else if err := os.MkdirAll(filepath.Dir(caminho), 0o755); err != nil {
		return r, err
	}
	if err := gravarAtomico(caminho, novo, modo); err != nil {
		return r, err
	}
	saida, err := check(ctx, caminho)
	if err != nil {
		var rerr error
		if existia {
			rerr = gravarAtomico(caminho, antes, modo)
		} else {
			rerr = os.Remove(caminho)
		}
		causa := "o herdr config check reprovou o arquivo editado: " + resumo(saida, err)
		rec := "o arquivo anterior foi restaurado"
		if rerr != nil {
			rec = fmt.Sprintf("a restauração falhou (%v): copie o backup de volta à mão", rerr)
		}
		if r.Backup != nil {
			rec += "; backup em " + *r.Backup
		}
		return r, &Erro{Codigo: "config_check_falhou", Causa: causa, Recuperacao: rec + ". Reporte o caso: o orq não devia gerar config inválido"}
	}
	r.Check = &saida
	return r, nil
}

func resumo(saida string, err error) string {
	if saida != "" {
		return saida
	}
	return err.Error()
}

// backupLivre escolhe <arquivo>.orq-AAAAMMDD-HHMMSS.bak, com -2, -3… se já existir.
func backupLivre(caminho string, agora time.Time) string {
	base := caminho + ".orq-" + agora.Format("20060102-150405")
	b := base + ".bak"
	for n := 2; ; n++ {
		if _, err := os.Lstat(b); errors.Is(err, fs.ErrNotExist) {
			return b
		}
		b = fmt.Sprintf("%s-%d.bak", base, n)
	}
}

func gravarAtomico(caminho string, dados []byte, modo fs.FileMode) error {
	// Um config.toml que é link simbólico (dotfiles) continua link: grava-se no alvo.
	if alvo, err := filepath.EvalSymlinks(caminho); err == nil {
		caminho = alvo
	}
	tmp, err := os.CreateTemp(filepath.Dir(caminho), ".config.toml.orq-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(dados); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(modo); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), caminho)
}
