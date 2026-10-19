package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/enriquecer"
	"github.com/BasisTI/orq/internal/herdrcfg"
	"github.com/BasisTI/orq/internal/output"
	"github.com/spf13/cobra"
)

// IDPlugin é o id do plugin no herdr-plugin.toml.
const IDPlugin = "orq"

func (a *App) pluginCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "plugin", Short: "Configuração do Herdr para o plugin orq: sidebar, teclas e diagnóstico"}
	var config string
	cmd.PersistentFlags().StringVar(&config, "config", "", "config.toml do Herdr (padrão: $HERDR_CONFIG_PATH, $XDG_CONFIG_HOME/herdr/config.toml ou ~/.config/herdr/config.toml)")
	caminho := func() (string, error) {
		if config != "" {
			return config, nil
		}
		return herdrcfg.CaminhoPadrao(a.Env)
	}
	var tecla, teclaCentral string
	configurar := &cobra.Command{
		Use:   "configurar [--tecla prefix+u] [--tecla-central prefix+shift+u] [--config <arquivo>]",
		Short: "Acrescenta ao config.toml do Herdr as linhas $orq/$orq_mrs da sidebar e as teclas do plugin",
		Long: `Acrescenta [{ token = "$orq" }] em [ui.sidebar.agents] rows, [{ token = "$orq_mrs" }] em
[ui.sidebar.spaces] rows e um [[keys.command]] para a ação popup-todos (--tecla) e outro para
abrir-central (--tecla-central); "" não cria a tecla. Os tokens mrs_prontas, story e fase, da
skill manual, não são tocados. Idempotente: o que já está lá não muda.

Antes de escrever, confere o arquivo atual com herdr config check (um config que já reprova
não é tocado) e copia o arquivo para <config>.orq-AAAAMMDD-HHMMSS.bak; depois de escrever,
roda o check de novo e, se ele reprovar, restaura o arquivo anterior.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := caminho()
			if err != nil {
				return erroCfg(err)
			}
			ed := func(b []byte) ([]byte, []string, error) {
				return herdrcfg.Configurar(b, herdrcfg.Opcoes{Tecla: tecla, TeclaCentral: teclaCentral})
			}
			return a.aplicarConfig(cmd.Context(), p, ed)
		},
	}
	configurar.Flags().StringVar(&tecla, "tecla", herdrcfg.TeclaPadrao, "tecla do popup com todos os coordenadores (ação popup-todos)")
	configurar.Flags().StringVar(&teclaCentral, "tecla-central", herdrcfg.TeclaCentralPadrao, "tecla que abre o workspace Central (ação abrir-central)")
	desconfigurar := &cobra.Command{
		Use:   "desconfigurar [--config <arquivo>]",
		Short: "Tira do config.toml do Herdr só o que orq plugin configurar acrescentou",
		Long: `Tira as linhas [{ token = "$orq" }] e [{ token = "$orq_mrs" }] das rows da sidebar e os
[[keys.command]] que têm o comentário "# orq:" logo acima, pelo texto. Token do orq numa linha
do usuário e tecla do usuário para uma ação orq.* (sem a marca) ficam, como o resto do arquivo.
Mesmo backup e mesmo check de configurar.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := caminho()
			if err != nil {
				return erroCfg(err)
			}
			return a.aplicarConfig(cmd.Context(), p, herdrcfg.Desconfigurar)
		},
	}
	doctor := &cobra.Command{
		Use:   "doctor [--config <arquivo>]",
		Short: "Diagnóstico: binários, daemon, plugin no Herdr, sidebar, teclas e banco",
		Long: `Confere, sem mudar nada: os binários que o orq chama (herdr, glab, gh, taiga), o daemon
(trava daemon.lock no diretório de estado), o plugin orq registrado e habilitado no Herdr, as
linhas da sidebar e as teclas no config.toml e o banco. "ok" é falso quando falta algo de que
a Central precisa; glab, gh e taiga ausentes são só avisos.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := caminho()
			if err != nil {
				return erroCfg(err)
			}
			d := a.doctor(cmd.Context(), p)
			return a.emit(d, func(w io.Writer) error { return textoDoctor(w, d) })
		},
	}
	cmd.AddCommand(configurar, desconfigurar, doctor)
	return cmd
}

func erroCfg(err error) error {
	var e *herdrcfg.Erro
	if errors.As(err, &e) {
		exit := output.ExitConflict
		if e.Codigo == "config_indisponivel" {
			exit = output.ExitUsage
		}
		return &output.Error{Code: e.Codigo, Cause: e.Causa, Recovery: e.Recuperacao, Exit: exit}
	}
	return err
}

// checarConfig roda `herdr config check` com HERDR_CONFIG_PATH apontando o arquivo.
func (a *App) checarConfig(ctx context.Context, caminho string) (string, error) {
	f := enriquecer.Ferramentas{Env: a.Env, Timeout: 20 * time.Second}
	out, err := f.RodarComEnv(ctx, []string{"HERDR_CONFIG_PATH=" + caminho}, "herdr", "config", "check")
	if err != nil {
		var fl *enriquecer.Falha
		if errors.As(err, &fl) {
			if s := strings.TrimSpace(string(fl.Saida) + "\n" + string(fl.Erro)); s != "" {
				return s, err
			}
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (a *App) aplicarConfig(ctx context.Context, caminho string, ed herdrcfg.Editor) error {
	r, err := herdrcfg.Aplicar(ctx, caminho, ed, a.checarConfig, time.Now())
	if err != nil {
		return erroCfg(err)
	}
	return a.emit(r, func(w io.Writer) error {
		if len(r.Mudancas) == 0 {
			_, err := fmt.Fprintf(w, "%s: nada a mudar\n", q(r.Config))
			return err
		}
		if _, err := fmt.Fprintf(w, "%s:\n", q(r.Config)); err != nil {
			return err
		}
		for _, m := range r.Mudancas {
			if _, err := fmt.Fprintf(w, "  %s\n", q(m)); err != nil {
				return err
			}
		}
		if r.Backup != nil {
			if _, err := fmt.Fprintf(w, "backup: %s\n", q(*r.Backup)); err != nil {
				return err
			}
		}
		if r.Check != nil {
			if _, err := fmt.Fprintf(w, "herdr config check: %s\n", q(*r.Check)); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintln(w, "o Herdr relê o config sozinho; se não, prefix+shift+r")
		return err
	})
}

// Item é uma conferência do doctor.
type Item struct {
	Nome    string `json:"nome"`
	Estado  string `json:"estado"` // ok, aviso, erro
	Detalhe string `json:"detalhe"`
	// Origem, nos binários, diz de onde veio o caminho: variavel, orq.env, HERDR_BIN_PATH ou diretorio.
	Origem string `json:"origem,omitempty"`
}

// Doctor é a saída de orq plugin doctor.
type Doctor struct {
	OK    bool   `json:"ok"`
	Itens []Item `json:"itens"`
}

func (a *App) doctor(ctx context.Context, config string) Doctor {
	d := Doctor{OK: true, Itens: []Item{}}
	add := func(nome, estado, detalhe string) {
		d.Itens = append(d.Itens, Item{Nome: nome, Estado: estado, Detalhe: detalhe})
		if estado == "erro" {
			d.OK = false
		}
	}
	a.doctorAmbiente(add)
	f := enriquecer.Ferramentas{Env: a.Env, OrigemVar: a.origemVar, Timeout: 10 * time.Second}
	binarios := []string{"herdr", "glab", "gh", "taiga"}
	for _, n := range binarios {
		r, err := f.Resolver(n)
		switch {
		case err == nil:
			add("binario "+n, "ok", r.Caminho)
			d.Itens[len(d.Itens)-1].Origem = r.Origem
		case n == "herdr":
			add("binario "+n, "erro", err.Error())
		default:
			add("binario "+n, "aviso", err.Error())
		}
	}
	a.doctorMesmoCaminho(add, binarios)
	dir, err := central.DirPadrao(a.Env)
	if err != nil {
		add("estado", "erro", err.Error())
	} else {
		if ativo, pid, err := enriquecer.DaemonAtivo(dir); err != nil {
			add("daemon", "erro", err.Error())
		} else if ativo {
			add("daemon", "ok", "pid "+pid+" (trava em "+dir+")")
		} else {
			add("daemon", "erro", "nenhum orq daemon com o estado "+dir+": o plugin o sobe no início do servidor do Herdr; à mão, orq daemon --detach")
		}
	}
	var pl struct {
		Plugins []struct {
			ID      string `json:"plugin_id"`
			Enabled bool   `json:"enabled"`
			Root    string `json:"plugin_root"`
		} `json:"plugins"`
	}
	if err := herdrJSON(ctx, f, &pl, "plugin", "list", "--json"); err != nil {
		add("plugin", "erro", err.Error())
	} else {
		achou := false
		for _, p := range pl.Plugins {
			if p.ID != IDPlugin {
				continue
			}
			achou = true
			if p.Enabled {
				add("plugin", "ok", "registrado e habilitado em "+p.Root)
			} else {
				add("plugin", "erro", "registrado e desabilitado: herdr plugin enable "+IDPlugin)
			}
		}
		if !achou {
			add("plugin", "erro", "não registrado: herdr plugin install BasisTI/orq")
		}
	}
	b, err := os.ReadFile(config)
	switch {
	case errors.Is(err, os.ErrNotExist):
		add("config", "erro", config+" não existe: orq plugin configurar")
	case err != nil:
		add("config", "erro", err.Error())
	default:
		s, err := herdrcfg.Ler(b)
		if err != nil {
			add("config", "erro", err.Error())
			break
		}
		linha := func(nome string, tem bool, token string) {
			if tem {
				add(nome, "ok", token+" em "+config)
			} else {
				add(nome, "erro", "falta "+token+": orq plugin configurar")
			}
		}
		linha("sidebar agentes", s.LinhaAgentes, herdrcfg.TokenAgentes)
		linha("sidebar espaços", s.LinhaEspacos, herdrcfg.TokenEspacos)
		if t, ok := s.Teclas[herdrcfg.AcaoPopup]; ok {
			add("tecla popup-todos", "ok", t)
		} else {
			add("tecla popup-todos", "erro", "sem tecla para "+herdrcfg.AcaoPopup+": orq plugin configurar")
		}
		if t, ok := s.Teclas[herdrcfg.AcaoCentral]; ok {
			add("tecla abrir-central", "ok", t)
		} else {
			add("tecla abrir-central", "aviso", "sem tecla para "+herdrcfg.AcaoCentral)
		}
		for _, av := range s.Avisos {
			add("sidebar", "aviso", av)
		}
	}
	if dir != "" {
		st, err := a.store(ctx)
		if err != nil {
			add("banco", "erro", output.AsError(err).Cause)
		} else if snap, err := st.Snapshot(ctx, false); err != nil {
			add("banco", "erro", err.Error())
		} else {
			ondas := 0
			for _, c := range snap.Coordenadores {
				ondas += len(c.Ondas)
			}
			add("banco", "ok", fmt.Sprintf("%s: %d coordenadores, %d ondas abertas, %d pendências abertas", dir, len(snap.Coordenadores), ondas, len(snap.PendenciasAbertas)))
			for _, fo := range central.Fontes {
				x := snap.Fontes[fo]
				switch {
				case x.Erro != nil:
					add("fonte "+fo, "aviso", x.Erro.Codigo+": "+x.Erro.Mensagem)
				case x.AtualizadoEm == nil:
					add("fonte "+fo, "aviso", "nunca consultada")
				default:
					add("fonte "+fo, "ok", "atualizada em "+*x.AtualizadoEm)
				}
			}
		}
	}
	return d
}

// origemVar diz "orq.env" para uma variável que só o arquivo fornece, "" para a do processo.
func (a *App) origemVar(k string) string {
	if a.envProcesso != nil && a.envProcesso(k) == "" && a.arquivoEnv[k] != "" {
		return enriquecer.OrigemOrqEnv
	}
	return ""
}

// doctorAmbiente mostra o orq.env que os comandos leem.
func (a *App) doctorAmbiente(add func(nome, estado, detalhe string)) {
	dir := DirConfigPlugin(a.Env)
	if dir == "" {
		add("orq.env", "aviso", "sem HERDR_PLUGIN_CONFIG_DIR, XDG_CONFIG_HOME nem HOME: o arquivo não é lido")
		return
	}
	arq := filepath.Join(dir, ArquivoAmbientePlugin)
	if _, err := os.Stat(arq); err != nil {
		add("orq.env", "ok", arq+" não existe (opcional)")
		return
	}
	add("orq.env", "ok", fmt.Sprintf("%s: %d chaves ORQ_ lidas", arq, len(a.arquivoEnv)))
}

// doctorMesmoCaminho compara o caminho que o daemon resolve com o do shell atual. O daemon
// nasce do plugin, com o ambiente do servidor do Herdr: só o orq.env fornece ORQ_*, e uma
// ORQ_*_BIN exportada no shell não chega a ele. O shell vê o ambiente do processo mais o orq.env.
func (a *App) doctorMesmoCaminho(add func(nome, estado, detalhe string), nomes []string) {
	daemon := enriquecer.Ferramentas{Timeout: 10 * time.Second, Env: func(k string) string {
		if strings.HasPrefix(k, "ORQ_") {
			return a.arquivoEnv[k]
		}
		if a.envProcesso != nil {
			return a.envProcesso(k)
		}
		return a.Env(k)
	}}
	shell := enriquecer.Ferramentas{Env: a.Env}
	var difere []string
	for _, n := range nomes {
		rd, errD := daemon.Resolver(n)
		rs, errS := shell.Resolver(n)
		switch {
		case errD != nil && errS != nil:
		case errD != nil:
			difere = append(difere, fmt.Sprintf("%s: daemon não acha, shell usa %s", n, rs.Caminho))
		case errS != nil:
			difere = append(difere, fmt.Sprintf("%s: daemon usa %s, shell não acha", n, rd.Caminho))
		case rd.Caminho != rs.Caminho:
			difere = append(difere, fmt.Sprintf("%s: daemon usa %s, shell usa %s", n, rd.Caminho, rs.Caminho))
		}
	}
	if len(difere) == 0 {
		add("daemon e shell", "ok", "resolvem o mesmo caminho em "+strings.Join(nomes, ", "))
		return
	}
	add("daemon e shell", "aviso", strings.Join(difere, "; ")+": ponha a variável ORQ_<NOME>_BIN no orq.env")
}

func textoDoctor(w io.Writer, d Doctor) error {
	for _, it := range d.Itens {
		marca := map[string]string{"ok": "ok   ", "aviso": "aviso", "erro": "ERRO "}[it.Estado]
		detalhe := q(it.Detalhe)
		if it.Origem != "" {
			detalhe += " (" + it.Origem + ")"
		}
		if _, err := fmt.Fprintf(w, "%s  %-20s %s\n", marca, it.Nome, detalhe); err != nil {
			return err
		}
	}
	res := "tudo certo"
	if !d.OK {
		res = "falta algo (veja ERRO acima)"
	}
	_, err := fmt.Fprintln(w, res)
	return err
}
