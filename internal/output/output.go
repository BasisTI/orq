// Package output renderiza resultados e erros em texto ou JSON e define os códigos de saída.
package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
)

type Mode int

const (
	Text Mode = iota
	JSON
)

const (
	ExitOK         = 0
	ExitUnexpected = 1
	ExitUsage      = 2
	ExitConflict   = 4
	ExitNotFound   = 5
)

// DetectMode escolhe o formato: a flag explícita vence; sem ela, texto num terminal e JSON fora dele.
func DetectMode(flag string, isTTY bool) (Mode, error) {
	switch flag {
	case "json":
		return JSON, nil
	case "text":
		return Text, nil
	case "":
		if isTTY {
			return Text, nil
		}
		return JSON, nil
	}
	return Text, &Error{Code: "uso", Cause: fmt.Sprintf("--output %q inválido", flag), Recovery: "use --output json ou --output text", Exit: ExitUsage}
}

// Error é o envelope estável de erro.
type Error struct {
	Code     string `json:"code"`
	Cause    string `json:"cause,omitempty"`
	Recovery string `json:"recovery,omitempty"`
	Exit     int    `json:"-"`
}

func (e *Error) Error() string {
	if e.Cause == "" {
		return e.Code
	}
	return e.Code + ": " + e.Cause
}

// AsError devolve err como *Error; erro desconhecido vira "inesperado".
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: "inesperado", Cause: err.Error(), Exit: ExitUnexpected}
}

// WriteError renderiza o envelope de erro.
func WriteError(w io.Writer, m Mode, e *Error) error {
	if m == JSON {
		return WriteJSON(w, map[string]*Error{"error": e})
	}
	s := fmt.Sprintf("erro [%s]\n", Quote(e.Code))
	if e.Cause != "" {
		s = fmt.Sprintf("erro [%s]: %s\n", Quote(e.Code), Quote(e.Cause))
	}
	if e.Recovery != "" {
		s += fmt.Sprintf("  como resolver: %s\n", Quote(e.Recovery))
	}
	_, err := io.WriteString(w, s)
	return err
}

// UnsafeRune é uma runa que quebra ou disfarça uma linha de texto: controles, separadores de
// linha e de parágrafo e caracteres de formatação como os de bidi.
func UnsafeRune(r rune) bool {
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
}

// Quote devolve s como está, ou entre aspas de Go quando tem runa insegura, para que o texto
// gravado por outro agente continue numa linha só e mostre o que carrega.
func Quote(s string) string {
	if strings.ContainsFunc(s, UnsafeRune) {
		return strconv.Quote(s)
	}
	return s
}

// WriteJSON escreve v como JSON indentado, sem escapar HTML.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

type Field struct{ Key, Value string }

// WriteFields imprime linhas "chave: valor" alinhadas, para o modo texto.
func WriteFields(w io.Writer, fields []Field) error {
	width := 0
	for _, f := range fields {
		if len(f.Key) > width {
			width = len(f.Key)
		}
	}
	for _, f := range fields {
		if _, err := fmt.Fprintf(w, "%-*s  %s\n", width+1, Quote(f.Key)+":", Quote(f.Value)); err != nil {
			return err
		}
	}
	return nil
}
