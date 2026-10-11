package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestDetectMode(t *testing.T) {
	cases := []struct {
		flag string
		tty  bool
		want Mode
	}{{"", true, Text}, {"", false, JSON}, {"json", true, JSON}, {"text", false, Text}}
	for _, c := range cases {
		got, err := DetectMode(c.flag, c.tty)
		if err != nil || got != c.want {
			t.Fatalf("DetectMode(%q,%v) = %v,%v", c.flag, c.tty, got, err)
		}
	}
	if _, err := DetectMode("yaml", true); err == nil {
		t.Fatal("yaml devia falhar")
	}
}

func TestWriteErrorJSON(t *testing.T) {
	var b bytes.Buffer
	_ = WriteError(&b, JSON, &Error{Code: "ref_ambigua", Cause: "c", Recovery: "r", Exit: ExitUsage})
	var got map[string]map[string]string
	if err := json.Unmarshal(b.Bytes(), &got); err != nil || got["error"]["code"] != "ref_ambigua" || got["error"]["recovery"] != "r" {
		t.Fatalf("envelope = %s", b.String())
	}
}

func TestWriteErrorTextEscapaControles(t *testing.T) {
	var b bytes.Buffer
	_ = WriteError(&b, Text, &Error{Code: "x", Cause: "a\nb\x1b[31m", Recovery: "ok"})
	if s := b.String(); strings.Count(s, "\n") != 2 || strings.Contains(s, "\x1b") {
		t.Fatalf("texto = %q", s)
	}
}

func TestAsError(t *testing.T) {
	if e := AsError(errors.New("boom")); e.Code != "inesperado" || e.Exit != ExitUnexpected {
		t.Fatalf("got %+v", e)
	}
}
