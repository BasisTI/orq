package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/output"
)

// enviarMensagem atende POST /onda/{id}/mensagens: grava uma mensagem ao coordenador da onda.
// Sem HX-Request redireciona (POST/Redirect/GET 303) para /#onda-<id>; com HX-Request devolve a
// lista atualizada #onda-<id>-msgs e atualiza a contagem (N) e #aviso via hx-swap-oob.
func (s *Servidor) enviarMensagem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != r.PathValue("id") {
		s.erro(w, r, http.StatusNotFound, "Página não encontrada", "Endereço de onda inválido.", "/")
		return
	}
	voltar := fmt.Sprintf("/#onda-%d", id)
	if ct, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";"); strings.TrimSpace(ct) != "application/x-www-form-urlencoded" {
		s.erro(w, r, http.StatusUnsupportedMediaType, "Formato não aceito", "Envie o formulário da página.", voltar)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, LimiteCorpo)
	if err := r.ParseForm(); err != nil {
		var grande *http.MaxBytesError
		if errors.As(err, &grande) {
			s.erro(w, r, http.StatusRequestEntityTooLarge, "Mensagem grande demais", fmt.Sprintf("O formulário passa de %d KiB.", LimiteCorpo>>10), voltar)
			return
		}
		s.erro(w, r, http.StatusBadRequest, "Formulário inválido", "Não consegui ler o formulário.", voltar)
		return
	}
	texto := strings.ReplaceAll(r.PostForm.Get("texto"), "\r\n", "\n")
	if len(texto) > LimiteTexto {
		s.erro(w, r, http.StatusRequestEntityTooLarge, "Mensagem grande demais", fmt.Sprintf("O texto passa de %d KiB.", LimiteTexto>>10), voltar)
		return
	}
	if _, err := s.St.EnviarMensagem(r.Context(), id, texto, usuarioDe(r)); err != nil {
		s.falhaMensagem(w, r, err, voltar)
		return
	}
	if !ehFragmento(r) {
		http.Redirect(w, r, voltar, http.StatusSeeOther)
		return
	}
	ms, err := s.St.ListarMensagens(r.Context(), central.FiltroMensagens{OndaID: id})
	if err != nil {
		s.falhaMensagem(w, r, err, voltar)
		return
	}
	ov := OndaVista{
		ID:      id,
		Msgs:    vistaMsgs(ms),
		MsgsQtd: len(ms),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.modelos.ExecuteTemplate(w, "msg-enviada", struct {
		Onda  OndaVista
		Aviso string
	}{Onda: ov, Aviso: "Mensagem enviada ao coordenador."}); err != nil && s.Log != nil {
		s.Log("web: modelo msg-enviada: %v", err)
	}
}

// falhaMensagem traduz o erro da Central para o fluxo de envio de mensagem ao coordenador,
// mantendo os textos das pendências em s.falha inalterados.
func (s *Servidor) falhaMensagem(w http.ResponseWriter, r *http.Request, err error, voltar string) {
	e := output.AsError(err)
	switch {
	case e.Exit == output.ExitNotFound:
		s.erro(w, r, http.StatusNotFound, "Página não encontrada", e.Cause, "/")
	case e.Code == "onda_encerrada":
		s.erro(w, r, http.StatusConflict, "Onda encerrada", e.Cause, voltar)
	case e.Exit == output.ExitUsage:
		s.erro(w, r, http.StatusBadRequest, "Mensagem recusada", e.Cause, voltar)
	case e.Code == "banco_somente_leitura" || e.Code == "versao_do_banco" || e.Code == "banco_ocupado":
		s.erro(w, r, http.StatusServiceUnavailable, "Indisponível", "A Central está indisponível agora; tente de novo em instantes.", voltar)
	default:
		if s.Log != nil {
			s.Log("web: erro interno: %s", e.Code)
		}
		s.erro(w, r, http.StatusInternalServerError, "Erro interno", "Algo falhou do nosso lado. A mensagem não foi enviada.", voltar)
	}
}
