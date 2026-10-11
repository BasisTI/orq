package enriquecer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
)

// ErrEventosPerdidos é o events_lost do Herdr: o assinante ficou para trás, a conexão foi
// fechada e o estado em cache tem de ser reconciliado com uma leitura completa.
var ErrEventosPerdidos = errors.New("herdr: events_lost")

// CaminhoSocket resolve o socket do Herdr: $HERDR_SOCKET_PATH, a sessão nomeada de
// $HERDR_SESSION ou o socket padrão em ~/.config/herdr.
func CaminhoSocket(env func(string) string) string {
	if p := env("HERDR_SOCKET_PATH"); p != "" {
		return p
	}
	base := env("XDG_CONFIG_HOME")
	if base == "" || !filepath.IsAbs(base) {
		base = filepath.Join(env("HOME"), ".config")
	}
	if s := env("HERDR_SESSION"); s != "" {
		return filepath.Join(base, "herdr", "sessions", s, "herdr.sock")
	}
	return filepath.Join(base, "herdr", "herdr.sock")
}

// EventoHerdr é um evento recebido na assinatura.
type EventoHerdr struct {
	Tipo        string `json:"event"`
	PaneID      string `json:"pane_id"`
	AgentStatus string `json:"agent_status"`
}

// Assinar abre o socket, assina a mudança de status dos panes dados e os eventos de ciclo de
// vida de pane, chama iniciou depois que o Herdr confirma e aoEvento a cada evento. Volta
// quando ctx acaba (nil), quando a conexão cai ou com ErrEventosPerdidos. Pane que não existe
// faz o Herdr recusar a assinatura inteira: passe só panes vistos na última leitura.
func Assinar(ctx context.Context, caminho string, panes []string, iniciou func(), aoEvento func(EventoHerdr)) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", caminho)
	if err != nil {
		return err
	}
	defer conn.Close()
	parar := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer parar()

	subs := []map[string]string{{"type": "pane.created"}, {"type": "pane.closed"}, {"type": "pane.exited"}, {"type": "pane.agent_detected"}}
	for _, p := range panes {
		subs = append(subs, map[string]string{"type": "pane.agent_status_changed", "pane_id": p})
	}
	const id = "orq-daemon"
	req, err := json.Marshal(map[string]any{"id": id, "method": "events.subscribe", "params": map[string]any{"subscriptions": subs}})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return fimDe(ctx, err)
	}
	r := bufio.NewReader(conn)
	iniciada := false
	for {
		linha, err := r.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && len(linha) == 0 && ctx.Err() == nil {
				return errors.New("herdr fechou a conexão de eventos")
			}
			if len(linha) == 0 {
				return fimDe(ctx, err)
			}
		}
		var m struct {
			ID     *string         `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Event string          `json:"event"`
			Data  json.RawMessage `json:"data"`
		}
		if json.Unmarshal(linha, &m) != nil {
			continue
		}
		switch {
		case m.Error != nil:
			if m.Error.Code == "events_lost" {
				return ErrEventosPerdidos
			}
			return fmt.Errorf("herdr recusou a assinatura: %s: %s", m.Error.Code, m.Error.Message)
		case m.Event != "":
			ev := EventoHerdr{Tipo: m.Event}
			_ = json.Unmarshal(m.Data, &ev)
			ev.Tipo = m.Event
			if ev.PaneID == "" {
				// Eventos de ciclo de vida trazem o pane dentro de "pane".
				var p struct {
					Pane struct {
						PaneID string `json:"pane_id"`
					} `json:"pane"`
				}
				if json.Unmarshal(m.Data, &p) == nil {
					ev.PaneID = p.Pane.PaneID
				}
			}
			if aoEvento != nil {
				aoEvento(ev)
			}
		case m.ID != nil && *m.ID == id && m.Result != nil && !iniciada:
			iniciada = true
			if iniciou != nil {
				iniciou()
			}
		}
		if err != nil {
			return fimDe(ctx, err)
		}
	}
}

// fimDe devolve nil quando a conexão caiu porque ctx acabou.
func fimDe(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	return err
}
