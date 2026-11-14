package central

import "testing"

func TestFamiliaDe(t *testing.T) {
	casos := []struct {
		id, chave, letra string
		ok               bool
	}{
		{"claude-fable-5-2", "claude", "C", true},
		{"Claude-Opus-5-5", "claude", "C", true},
		{"gpt-5.5-codex", "gpt", "O", true},
		{"deepseek-v4-pro", "deepseek", "D", true},
		{"glm-5", "glm", "Z", true},
		{"kimi-k3", "kimi", "K", true},
		{"gemini-3-pro", "gemini", "G", true},
		{"llama-4", "", "", false},
		{"claude", "", "", false},
		{"", "", "", false},
	}
	for _, c := range casos {
		f, ok := FamiliaDe(c.id)
		if ok != c.ok || f.Chave != c.chave || f.Letra != c.letra {
			t.Errorf("FamiliaDe(%q) = %+v, %v", c.id, f, ok)
		}
	}
}

func TestRotuloModelo(t *testing.T) {
	med := "medium"
	vazio := ""
	for _, c := range []struct {
		id      string
		esforco *string
		quer    string
	}{
		{"claude-fable-5-2", &med, "fable-5-2 medium"},
		{"claude-fable-5-2", nil, "fable-5-2"},
		{"claude-fable-5-2", &vazio, "fable-5-2"},
		{"gpt-5.5", &med, "gpt-5.5 medium"},
		{"llama-4", nil, "llama-4"},
		{"claude-", nil, "claude-"},
	} {
		if got := RotuloModelo(c.id, c.esforco); got != c.quer {
			t.Errorf("RotuloModelo(%q) = %q, quer %q", c.id, got, c.quer)
		}
	}
}
