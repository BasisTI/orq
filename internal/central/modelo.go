package central

import "strings"

// FamiliaModelo é a marca de um modelo, derivada do prefixo do id (US #81).
type FamiliaModelo struct {
	Chave string // "claude", "gpt"…; vazia quando o prefixo é desconhecido
	Nome  string // "Claude", "GPT"…
	Letra string // letra do selo no terminal e na web sem ícone
}

// familias na ordem em que o prefixo é testado.
var familias = []struct {
	prefixo string
	FamiliaModelo
}{
	{"claude-", FamiliaModelo{"claude", "Claude", "C"}},
	{"gpt-", FamiliaModelo{"gpt", "GPT", "O"}}, // O de OpenAI: o G ficaria ambíguo com GLM e Gemini
	{"deepseek-", FamiliaModelo{"deepseek", "DeepSeek", "D"}},
	{"glm-", FamiliaModelo{"glm", "GLM", "Z"}}, // Z de Z.ai
	{"kimi-", FamiliaModelo{"kimi", "Kimi", "K"}},
	{"gemini-", FamiliaModelo{"gemini", "Gemini", "G"}},
}

// FamiliaDe devolve a família do id do modelo, sem diferenciar maiúsculas. ok é falso para
// prefixo desconhecido: a tela mostra só o texto, sem selo nem ícone.
func FamiliaDe(id string) (f FamiliaModelo, ok bool) {
	l := strings.ToLower(id)
	for _, c := range familias {
		if strings.HasPrefix(l, c.prefixo) {
			return c.FamiliaModelo, true
		}
	}
	return FamiliaModelo{}, false
}

// ModeloCurto é o id sem o prefixo `claude-` (`claude-fable-5-2` vira `fable-5-2`): o nome da
// marca já está no selo. Os outros ids ficam inteiros (`gpt-5.5`, `glm-5`), onde o prefixo é
// parte do nome que a pessoa conhece.
func ModeloCurto(id string) string {
	if len(id) > len("claude-") && strings.EqualFold(id[:len("claude-")], "claude-") {
		return id[len("claude-"):]
	}
	return id
}

// RotuloModelo é o texto `fable-5-2 medium` (modelo curto e esforço, se houver), sem selo.
func RotuloModelo(modelo string, esforco *string) string {
	s := ModeloCurto(modelo)
	if esforco != nil && *esforco != "" {
		s += " " + *esforco
	}
	return s
}
