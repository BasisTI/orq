package upgrade

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/output"
)

// Módulo e pacote que o go install baixa; o plugin vem de BasisTI/orq no Herdr.
const (
	PacoteGo    = "github.com/BasisTI/orq/cmd/orq"
	RepoPlugin  = "BasisTI/orq"
	RefPadrao   = "main"
	estadoOK    = "ok"
	estadoPulou = "pulado"
	estadoFalha = "falhou"
)

// Opcoes ajusta uma execução do upgrade.
type Opcoes struct {
	Ref string
	// Log recebe o andamento, uma linha por etapa; pode ser nil.
	Log func(format string, args ...any)
	// EsperaParada é quanto esperar o daemon antigo soltar a trava; zero usa 30 s.
	EsperaParada time.Duration
	// TempoInstalacao limita cada go install e cada build do plugin; zero usa 10 min.
	TempoInstalacao time.Duration
}

// Etapa é um passo do upgrade e o que aconteceu com ele.
type Etapa struct {
	Nome    string `json:"nome"`
	Estado  string `json:"estado"` // ok, pulado, falhou
	Detalhe string `json:"detalhe,omitempty"`
}

// Atraso é um binário ou processo que não ficou na versão de referência.
type Atraso struct {
	Quem   string `json:"quem"`
	Motivo string `json:"motivo"`
}

// TUI é um `orq tui` aberto que segue com o binário antigo.
type TUI struct {
	PID    string `json:"pid"`
	Versao string `json:"versao,omitempty"`
}

// Resultado é a saída de orq upgrade.
type Resultado struct {
	OK     bool    `json:"ok"`
	Ref    string  `json:"ref"`
	Etapas []Etapa `json:"etapas"`
	// Referencia é o commit em que todos os binários têm de estar: o do que o upgrade instalou.
	Referencia string                `json:"referencia,omitempty"`
	Antes      []*Binario            `json:"antes"`
	Depois     []*Binario            `json:"depois"`
	Atrasados  []Atraso              `json:"atrasados"`
	TUIs       []TUI                 `json:"tuis_para_reabrir"`
	Banco      *central.EstadoVersao `json:"banco,omitempty"`
	Avisos     []string              `json:"avisos,omitempty"`
}

func (r *Resultado) etapa(nome, estado, detalhe string) {
	r.Etapas = append(r.Etapas, Etapa{nome, estado, detalhe})
}

func (r *Resultado) falhou() bool {
	for _, e := range r.Etapas {
		if e.Estado == estadoFalha {
			return true
		}
	}
	return false
}

// ValidarRef recusa um ref que o go install ou o Herdr leriam como opção ou que não é um nome
// de branch, tag ou commit.
func ValidarRef(ref string) error {
	if ref == "" || strings.HasPrefix(ref, "-") || strings.ContainsFunc(ref, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._/+-", r))
	}) {
		return fmt.Errorf("ref %q inválido: use o nome de uma branch, tag ou commit", ref)
	}
	return nil
}

// goBin acha o go: ORQ_GO_BIN, os lugares de instalação e, por último, o PATH. O upgrade roda de
// um terminal, onde o PATH tem o go; o daemon e as ações do plugin é que não têm.
func (a Ambiente) goBin() (string, error) {
	f := a.ferramentas()
	p, err := f.Caminho("go")
	if err == nil {
		return p, nil
	}
	if p, lerr := exec.LookPath("go"); lerr == nil && a.Env("PATH") == os.Getenv("PATH") {
		return p, nil
	}
	for _, d := range filepath.SplitList(a.Env("PATH")) {
		if d != "" && filepath.IsAbs(d) && executavel(filepath.Join(d, "go")) {
			return filepath.Join(d, "go"), nil
		}
	}
	return "", err
}

// Executar atualiza os binários, reinicia o daemon e confere as versões. Devolve sempre o
// resultado; o erro, quando há, descreve o que ficou para trás.
func Executar(ctx context.Context, a Ambiente, o Opcoes) (*Resultado, error) {
	if err := ValidarRef(o.Ref); err != nil {
		return nil, &output.Error{Code: "uso", Cause: err.Error(), Exit: output.ExitUsage}
	}
	if o.Log == nil {
		o.Log = func(string, ...any) {}
	}
	if o.EsperaParada == 0 {
		o.EsperaParada = 30 * time.Second
	}
	if o.TempoInstalacao == 0 {
		o.TempoInstalacao = 10 * time.Minute
	}
	r := &Resultado{Ref: o.Ref, Etapas: []Etapa{}, Atrasados: []Atraso{}, TUIs: []TUI{}}

	antes := a.Conhecidos(ctx)
	r.Antes = antes
	hoje := temRotulo(antes, RotuloGoInstall)
	plugin, errPlugin := a.pluginRaiz(ctx)

	var instalados []string // rótulos dos que o upgrade atualizou
	// go install
	switch {
	case !hoje:
		r.etapa("go install", estadoPulou, "não há orq do go install distinto do do plugin")
	default:
		o.Log("go install %s@%s", PacoteGo, o.Ref)
		if err := a.goInstall(ctx, o); err != nil {
			r.etapa("go install", estadoFalha, err.Error())
		} else {
			r.etapa("go install", estadoOK, PacoteGo+"@"+o.Ref)
			instalados = append(instalados, RotuloGoInstall)
		}
	}
	// plugin
	switch {
	case errors.Is(errPlugin, errNaoInstalado):
		r.etapa("plugin", estadoPulou, errPlugin.Error())
	case errPlugin != nil:
		r.etapa("plugin", estadoFalha, "não consegui consultar o Herdr: "+errPlugin.Error())
	default:
		o.Log("herdr plugin install %s --ref %s --yes (%s)", RepoPlugin, o.Ref, plugin)
		if err := a.instalarPlugin(ctx, o); err != nil {
			r.etapa("plugin", estadoFalha, err.Error())
		} else {
			r.etapa("plugin", estadoOK, RepoPlugin+" --ref "+o.Ref)
			instalados = append(instalados, RotuloPlugin)
		}
	}

	depoisInstalar := a.Descobrir(ctx)
	Versoes(ctx, depoisInstalar)
	for _, rot := range instalados {
		for _, b := range depoisInstalar {
			if b.tem(rot) && b.Commit != "" {
				r.Referencia = b.Commit
				break
			}
		}
		if r.Referencia != "" {
			break
		}
	}
	if len(instalados) == 0 || r.Referencia == "" {
		r.Depois = a.Conhecidos(ctx)
		return r, a.erroSemReferencia(r, len(instalados) > 0)
	}

	// Daemon: só depois de os binários estarem no lugar, e só se um deles ficou novo.
	a.reiniciarDaemon(ctx, r, o, depoisInstalar)

	// Conferência final, com tudo o que existe agora.
	r.Depois = a.Conhecidos(ctx)
	if e, existe, err := central.LerEstado(ctx, a.Dir); err == nil && existe {
		r.Banco = &e
	}
	for _, b := range r.Depois {
		if motivo := a.atraso(b, r); motivo != "" {
			r.Atrasados = append(r.Atrasados, Atraso{Quem: nome(b), Motivo: motivo})
		}
	}
	r.TUIs = tuisAbertos(r.Referencia)
	r.OK = len(r.Atrasados) == 0 && !r.falhou()
	if r.OK {
		return r, nil
	}
	return r, erroIncompleto(r)
}

func temRotulo(bs []*Binario, rot string) bool {
	for _, b := range bs {
		if b.tem(rot) {
			return true
		}
	}
	return false
}

func nome(b *Binario) string {
	n := strings.Join(b.Rotulos, " + ")
	if b.tem(RotuloDaemon) {
		return "daemon (pid " + b.PID + ")"
	}
	return n + " (" + b.Caminho + ")"
}

// atraso diz por que b não está na versão de referência; "" quando está.
func (a Ambiente) atraso(b *Binario, r *Resultado) string {
	switch {
	case b.Erro != "":
		return "não respondeu a `version`: " + b.Erro
	case b.Commit == "":
		return fmt.Sprintf("versão %q não diz o commit, então não dá para conferir", b.Versao)
	case MesmoCommit(b.Commit, r.Referencia):
		return ""
	}
	m := fmt.Sprintf("está em %s (versão %s) e a referência é %s", abrev(b.Commit), b.Versao, abrev(r.Referencia))
	if r.Banco != nil {
		schema := b.Schema
		if schema == 0 && r.Banco.Banco > 3 {
			m += fmt.Sprintf("; é anterior ao leitor mínimo: NÃO abre o banco (versão %d)", r.Banco.Banco)
		} else if schema == 0 {
			m += fmt.Sprintf("; é anterior ao leitor mínimo: ainda abre o banco (versão %d), mas não abrirá o da próxima migration", r.Banco.Banco)
		} else if !r.Banco.PodeLer(schema) {
			m += fmt.Sprintf("; NÃO abre o banco (versão %d, exige orq com a migration %d ou mais nova, este chega à %d)", r.Banco.Banco, r.Banco.LeitorMinimo, schema)
		} else if !r.Banco.PodeEscrever(schema) {
			m += "; abre o banco só para leitura"
		}
	}
	if !b.tem(RotuloDaemon) && !b.tem(RotuloGoInstall) && !b.tem(RotuloPlugin) {
		m += "; não é o orq do go install nem o do plugin, então o upgrade não o atualiza: troque-o à mão"
	}
	return m
}

func abrev(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

func (a Ambiente) goInstall(ctx context.Context, o Opcoes) error {
	bin, err := a.goBin()
	if err != nil {
		return fmt.Errorf("go não encontrado: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, o.TempoInstalacao)
	defer cancel()
	// Fora de qualquer módulo: o go install com @versão ignora o go.mod do diretório atual.
	saida, err := rodarEm(ctx, os.TempDir(), nil, bin, "install", PacoteGo+"@"+o.Ref)
	if err != nil {
		return fmt.Errorf("go install %s@%s falhou: %v: %s", PacoteGo, o.Ref, err, resumo(saida))
	}
	return nil
}

func (a Ambiente) instalarPlugin(ctx context.Context, o Opcoes) error {
	f := a.ferramentas()
	f.Timeout = o.TempoInstalacao
	if _, err := f.Rodar(ctx, "herdr", "plugin", "install", RepoPlugin, "--ref", o.Ref, "--yes"); err != nil {
		return fmt.Errorf("herdr plugin install %s --ref %s --yes falhou: %v", RepoPlugin, o.Ref, err)
	}
	return nil
}

// reiniciarDaemon troca o daemon pelo da versão nova: para o antigo, espera a trava soltar e
// sobe o novo. Se nenhum binário novo sobe, volta ao que rodava antes; o daemon só fica parado
// se nem isso for possível, e a etapa diz.
func (a Ambiente) reiniciarDaemon(ctx context.Context, r *Resultado, o Opcoes, bs []*Binario) {
	const nomeEtapa = "daemon"
	velho := a.Daemon()
	if velho != nil {
		Versoes(ctx, []*Binario{velho})
		if velho.Erro == "" && MesmoCommit(velho.Commit, r.Referencia) {
			r.etapa(nomeEtapa, estadoPulou, "o daemon (pid "+velho.PID+") já roda a versão "+abrev(r.Referencia))
			return
		}
	}
	var (
		args   []string
		env    []string
		backup string
	)
	if velho != nil {
		args = argsDoDaemon(velho.PID)
		env = ambienteDoProcesso(velho.PID)
		backup = filepath.Join(a.Dir, "upgrade", "orq-anterior")
		if err := copiarExecutavel(velho.Caminho, backup); err != nil {
			backup = ""
			r.Avisos = append(r.Avisos, "não guardei cópia do daemon antigo para voltar atrás: "+err.Error())
		}
		defer os.RemoveAll(filepath.Join(a.Dir, "upgrade"))
		o.Log("parando o daemon antigo (pid %s)", velho.PID)
		if err := a.parar(ctx, velho.PID, o.EsperaParada); err != nil {
			r.etapa(nomeEtapa, estadoFalha, "o daemon antigo segue rodando, sem a versão nova: "+err.Error())
			return
		}
	}
	if env == nil {
		env = os.Environ()
	}
	// Candidatos: os binários que ficaram na versão nova, o do plugin primeiro (é o que o
	// startup do Herdr usa), e por último o que rodava antes.
	var candidatos []string
	vistos := map[*Binario]bool{}
	for _, rot := range []string{RotuloPlugin, RotuloGoInstall, RotuloPATH} {
		for _, b := range bs {
			if b.tem(rot) && !vistos[b] && b.Erro == "" && MesmoCommit(b.Commit, r.Referencia) {
				vistos[b] = true
				candidatos = append(candidatos, b.Caminho)
			}
		}
	}
	var falhas []string
	for _, c := range candidatos {
		o.Log("subindo o daemon novo com %s", c)
		pid, err := a.subir(ctx, c, env, args)
		if err == nil {
			r.etapa(nomeEtapa, estadoOK, fmt.Sprintf("daemon novo, pid %s, de %s", pid, c))
			return
		}
		falhas = append(falhas, err.Error())
	}
	if backup != "" {
		o.Log("o daemon novo não subiu; voltando ao anterior")
		if pid, err := a.subir(ctx, backup, env, args); err == nil {
			r.etapa(nomeEtapa, estadoFalha, fmt.Sprintf("o daemon novo não subiu (%s); voltei ao daemon anterior (pid %s, versão %s)", strings.Join(falhas, "; "), pid, abrev(velho.Commit)))
			return
		} else {
			falhas = append(falhas, "daemon anterior: "+err.Error())
		}
	}
	r.etapa(nomeEtapa, estadoFalha, "daemon PARADO: nenhum candidato subiu ("+strings.Join(falhas, "; ")+"); suba com `orq daemon --detach`")
}

func (a Ambiente) erroSemReferencia(r *Resultado, instalou bool) error {
	cause := "nenhum binário foi atualizado"
	for _, e := range r.Etapas {
		if e.Estado != estadoOK {
			cause += "; " + e.Nome + " " + e.Estado + ": " + e.Detalhe
		}
	}
	if instalou {
		cause = "o upgrade instalou, mas nenhum binário atualizado informa o commit, então não dá para conferir as versões"
	}
	rec := "o daemon não foi tocado e segue rodando; corrija o que está acima e rode `orq upgrade` de novo"
	return &output.Error{Code: "upgrade_incompleto", Cause: cause, Recovery: rec, Exit: output.ExitConflict}
}

func erroIncompleto(r *Resultado) error {
	var partes []string
	for _, e := range r.Etapas {
		if e.Estado == estadoFalha {
			partes = append(partes, e.Nome+": "+e.Detalhe)
		}
	}
	for _, at := range r.Atrasados {
		partes = append(partes, "ficou para trás: "+at.Quem+": "+at.Motivo)
	}
	return &output.Error{
		Code:     "upgrade_incompleto",
		Cause:    strings.Join(partes, "; "),
		Recovery: "corrija o que está acima e rode `orq upgrade` de novo: ele só repete o que falta (o daemon que já roda a versão nova não é reiniciado)",
		Exit:     output.ExitConflict,
	}
}
