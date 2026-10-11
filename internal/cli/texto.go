package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/output"
)

// Tudo o que vai para o terminal passa por output.Quote: textos de pendência e títulos vêm de
// outros agentes e não podem quebrar ou disfarçar a linha.
var q = output.Quote

func writeJSON(w io.Writer, v any) error { return output.WriteJSON(w, v) }

func textoCoord(w io.Writer, c central.Coordenador) error {
	_, err := fmt.Fprintf(w, "%s  pane %s  workspace %s  projeto %s\n", q(c.Nome), ou(c.PaneID), ou(c.WorkspaceID), ou(c.ProjetoPadrao))
	return err
}

func textoOndaResumo(w io.Writer, o central.Onda) error {
	feitas := 0
	for _, u := range o.US {
		if u.Status == "done" {
			feitas++
		}
	}
	estado := ""
	if o.EncerradaEm != nil {
		estado = "  encerrada " + *o.EncerradaEm
	}
	_, err := fmt.Fprintf(w, "onda %d  %s  [%s · até %s · %s]  %d/%d done%s\n", o.ID, q(o.Titulo), q(o.Coordenador), o.AutorizacaoAte, o.Ordem, feitas, len(o.US), estado)
	return err
}

func textoOnda(w io.Writer, o central.Onda) error {
	if err := textoOndaResumo(w, o); err != nil {
		return err
	}
	for _, u := range o.US {
		dep := ""
		if len(u.DependeDe) > 0 {
			dep = "  depende de " + strings.Join(u.DependeDe, ", ")
		}
		if _, err := fmt.Fprintf(w, "  %d. %-20s %s%s\n", u.Posicao, u.Chave, linhaStatus(u), dep); err != nil {
			return err
		}
		for _, m := range u.MRs {
			estado := ""
			if m.Estado != nil {
				estado = " [" + q(*m.Estado) + "]"
			}
			if _, err := fmt.Fprintf(w, "       MR %s%s%s\n", m.URL, estado, iacTexto(m.IaC)); err != nil {
				return err
			}
		}
		for _, p := range u.Pendencias {
			if err := textoPendencia(w, p, "       "); err != nil {
				return err
			}
		}
	}
	return nil
}

func linhaStatus(u central.US) string {
	s := u.Status
	if u.Rodada != nil {
		s += fmt.Sprintf(" r%d", *u.Rodada)
	}
	if u.Papel != nil {
		s += " " + *u.Papel
	}
	if u.PaneID != nil {
		s += " pane " + q(*u.PaneID)
		if u.Herdr != nil && u.Herdr.Status != nil {
			s += " (" + q(*u.Herdr.Status) + ")"
		}
	}
	if u.Taiga.Status != nil {
		s += "  taiga " + q(*u.Taiga.Status)
	}
	return s
}

func textoProxima(w io.Writer, p central.Proxima) error {
	var err error
	if p.US != nil {
		_, err = fmt.Fprintf(w, "próxima: %s (%s)\n", p.US.Chave, p.US.ID)
	} else {
		_, err = fmt.Fprintf(w, "nada elegível: %s\n", q(p.Motivo))
	}
	if err != nil {
		return err
	}
	if len(p.Elegiveis) > 1 {
		if _, err := fmt.Fprintf(w, "elegíveis: %s\n", strings.Join(p.Elegiveis, ", ")); err != nil {
			return err
		}
	}
	for _, b := range p.Bloqueadas {
		if _, err := fmt.Fprintf(w, "bloqueada: %s %s\n", b.US, b.Motivo); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(w, "limite (%s): %s\n", p.AutorizacaoAte, p.Limite)
	return err
}

func textoPendencia(w io.Writer, p central.Pendencia, recuo string) error {
	estado := "aberta"
	if !p.Aberta {
		estado = "resolvida"
		if p.Resposta != nil {
			estado += ": " + q(*p.Resposta)
		}
	}
	_, err := fmt.Fprintf(w, "%s%s  %s  %s  %s  (%s)\n", recuo, p.ID, p.Tipo, q(p.Texto), estado, p.US)
	return err
}

func textoEvento(w io.Writer, e central.Evento) error {
	_, err := fmt.Fprintf(w, "%s  %s  %s  %s  %s\n", e.TS, ou(e.Coordenador), e.Tipo, ou(e.US), q(string(e.Detalhe)))
	return err
}

func iacTexto(iac bool) string {
	if iac {
		return " [IaC]"
	}
	return ""
}

func ou(s *string) string {
	if s == nil {
		return "-"
	}
	return q(*s)
}
