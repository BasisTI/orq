package enriquecer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// servidorFalso é um Herdr de mentira no socket: confirma a assinatura e roda o roteiro de cada
// conexão (uma função por conexão, na ordem; acabado o roteiro, a conexão fica aberta).
type servidorFalso struct {
	t       *testing.T
	caminho string
	ln      net.Listener
	mu      sync.Mutex
	pedidos []map[string]any
	roteiro []func(enviar func(string))
}

func novoServidor(t *testing.T, roteiro ...func(enviar func(string))) *servidorFalso {
	t.Helper()
	// Caminho curto: socket Unix tem limite de ~108 bytes.
	dir, err := os.MkdirTemp("", "orq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	s := &servidorFalso{t: t, caminho: filepath.Join(dir, "h.sock"), roteiro: roteiro}
	if s.ln, err = net.Listen("unix", s.caminho); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.ln.Close() })
	go s.servir()
	return s
}

func (s *servidorFalso) servir() {
	for n := 0; ; n++ {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go func(n int, c net.Conn) {
			defer c.Close()
			r := bufio.NewReader(c)
			linha, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			var req map[string]any
			_ = json.Unmarshal(linha, &req)
			s.mu.Lock()
			s.pedidos = append(s.pedidos, req)
			s.mu.Unlock()
			enviar := func(l string) { _, _ = c.Write([]byte(l + "\n")) }
			enviar(`{"id":"` + req["id"].(string) + `","result":{"type":"subscription_started"}}`)
			if n < len(s.roteiro) {
				s.roteiro[n](enviar)
			}
			_, _ = r.ReadBytes('\n') // segura a conexão até o cliente fechar
		}(n, c)
	}
}

func (s *servidorFalso) conexoes() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.pedidos...)
}

const eventoBlocked = `{"event":"pane.agent_status_changed","data":{"pane_id":"w1:p2","workspace_id":"w1","agent_status":"blocked"}}`

func TestAssinarEventoEEventsLost(t *testing.T) {
	s := novoServidor(t, func(enviar func(string)) {
		enviar(eventoBlocked)
		enviar(`{"event":"pane.closed","data":{"pane":{"pane_id":"w1:p9","workspace_id":"w1"}}}`)
		enviar(`{"id":"orq-daemon","error":{"code":"events_lost","message":"subscriber fell behind"}}`)
	})
	var evs []EventoHerdr
	iniciou := 0
	err := Assinar(context.Background(), s.caminho, []string{"w1:p1", "w1:p2"}, func() { iniciou++ }, func(e EventoHerdr) { evs = append(evs, e) })
	if !errors.Is(err, ErrEventosPerdidos) {
		t.Fatalf("err = %v", err)
	}
	if iniciou != 1 || len(evs) != 2 || evs[0] != (EventoHerdr{"pane.agent_status_changed", "w1:p2", "blocked"}) || evs[1].PaneID != "w1:p9" {
		t.Fatalf("iniciou %d, eventos %+v", iniciou, evs)
	}
	req := s.conexoes()[0]
	b, _ := json.Marshal(req["params"])
	if req["method"] != "events.subscribe" || !strings.Contains(string(b), `{"pane_id":"w1:p2","type":"pane.agent_status_changed"}`) {
		t.Fatalf("pedido = %v", req)
	}
}

func TestAssinarRecusadaECancelada(t *testing.T) {
	s := novoServidor(t, func(enviar func(string)) {
		enviar(`{"id":"orq-daemon","error":{"code":"not_found","message":"pane not found"}}`)
	})
	if err := Assinar(context.Background(), s.caminho, []string{"w1:p404"}, nil, nil); err == nil || !strings.Contains(err.Error(), "not_found") {
		t.Fatalf("recusa = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	fim := make(chan error, 1)
	go func() { fim <- Assinar(ctx, s.caminho, nil, func() { cancel() }, nil) }()
	select {
	case err := <-fim:
		if err != nil {
			t.Fatalf("cancelada = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Assinar não voltou com ctx cancelado")
	}
	if err := Assinar(context.Background(), filepath.Join(t.TempDir(), "nada.sock"), nil, nil, nil); err == nil {
		t.Fatal("socket inexistente devia falhar")
	}
}

func TestCaminhoSocket(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	casos := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"HERDR_SOCKET_PATH": "/x/h.sock", "HOME": "/h"}, "/x/h.sock"},
		{map[string]string{"HOME": "/h"}, "/h/.config/herdr/herdr.sock"},
		{map[string]string{"HOME": "/h", "HERDR_SESSION": "s1"}, "/h/.config/herdr/sessions/s1/herdr.sock"},
		{map[string]string{"HOME": "/h", "XDG_CONFIG_HOME": "/c"}, "/c/herdr/herdr.sock"},
	}
	for _, c := range casos {
		if got := CaminhoSocket(env(c.env)); got != c.want {
			t.Errorf("%v = %s, queria %s", c.env, got, c.want)
		}
	}
}

// O daemon assina os panes vistos, republica a cada evento, reassina e reconcilia depois de
// events_lost e limpa os tokens ao sair.
func TestDaemonEventosEReconciliacao(t *testing.T) {
	c := novoCenario(t)
	c.st.Now, c.e.Now = time.Now, time.Now
	co := c.coord("a", "w1:p1", "")
	c.onda(co, "1")
	c.status(co, "1", "exec", "w1:p2")
	c.sessao([]map[string]any{
		{"pane_id": "w1:p1", "workspace_id": "w1", "agent": "claude", "agent_status": "working"},
		{"pane_id": "w1:p2", "workspace_id": "w1", "agent": "claude", "agent_status": "working"},
	}, []map[string]any{{"workspace_id": "w1"}})
	continuar := make(chan struct{})
	s := novoServidor(t, func(enviar func(string)) {
		<-continuar
		enviar(eventoBlocked)
		<-continuar
		enviar(`{"id":"orq-daemon","error":{"code":"events_lost","message":"x"}}`)
	})
	ctx, cancel := context.WithCancel(context.Background())
	d := &Daemon{E: c.e, Socket: s.caminho, Tick: time.Hour, IntervaloRemoto: time.Hour, Reconexao: 50 * time.Millisecond}
	fim := make(chan error, 1)
	go func() { fim <- d.Rodar(ctx) }()

	snapshots := func() int {
		n := 0
		for _, a := range c.chamadas("herdr") {
			if strings.Join(a, " ") == "api snapshot" {
				n++
			}
		}
		return n
	}
	esperar := func(oque string, cond func() bool) {
		t.Helper()
		limite := time.Now().Add(10 * time.Second)
		for !cond() {
			if time.Now().After(limite) {
				t.Fatalf("esperando %s", oque)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	// Início: ciclo, assinatura e o ciclo de reconciliação que ela pede.
	vistos := 0
	esperar("assinatura e reconciliação", func() bool { vistos += snapshots(); return len(s.conexoes()) == 1 && vistos >= 2 })
	b, _ := json.Marshal(s.conexoes()[0]["params"])
	if !strings.Contains(string(b), `"w1:p1"`) || !strings.Contains(string(b), `"w1:p2"`) {
		t.Fatalf("assinatura = %s", b)
	}
	vistos = 0
	continuar <- struct{}{}
	esperar("ciclo pelo evento", func() bool { vistos += snapshots(); return vistos >= 1 })
	vistos = 0
	continuar <- struct{}{}
	esperar("reassinatura depois de events_lost", func() bool { vistos += snapshots(); return len(s.conexoes()) == 2 && vistos >= 1 })
	ts, _ := c.st.TokensPublicados(context.Background())
	if len(ts) != 1 || ts[0].Valor != "1 Exec" {
		t.Fatalf("tokens = %+v", ts)
	}
	// O Herdr passa a mostrar o token do orq: a saída relê o Herdr e limpa o que é do orq.
	c.sessao([]map[string]any{
		{"pane_id": "w1:p1", "workspace_id": "w1", "agent": "claude", "agent_status": "working", "tokens": map[string]string{"orq": "1 Exec"}},
		{"pane_id": "w1:p2", "workspace_id": "w1", "agent": "claude", "agent_status": "working"},
	}, []map[string]any{{"workspace_id": "w1"}})
	cancel()
	select {
	case err := <-fim:
		if err != nil {
			t.Fatalf("Rodar = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon não saiu com ctx cancelado")
	}
	limpou := false
	for _, a := range c.chamadas("herdr") {
		limpou = limpou || strings.Join(a, " ") == "pane report-metadata w1:p1 --source orq --clear-token orq"
	}
	if ts, _ := c.st.TokensPublicados(context.Background()); !limpou || len(ts) != 0 {
		t.Fatalf("não limpou ao sair: %+v", ts)
	}
}
