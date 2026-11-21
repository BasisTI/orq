package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/output"
)

func rodarApp(env map[string]string, in string, outTTY bool, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	a := &App{
		In:     strings.NewReader(in),
		Out:    &out,
		Err:    &errOut,
		Env:    func(k string) string { return env[k] },
		OutTTY: outTTY,
	}
	code := a.Run(args)
	return code, out.String(), errOut.String()
}

func TestCLIMsgVerResponderList(t *testing.T) {
	env, _ := pendEnv(t)
	ctx := context.Background()
	st, err := central.Open(ctx, env["ORQ_STATE_DIR"])
	if err != nil {
		t.Fatal(err)
	}
	ondas, err := st.ListarOndas(ctx, central.FiltroOndas{Abertas: true})
	if err != nil || len(ondas) == 0 {
		t.Fatalf("ondas = %+v, %v", ondas, err)
	}
	ondaID := ondas[0].ID
	m1, err := st.EnviarMensagem(ctx, ondaID, "Primeira pergunta\nsegunda linha", "cedric")
	if err != nil {
		t.Fatal(err)
	}
	m2, err := st.EnviarMensagem(ctx, ondaID, "Segunda pergunta", "cedric")
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()

	// orq msg list (JSON) -> ordem decrescente (m2 antes de m1).
	code, out, errOut := rodarApp(env, "", false, "msg", "list")
	if code != 0 {
		t.Fatalf("msg list exit %d: %s", code, errOut)
	}
	var lista []central.Mensagem
	if err := json.Unmarshal([]byte(out), &lista); err != nil {
		t.Fatalf("json list: %v\n%s", err, out)
	}
	if len(lista) != 2 || lista[0].ID != m2.ID || lista[1].ID != m1.ID {
		t.Fatalf("lista = %+v", lista)
	}

	// orq msg ver <id> (texto)
	code, out, errOut = rodarApp(env, "", true, "msg", "ver", strconv.FormatInt(m1.ID, 10))
	if code != 0 {
		t.Fatalf("msg ver exit %d: %s", code, errOut)
	}
	for _, quer := range []string{"Primeira pergunta", "segunda linha", "cedric", "aguardando envio"} {
		if !strings.Contains(out, quer) {
			t.Errorf("msg ver não contém %q:\n%s", quer, out)
		}
	}

	// orq msg responder <id> "texto"
	code, out, errOut = rodarApp(env, "", false, "msg", "responder", strconv.FormatInt(m1.ID, 10), "Resposta direta")
	if code != 0 {
		t.Fatalf("msg responder exit %d: %s", code, errOut)
	}
	var resp central.Mensagem
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("json responder: %v\n%s", err, out)
	}
	if resp.Resposta == nil || *resp.Resposta != "Resposta direta" || resp.RespondidaPor == nil || *resp.RespondidaPor != "coord" {
		t.Fatalf("resp = %+v", resp)
	}

	// Responder de novo falha com conflito mensagem_respondida (exit 4).
	code, _, errOut = rodarApp(env, "", false, "msg", "responder", strconv.FormatInt(m1.ID, 10), "outra")
	if code != output.ExitConflict || !strings.Contains(errOut, "mensagem_respondida") {
		t.Fatalf("segunda resposta exit %d, err %s", code, errOut)
	}

	// orq msg list --abertas traz só m2.
	code, out, errOut = rodarApp(env, "", false, "msg", "list", "--onda", strconv.FormatInt(ondaID, 10), "--abertas")
	if code != 0 {
		t.Fatalf("msg list --abertas exit %d: %s", code, errOut)
	}
	lista = nil
	if err := json.Unmarshal([]byte(out), &lista); err != nil {
		t.Fatal(err)
	}
	if len(lista) != 1 || lista[0].ID != m2.ID {
		t.Fatalf("abertas = %+v", lista)
	}

	// orq msg responder com --arquivo - (stdin).
	code, out, errOut = rodarApp(env, "## Resposta\n\nVia stdin", true, "msg", "responder", strconv.FormatInt(m2.ID, 10), "--arquivo", "-")
	if code != 0 {
		t.Fatalf("msg responder stdin exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "respondida") {
		t.Errorf("saída texto = %q", out)
	}

	// Conferir orq msg ver com resposta.
	code, out, errOut = rodarApp(env, "", true, "msg", "ver", strconv.FormatInt(m2.ID, 10))
	if code != 0 {
		t.Fatalf("msg ver respondida exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "## Resposta") || !strings.Contains(out, "Via stdin") || !strings.Contains(out, "respondida") {
		t.Errorf("msg ver respondida:\n%s", out)
	}

	// Testar --arquivo com arquivo em disco.
	st, err = central.Open(ctx, env["ORQ_STATE_DIR"])
	if err != nil {
		t.Fatal(err)
	}
	m3, err := st.EnviarMensagem(ctx, ondaID, "Terceira", "cedric")
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	arq := filepath.Join(t.TempDir(), "resp.md")
	if err := os.WriteFile(arq, []byte("Resposta de arquivo"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = rodarApp(env, "", false, "msg", "responder", strconv.FormatInt(m3.ID, 10), "--arquivo", arq)
	if code != 0 || !strings.Contains(out, "Resposta de arquivo") {
		t.Fatalf("msg responder --arquivo exit %d, out=%s, err=%s", code, out, errOut)
	}
}

func TestCLIMsgResponderLimitesTruncamentoECR(t *testing.T) {
	env, _ := pendEnv(t)
	ctx := context.Background()
	st, err := central.Open(ctx, env["ORQ_STATE_DIR"])
	if err != nil {
		t.Fatal(err)
	}
	ondas, err := st.ListarOndas(ctx, central.FiltroOndas{Abertas: true})
	if err != nil || len(ondas) == 0 {
		t.Fatalf("ondas = %+v, %v", ondas, err)
	}
	m, err := st.EnviarMensagem(ctx, ondas[0].ID, "Pergunta longa", "cedric")
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	idStr := strconv.FormatInt(m.ID, 10)

	// Caso 1: stdin com quebra inicial ultrapassando 64 KiB (65553 bytes).
	stdinGrande := "\n" + strings.Repeat("A", 65535) + "FIM_PERDIDO_12345"
	code, _, errOut := rodarApp(env, stdinGrande, false, "msg", "responder", idStr, "--arquivo", "-")
	if code != output.ExitUsage || !strings.Contains(errOut, "65536") {
		t.Fatalf("caso 1 (stdin): exit=%d err=%s", code, errOut)
	}

	// Caso 2: arquivo em disco com CRLF ultrapassando 64 KiB (65552 bytes).
	arqCRLF := filepath.Join(t.TempDir(), "resp_crlf.md")
	if err := os.WriteFile(arqCRLF, []byte("linha1\r\n"+strings.Repeat("B", 65528)+"PERDIDO_NO_FINAL"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut = rodarApp(env, "", false, "msg", "responder", idStr, "--arquivo", arqCRLF)
	if code != output.ExitUsage || !strings.Contains(errOut, "65536") {
		t.Fatalf("caso 2 (arquivo CRLF): exit=%d err=%s", code, errOut)
	}

	// Caso 3: UTF-8 multibyte cortado no byte 65537 (65543 bytes) — deve dar erro de tamanho, não UTF-8 inválido.
	utf8Cortado := "\n" + strings.Repeat("a", 65535) + "é" + "resto"
	code, _, errOut = rodarApp(env, utf8Cortado, false, "msg", "responder", idStr, "--arquivo", "-")
	if code != output.ExitUsage || !strings.Contains(errOut, "65536") || strings.Contains(errOut, "UTF-8") {
		t.Fatalf("caso 3 (UTF-8 cortado): exit=%d err=%s", code, errOut)
	}

	// Achado 3 (CLI): \r solto recusado com exit 2.
	code, _, errOut = rodarApp(env, "", false, "msg", "responder", idStr, "resp\rcom_cr")
	if code != output.ExitUsage || !strings.Contains(errOut, "controle") {
		t.Fatalf("CR solto na CLI: exit=%d err=%s", code, errOut)
	}

	// Mensagem continua sem resposta e pode ser respondida normalmente (com CRLF normalizado).
	st, err = central.Open(ctx, env["ORQ_STATE_DIR"])
	if err != nil {
		t.Fatal(err)
	}
	mv, err := st.VerMensagem(ctx, m.ID)
	_ = st.Close()
	if err != nil || mv.RespondidaEm != nil {
		t.Fatalf("mensagem foi respondida indevidamente: %+v, %v", mv, err)
	}

	code, out, errOut := rodarApp(env, "", false, "msg", "responder", idStr, "linha1\r\nlinha2")
	if code != 0 {
		t.Fatalf("resposta válida depois das recusas: exit=%d err=%s", code, errOut)
	}
	var resp central.Mensagem
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Resposta == nil || *resp.Resposta != "linha1\nlinha2" {
		t.Fatalf("resposta normalizada = %+v", resp.Resposta)
	}
}
