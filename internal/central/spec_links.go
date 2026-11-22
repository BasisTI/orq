package central

import (
	"net/url"
	"path"
	"strings"
)

// ResolverLinkSpec resolve o destino de um link do markdown de um arquivo da spec para um caminho
// relativo à raiz do repositório. base é a pasta do arquivo que traz o link. Só vale link
// relativo: o que tem esquema (http:, mailto:), começa por `//`, é só âncora ou vazio devolve
// false. O caminho é normalizado (`..` e `.` resolvidos, `%xx` decodificado, âncora e query
// cortadas) e o que sai da raiz do repositório também devolve false. A decisão de o caminho
// estar ou não na allowlist é de quem chama (a web e o daemon).
func ResolverLinkSpec(base, destino string) (string, bool) {
	d := strings.TrimSpace(destino)
	if d == "" || strings.HasPrefix(d, "#") || strings.HasPrefix(d, "//") || strings.HasPrefix(d, `\`) {
		return "", false
	}
	if i := strings.IndexAny(d, "?#"); i >= 0 {
		d = d[:i]
	}
	// Esquema (http:, mailto:, javascript:): o primeiro segmento tem ":".
	if strings.Contains(strings.SplitN(d, "/", 2)[0], ":") {
		return "", false
	}
	d, err := url.PathUnescape(d)
	if err != nil || d == "" || temControle(d) || strings.Contains(d, `\`) {
		return "", false
	}
	var p string
	if strings.HasPrefix(d, "/") {
		p = path.Clean(strings.TrimPrefix(d, "/"))
	} else {
		p = path.Clean(path.Join(base, d))
	}
	if p == "." || p == ".." || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") {
		return "", false
	}
	return p, true
}

// MdDiretoNaPasta diz se o caminho (relativo à raiz do repositório) é um `.md` diretamente na
// pasta dada, sem subpasta.
func MdDiretoNaPasta(caminho, pasta string) bool {
	return strings.HasSuffix(caminho, ".md") && path.Dir(caminho) == path.Clean(pasta)
}
