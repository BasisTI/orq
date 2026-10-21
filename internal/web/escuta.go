// Package web serve as pendências numa página: a lista, o detalhe renderizado e o formulário de
// resposta. Roda dentro do daemon (orq daemon --web), renderiza no servidor e não precisa de JS.
package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/BasisTI/orq/internal/output"
)

// Escuta é o socket em que a web escuta: Unix (atrás do Caddy, que autentica) ou TCP no
// loopback (uso local, sem autenticação).
type Escuta struct {
	Listener net.Listener
	// Unix: o cabeçalho de usuário é aceito. Em TCP ele é ignorado.
	Unix bool
	// Descricao é o endereço, para o log.
	Descricao string
	// Porta é a porta TCP; zero no socket Unix.
	Porta int
}

// Close fecha o socket (o Unix é removido do disco).
func (e *Escuta) Close() error { return e.Listener.Close() }

func falhaAbrir(cause, recovery string) error {
	return &output.Error{Code: "web_nao_abriu", Cause: cause, Recovery: recovery, Exit: output.ExitConflict}
}

func usoWeb(cause, recovery string) error {
	return &output.Error{Code: "uso", Cause: cause, Recovery: recovery, Exit: output.ExitUsage}
}

// SocketPadrao é $XDG_RUNTIME_DIR/orq/web.sock.
func SocketPadrao(env func(string) string) (string, error) {
	x := env("XDG_RUNTIME_DIR")
	if x == "" || !filepath.IsAbs(x) {
		return "", usoWeb("sem $XDG_RUNTIME_DIR para achar o socket padrão da web", "passe --web unix:/caminho/web.sock")
	}
	return filepath.Join(x, "orq", "web.sock"), nil
}

// Abrir interpreta --web e abre o socket: "unix:<caminho>" (sem caminho, o padrão), ou
// "127.0.0.1:<porta>" ("localhost" e "[::1]" também). Qualquer outro endereço é recusado: a web
// não escuta fora do loopback.
func Abrir(spec string, env func(string) string) (*Escuta, error) {
	if c, ok := strings.CutPrefix(spec, "unix:"); ok {
		if c == "" {
			var err error
			if c, err = SocketPadrao(env); err != nil {
				return nil, err
			}
		}
		return abrirUnix(c)
	}
	host, porta, err := net.SplitHostPort(spec)
	if err != nil {
		return nil, usoWeb(fmt.Sprintf("--web %q inválido", spec), "use unix:<caminho> ou 127.0.0.1:<porta>")
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return nil, usoWeb(fmt.Sprintf("--web %q escutaria fora do loopback", spec), "a web só escuta em 127.0.0.1; para outro dispositivo, ponha o Caddy na frente do socket Unix")
	}
	l, err := net.Listen("tcp", net.JoinHostPort(host, porta))
	if err != nil {
		return nil, falhaAbrir(err.Error(), "escolha outra porta ou veja quem usa esta (ss -ltnp)")
	}
	return &Escuta{Listener: l, Descricao: "http://" + l.Addr().String(), Porta: l.Addr().(*net.TCPAddr).Port}, nil
}

func abrirUnix(caminho string) (*Escuta, error) {
	if !filepath.IsAbs(caminho) {
		return nil, usoWeb(fmt.Sprintf("o socket %q precisa de caminho absoluto", caminho), "")
	}
	caminho = filepath.Clean(caminho)
	if err := os.MkdirAll(filepath.Dir(caminho), 0o700); err != nil {
		return nil, falhaAbrir(err.Error(), "confira as permissões de "+filepath.Dir(caminho))
	}
	if st, err := os.Lstat(caminho); err == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return nil, falhaAbrir(caminho+" existe e não é um socket", "mova o arquivo ou escolha outro caminho")
		}
		// Um socket que aceita conexão tem dono vivo; um que recusa é sobra de um daemon que caiu.
		c, err := net.DialTimeout("unix", caminho, time.Second)
		if err == nil {
			_ = c.Close()
			return nil, falhaAbrir("já há alguém escutando em "+caminho, "só um daemon por socket; pare o outro ou escolha outro caminho")
		}
		if !errors.Is(err, syscall.ECONNREFUSED) {
			return nil, falhaAbrir(fmt.Sprintf("não consegui testar o socket %s: %v", caminho, err), "")
		}
		if err := os.Remove(caminho); err != nil {
			return nil, falhaAbrir(err.Error(), "")
		}
	}
	l, err := net.Listen("unix", caminho)
	if err != nil {
		return nil, falhaAbrir(err.Error(), "")
	}
	if err := os.Chmod(caminho, 0o600); err != nil {
		_ = l.Close()
		return nil, falhaAbrir(err.Error(), "")
	}
	return &Escuta{Listener: l, Unix: true, Descricao: "unix:" + caminho}, nil
}

// contextoDe devolve um contexto para a parada do servidor com prazo.
func contextoDe(base context.Context, prazo time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(base), prazo)
}
