package pendencias

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
)

// Estados do resultado de uma importação.
const (
	EstadoRegistrada   = "registrada"    // pendência nova
	EstadoJaRegistrada = "ja_registrada" // o slug já tinha pendência; nada novo
	EstadoErro         = "erro"          // o arquivo não vira pendência; o motivo está no .erro.txt
)

// Resultado é o desfecho de um arquivo.
type Resultado struct {
	Arquivo string `json:"arquivo"`
	Estado  string `json:"estado"`
	ID      string `json:"id,omitempty"`
	Chave   string `json:"chave,omitempty"`
	URL     string `json:"url,omitempty"`
	// Alterado: o arquivo mudou depois do registro (pend.arquivo_alterado gravado agora).
	Alterado bool   `json:"alterado,omitempty"`
	Erro     string `json:"erro,omitempty"`
}

// Registrada é o conteúdo de <slug>.registrada.json, o que o worker lê para citar a pendência.
type Registrada struct {
	ID    string `json:"id"`
	Chave string `json:"chave"`
	URL   string `json:"url"`
}

// Importador transforma os arquivos de pendência da pasta de trabalho em pendências da Central.
// O daemon (Varrer) e orq pend importar (Importar) usam o mesmo caminho.
type Importador struct {
	St *central.Store
	// Raiz é o ORQ_TRABALHO_DIR; nada fora dela é lido ou escrito.
	Raiz string
	// BaseURL é a URL pública da web (ORQ_WEB_URL); vazia, o registro traz só o caminho.
	BaseURL string
	Log     func(format string, args ...any)
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// local é o que o caminho do arquivo diz: projeto, US e slug. O conteúdo não decide isso.
type local struct {
	us   refs.US
	slug string
	dir  string // caminho absoluto da pasta, para as mensagens
	path string // caminho absoluto do arquivo, para as mensagens
	// rel e dirRel são os mesmos caminhos relativos à raiz: é por eles, e só por eles, que o
	// importador lê e escreve (os.Root).
	rel, dirRel string
}

func (i *Importador) logf(f string, a ...any) {
	if i.Log != nil {
		i.Log(f, a...)
	}
}

// Ignorado diz se o nome é de arquivo que o importador não olha: oculto ou temporário.
func Ignorado(nome string) bool {
	return strings.HasPrefix(nome, ".") || strings.HasSuffix(nome, ".tmp") || !strings.HasSuffix(nome, ".md")
}

// localDe lê o caminho: <raiz>/<projeto>/TG-<ref>/pendencias/<slug>.md (coordenador) ou
// <raiz>/<projeto>/TG-<ref>/<papel>/<rodada>/pendencias/<slug>.md (worker).
func (i *Importador) localDe(arquivo string) (local, error) {
	if !filepath.IsAbs(arquivo) {
		return local{}, usoImport("o caminho do arquivo precisa ser absoluto", "")
	}
	arquivo = filepath.Clean(arquivo)
	rel, err := filepath.Rel(filepath.Clean(i.Raiz), arquivo)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return local{}, usoImport(fmt.Sprintf("%s fica fora de ORQ_TRABALHO_DIR (%s)", arquivo, i.Raiz), "grave o arquivo em <ORQ_TRABALHO_DIR>/<projeto>/TG-<ref>/…/pendencias/<slug>.md")
	}
	partes := strings.Split(rel, string(filepath.Separator))
	n := len(partes)
	if (n != 4 && n != 6) || partes[n-2] != "pendencias" {
		return local{}, usoImport(fmt.Sprintf("o caminho %s não é <projeto>/TG-<ref>[/<papel>/<rodada>]/pendencias/<slug>.md", rel), "")
	}
	nome := partes[n-1]
	if Ignorado(nome) {
		return local{}, usoImport(fmt.Sprintf("%s é oculto, temporário ou não termina em .md", nome), "")
	}
	slug := strings.TrimSuffix(nome, ".md")
	if !slugRe.MatchString(slug) {
		return local{}, usoImport(fmt.Sprintf("slug %q inválido", slug), "use minúsculas, dígitos, - e _ (até 64), ex. decisao-yaml.md")
	}
	projeto, err := refs.ValidarProjeto(partes[0])
	if err != nil || projeto != partes[0] {
		return local{}, usoImport(fmt.Sprintf("projeto %q no caminho inválido", partes[0]), "")
	}
	num, ok := strings.CutPrefix(partes[1], "TG-")
	ref, err := strconv.Atoi(num)
	if !ok || err != nil || ref <= 0 || strings.HasPrefix(num, "+") {
		return local{}, usoImport(fmt.Sprintf("pasta %q não é TG-<ref>", partes[1]), "")
	}
	for _, p := range partes[2 : n-2] {
		if strings.HasPrefix(p, ".") {
			return local{}, usoImport("pasta oculta no caminho", "")
		}
	}
	return local{us: refs.US{Projeto: projeto, Ref: ref}, slug: slug, dir: filepath.Dir(arquivo), path: arquivo, rel: rel, dirRel: filepath.Dir(rel)}, nil
}

func usoImport(cause, recovery string) error {
	return &output.Error{Code: "uso", Cause: cause, Recovery: recovery, Exit: output.ExitUsage}
}

// errFora é o erro de um caminho que sai da raiz (symlink para fora, ou um ".." que o os.Root recusa).
func errFora(caminho string) error {
	return &output.Error{Code: "symlink_fora_da_raiz", Cause: fmt.Sprintf("%s resolve para fora de ORQ_TRABALHO_DIR", caminho), Recovery: "o orq não segue symlink para fora da pasta de trabalho", Exit: output.ExitConflict}
}

// foraDaRaiz diz se err é a recusa do os.Root a um caminho que escapa da raiz.
func foraDaRaiz(err error) bool {
	return err != nil && strings.Contains(err.Error(), "path escapes from parent")
}

// abrirRaiz abre ORQ_TRABALHO_DIR. Toda leitura e escrita da importação passa por ela: o os.Root
// resolve cada componente com openat e recusa o que sai da raiz, também quando uma pasta vira
// symlink depois de conferida (conferir antes e usar depois não basta). Raiz que não existe devolve
// (nil, nil): não há o que importar.
func (i *Importador) abrirRaiz() (*os.Root, error) {
	r, err := os.OpenRoot(i.Raiz)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return r, err
}

// lerArquivo lê o arquivo, só regular e com o limite de tamanho. Um FIFO plantado como x.md
// travaria o daemon no open: o tipo é conferido antes, e a abertura não bloqueia.
func lerArquivo(raiz *os.Root, rel string) ([]byte, error) {
	if st, err := raiz.Stat(rel); err != nil {
		return nil, err
	} else if !st.Mode().IsRegular() {
		return nil, &ErroArquivo{Problemas: []Problema{{Motivo: "não é um arquivo regular"}}}
	}
	f, err := raiz.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, &ErroArquivo{Problemas: []Problema{{Motivo: "não é um arquivo regular"}}}
	}
	return io.ReadAll(io.LimitReader(f, LimiteArquivo+1))
}

// Importar importa um arquivo: cria a pendência (ou reconhece a que já existe), grava
// <slug>.registrada.json e apaga o <slug>.erro.txt. Arquivo inválido grava o .erro.txt e volta
// com Estado "erro" (e sem erro Go); só falha de leitura do banco ou do disco volta como erro.
func (i *Importador) Importar(ctx context.Context, arquivo string) (Resultado, error) {
	l, err := i.localDe(arquivo)
	if err != nil {
		return Resultado{Arquivo: arquivo}, err
	}
	raiz, err := i.abrirRaiz()
	if err != nil {
		return Resultado{Arquivo: arquivo}, err
	}
	if raiz == nil {
		return Resultado{Arquivo: arquivo}, naoExiste(arquivo)
	}
	defer raiz.Close()
	return i.processar(ctx, raiz, l, nil)
}

func naoExiste(arquivo string) error {
	return &output.Error{Code: "nao_encontrado", Cause: arquivo + " não existe", Exit: output.ExitNotFound}
}

func (i *Importador) processar(ctx context.Context, raiz *os.Root, l local, conhecidas map[central.ChaveSlug]central.Importada) (Resultado, error) {
	r := Resultado{Arquivo: l.path}
	conteudo, err := lerArquivo(raiz, l.rel)
	if err != nil {
		var ea *ErroArquivo
		switch {
		case foraDaRaiz(err):
			e := errFora(l.path)
			// Só escreve ao lado do arquivo se a pasta também fica dentro da raiz: o os.Root recusa o resto.
			i.gravarErro(raiz, l, []string{output.AsError(e).Cause})
			r.Estado, r.Erro = EstadoErro, output.AsError(e).Cause
			return r, nil
		case errors.As(err, &ea):
			return i.recusar(raiz, l, r, ea), nil
		}
		return r, err
	}
	soma := sha256.Sum256(conteudo)
	hash := hex.EncodeToString(soma[:])
	chave := central.ChaveSlug{Projeto: l.us.Projeto, Ref: l.us.Ref, Slug: l.slug}
	if conhecidas == nil {
		if conhecidas, err = i.St.PendenciasImportadas(ctx); err != nil {
			return r, err
		}
	}
	if reg, ok := conhecidas[chave]; ok {
		if (hash != reg.Hash && hash != reg.Alterado) || (hash == reg.Hash && reg.Alterado != "") {
			if r.Alterado, err = i.St.ConferirArquivoPendencia(ctx, l.us, l.slug, l.path, hash); err != nil {
				return r, err
			}
			if r.Alterado {
				i.logf("pendência %s: o arquivo %s mudou depois do registro; a pendência não muda", refs.PendID(l.us.Ref, reg.Numero), l.path)
			}
		}
		return i.concluir(raiz, l, r, l.us, reg.Numero, EstadoJaRegistrada)
	}
	a, err := Ler(conteudo)
	if err != nil {
		var ea *ErroArquivo
		if errors.As(err, &ea) {
			return i.recusar(raiz, l, r, ea), nil
		}
		return r, err
	}
	res, err := i.St.RegistrarPendencia(ctx, central.NovaPendencia{US: l.us, Tipo: a.Tipo, Titulo: a.Titulo, Autor: a.Autor, Detalhe: a.Detalhe,
		Opcoes: a.Opcoes, Slug: l.slug, Arquivo: l.path, Hash: hash})
	if err != nil {
		if e := output.AsError(err); problemaDoArquivo(e) {
			return i.recusar(raiz, l, r, &ErroArquivo{Problemas: []Problema{{Motivo: e.Cause + recoveryDe(e)}}}), nil
		}
		return r, err
	}
	estado := EstadoRegistrada
	if !res.Criada {
		estado = EstadoJaRegistrada
	}
	r.Alterado = res.Alterada
	return i.concluir(raiz, l, r, l.us, res.Pendencia.Numero, estado)
}

// problemaDoArquivo separa o que é culpa do arquivo (US que não existe, onda encerrada, opção
// inválida) do que é do ambiente (banco ocupado, versão do banco): este tenta de novo depois.
func problemaDoArquivo(e *output.Error) bool {
	return e.Exit == output.ExitUsage || e.Exit == output.ExitNotFound || e.Code == "onda_encerrada"
}

func recoveryDe(e *output.Error) string {
	if e.Recovery == "" {
		return ""
	}
	return " (" + e.Recovery + ")"
}

// recusar grava o .erro.txt e devolve o resultado de erro.
func (i *Importador) recusar(raiz *os.Root, l local, r Resultado, e *ErroArquivo) Resultado {
	linhas := make([]string, len(e.Problemas))
	for k, p := range e.Problemas {
		linhas[k] = p.String()
	}
	i.gravarErro(raiz, l, linhas)
	r.Estado, r.Erro = EstadoErro, e.Error()
	return r
}

func (l local) relErro() string { return filepath.Join(l.dirRel, l.slug+".erro.txt") }

func (i *Importador) gravarErro(raiz *os.Root, l local, linhas []string) {
	texto := "Pendência não registrada: " + filepath.Base(l.path) + "\n" + strings.Join(linhas, "\n") + "\n"
	if b, ok := lerSeRegular(raiz, l.relErro()); ok && string(b) == texto {
		return
	}
	if err := escreverAtomico(raiz, l.relErro(), []byte(texto)); err != nil {
		if !foraDaRaiz(err) {
			i.logf("não gravei %s: %v", l.relErro(), err)
		}
		return
	}
	i.logf("pendência %s recusada: %s", l.path, strings.Join(linhas, "; "))
}

// concluir grava o .registrada.json (se faltar ou estiver diferente) e apaga o .erro.txt. Se a
// pasta saiu da raiz no meio do caminho, o resultado vira erro e nada é escrito fora.
func (i *Importador) concluir(raiz *os.Root, l local, r Resultado, u refs.US, numero int, estado string) (Resultado, error) {
	reg := Registrada{ID: refs.PendID(u.Ref, numero), Chave: u.Chave() + "#P" + strconv.Itoa(numero), URL: refs.URLPendencia(i.BaseURL, u.Projeto, u.Ref, numero)}
	r.Estado, r.ID, r.Chave, r.URL = estado, reg.ID, reg.Chave, reg.URL
	b, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return r, err
	}
	b = append(b, '\n')
	p := filepath.Join(l.dirRel, l.slug+".registrada.json")
	if atual, ok := lerSeRegular(raiz, p); !ok || string(atual) != string(b) {
		if err := escreverAtomico(raiz, p, b); err != nil {
			if foraDaRaiz(err) {
				r.Estado, r.Erro = EstadoErro, errFora(l.dir).(*output.Error).Cause
				return r, nil
			}
			return r, fmt.Errorf("gravar %s: %w", p, err)
		}
		if estado == EstadoJaRegistrada {
			r.Estado = EstadoRegistrada
		}
		i.logf("pendência %s registrada para %s", reg.ID, l.path)
	}
	if err := raiz.Remove(l.relErro()); err != nil && !os.IsNotExist(err) && !foraDaRaiz(err) {
		i.logf("não removi %s: %v", l.relErro(), err)
	}
	return r, nil
}

// lerSeRegular lê rel só se for arquivo regular: um FIFO no lugar do .erro.txt não trava o daemon.
func lerSeRegular(raiz *os.Root, rel string) ([]byte, bool) {
	if st, err := raiz.Lstat(rel); err != nil || !st.Mode().IsRegular() {
		return nil, false
	}
	f, err := raiz.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, LimiteArquivo))
	return b, err == nil
}

// escreverAtomico grava num arquivo oculto .tmp e renomeia: quem lê nunca vê o meio. A pasta é de
// quem escreve o arquivo de pendência (um worker): um .tmp que já exista, ou um link, não redireciona
// a escrita do daemon, e uma pasta trocada por symlink para fora da raiz é recusada pelo os.Root.
func escreverAtomico(raiz *os.Root, rel string, b []byte) error {
	tmp := filepath.Join(filepath.Dir(rel), "."+filepath.Base(rel)+".tmp")
	if err := raiz.Remove(tmp); err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := raiz.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = raiz.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = raiz.Remove(tmp)
		return err
	}
	if err := raiz.Rename(tmp, rel); err != nil {
		_ = raiz.Remove(tmp)
		return err
	}
	return nil
}

// Varrer procura os arquivos de pendência em Raiz e importa cada um. Devolve só o que mudou ou
// falhou (registrada, erro, ou arquivo alterado); o resto é silêncio. Falha de um arquivo não
// interrompe os outros; o primeiro erro de banco ou de disco volta no fim.
func (i *Importador) Varrer(ctx context.Context) ([]Resultado, error) {
	// A busca só lista nomes; o que ela acha é lido e escrito pelo os.Root, que recusa o que sai da raiz.
	var arquivos []string
	for _, padrao := range []string{"*/TG-*/pendencias/*.md", "*/TG-*/*/*/pendencias/*.md"} {
		achados, err := filepath.Glob(filepath.Join(globEscapado(i.Raiz), padrao))
		if err != nil {
			return nil, err
		}
		arquivos = append(arquivos, achados...)
	}
	sort.Strings(arquivos)
	if len(arquivos) == 0 {
		return nil, nil
	}
	raiz, err := i.abrirRaiz()
	if err != nil || raiz == nil {
		return nil, err
	}
	defer raiz.Close()
	conhecidas, err := i.St.PendenciasImportadas(ctx)
	if err != nil {
		return nil, err
	}
	var out []Resultado
	var primeiro error
	for _, a := range arquivos {
		if ctx.Err() != nil {
			break
		}
		if Ignorado(filepath.Base(a)) {
			continue
		}
		l, err := i.localDe(a)
		if err != nil {
			continue // fora do formato: não é nosso
		}
		r, err := i.processar(ctx, raiz, l, conhecidas)
		if err != nil {
			if primeiro == nil {
				primeiro = fmt.Errorf("%s: %w", a, err)
			}
			continue
		}
		if r.Estado == EstadoRegistrada || r.Estado == EstadoErro || r.Alterado {
			out = append(out, r)
		}
	}
	return out, primeiro
}

// globEscapado protege os metacaracteres do glob num caminho fixo.
func globEscapado(p string) string {
	return strings.NewReplacer("*", `\*`, "?", `\?`, "[", `\[`, `\`, `\\`).Replace(p)
}
