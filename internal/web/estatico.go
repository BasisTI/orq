package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

const (
	// Estático com hash na URL (?v=<hash>): o hash é o do conteúdo embutido, então a URL muda
	// quando o arquivo muda e a resposta pode ficar um ano. Nunca "public": a web fica atrás de
	// basic_auth, e public autoriza cache compartilhado a guardar resposta a pedido com
	// Authorization (RFC 9111 §3.5).
	cacheComHash = "private, max-age=31536000, immutable"
	// Fontes e imagens ficam sem hash dentro do app.css (url(/estatico/fontes/…)): uma revalidação
	// por dia em troca de não reescrever as url() do CSS.
	cacheSemHash = "private, max-age=86400"
)

// comHash são os arquivos que o base referencia com ?v=<hash>.
var comHash = []string{"app.css", "htmx.min.js", "app.js"}

var tiposEstaticos = map[string]string{
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".woff2": "font/woff2",
	".png":   "image/png",
	".txt":   "text/plain; charset=utf-8",
}

// estaticos é o conteúdo de estatico/ e o hash dos arquivos com ?v=.
type estaticos struct {
	fs     fs.FS
	hashes map[string]string
}

func carregarEstaticos() *estaticos {
	sub, err := fs.Sub(estaticoFS, "estatico")
	if err != nil {
		panic(err)
	}
	e := &estaticos{fs: sub, hashes: map[string]string{}}
	for _, nome := range comHash {
		b, err := fs.ReadFile(sub, nome)
		if err != nil {
			panic("estatico/" + nome + " não está embutido: rode scripts/build-frontend.sh (" + err.Error() + ")")
		}
		e.hashes[nome] = hashDe(b)
	}
	return e
}

// hashDe é o ?v= de um arquivo: o começo do SHA-256 do conteúdo.
func hashDe(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])[:12]
}

// versao devolve o hash do arquivo, para o ?v= do base.
func (e *estaticos) versao(nome string) string { return e.hashes[nome] }

// servirEstatico atende /estatico/{caminho...}. Serve só arquivo regular do embed: diretório,
// caminho com "." ou ".." e o que não existe dão 404; nada sai do embed.
func (s *Servidor) servirEstatico(w http.ResponseWriter, r *http.Request) {
	nome := r.PathValue("caminho")
	if nome == "" || strings.HasSuffix(nome, "/") || path.Clean(nome) != nome || !fs.ValidPath(nome) {
		http.NotFound(w, r)
		return
	}
	b, err := fs.ReadFile(s.estatico.fs, nome)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	tipo, ok := tiposEstaticos[path.Ext(nome)]
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", tipo)
	h.Set("Cache-Control", cacheSemHash)
	if v, ok := s.estatico.hashes[nome]; ok && r.URL.Query().Get("v") == v {
		h.Set("Cache-Control", cacheComHash)
	}
	http.ServeContent(w, r, nome, time.Time{}, bytes.NewReader(b))
}
