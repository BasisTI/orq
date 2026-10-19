package web

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/output"
	"github.com/BasisTI/orq/internal/refs"
)

//go:embed tmpl/*.html estilo.css
var assets embed.FS

// Limites do POST: o corpo todo e o texto da resposta.
const (
	LimiteCorpo     = 16 << 10
	LimiteTexto     = central.LimiteResposta
	itensPorPagina  = 20
	cabecalhoPadrao = "X-Orq-User"
	csp             = "default-src 'none'; style-src 'self'; form-action 'self'; frame-ancestors 'none'"
)

// Servidor atende as páginas. Não serve arquivo do disco e não expõe o snapshot: só desenha o
// que a Central guarda das pendências.
type Servidor struct {
	St *central.Store
	// Unix: a requisição veio do socket Unix, atrás do Caddy; o usuário vem do cabeçalho
	// CabecalhoUsuario, e sem ele a requisição é recusada. Em TCP (loopback) o cabeçalho é
	// ignorado e quem responde é "local".
	Unix             bool
	CabecalhoUsuario string
	// Porta é a porta TCP, para conferir o cabeçalho Host (rebinding de DNS); zero no Unix.
	Porta int
	// Log recebe uma linha por requisição, só com método, rota e status.
	Log   func(format string, args ...any)
	Agora func() time.Time

	modelos *template.Template
	css     []byte
}

func (s *Servidor) agora() time.Time {
	if s.Agora != nil {
		return s.Agora()
	}
	return time.Now()
}

func (s *Servidor) cabecalho() string {
	if s.CabecalhoUsuario != "" {
		return s.CabecalhoUsuario
	}
	return cabecalhoPadrao
}

// Handler monta as rotas e o que vale para todas: CSP, proteção contra POST de outra origem,
// identificação do usuário e o log.
func (s *Servidor) Handler() http.Handler {
	if s.modelos == nil {
		s.modelos = template.Must(template.New("").Funcs(template.FuncMap{}).ParseFS(assets, "tmpl/*.html"))
		s.css, _ = assets.ReadFile("estilo.css")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.indice)
	mux.HandleFunc("GET /p/{projeto}/{ref}/{n}", s.detalhe)
	mux.HandleFunc("POST /p/{projeto}/{ref}/{n}/responder", s.responder)
	mux.HandleFunc("GET /historico", s.historico)
	mux.HandleFunc("GET /saude", s.saude)
	mux.HandleFunc("GET /estilo.css", s.estilo)
	// O mux grava o padrão no Request que recebe; os de fora são cópias (WithContext), então a
	// rota é anotada aqui, depois do mux, para o log.
	comRota := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
		if rs, ok := r.Context().Value(chaveRota{}).(*string); ok {
			*rs = r.Pattern
		}
	})
	protegido := http.NewCrossOriginProtection().Handler(comRota)
	return s.comLog(s.comCabecalhos(s.comUsuario(protegido)))
}

type (
	chaveUsuario struct{}
	chaveRota    struct{}
)

type respostaStatus struct {
	http.ResponseWriter
	status int
}

func (r *respostaStatus) WriteHeader(c int) {
	if r.status == 0 {
		r.status = c
	}
	r.ResponseWriter.WriteHeader(c)
}

func (r *respostaStatus) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// comLog escreve só o método, a rota (o padrão, sem o caminho nem a query) e o status: nada do
// que a pessoa digitou nem do que o agente escreveu vai para o log.
func (s *Servidor) comLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs := &respostaStatus{ResponseWriter: w}
		var rota string
		next.ServeHTTP(rs, r.WithContext(context.WithValue(r.Context(), chaveRota{}, &rota)))
		if s.Log != nil {
			if rota == "" {
				rota = "-"
			}
			s.Log("web %s %d", rota, rs.status)
		}
	})
}

func (s *Servidor) comCabecalhos(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// comUsuario decide quem fala. No socket Unix, o Caddy já autenticou e põe o usuário no
// cabeçalho; sem ele, ninguém passou pela autenticação e a requisição é recusada (só /saude
// responde). Em TCP o cabeçalho é ignorado, porque qualquer processo local o forjaria.
func (s *Servidor) comUsuario(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		usuario := central.PorLocal
		if s.Unix {
			usuario = strings.TrimSpace(r.Header.Get(s.cabecalho()))
			if r.URL.Path == "/saude" {
				next.ServeHTTP(w, r)
				return
			}
			if usuario == "" || len(usuario) > 100 || strings.ContainsFunc(usuario, output.UnsafeRune) {
				http.Error(w, "sem usuário autenticado: o proxy precisa enviar o cabeçalho "+s.cabecalho(), http.StatusUnauthorized)
				return
			}
		} else if !s.hostPermitido(r.Host) {
			http.Error(w, "host não permitido", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), chaveUsuario{}, usuario)))
	})
}

func (s *Servidor) hostPermitido(host string) bool {
	if s.Porta == 0 {
		return true
	}
	h, p, err := net.SplitHostPort(host)
	if err != nil || p != strconv.Itoa(s.Porta) {
		return false
	}
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

func usuarioDe(r *http.Request) string {
	u, _ := r.Context().Value(chaveUsuario{}).(string)
	return u
}

type base struct {
	Titulo     string
	Usuario    string
	Recarregar bool
}

func (s *Servidor) pagina(w http.ResponseWriter, status int, nome string, dados any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.modelos.ExecuteTemplate(w, nome, dados); err != nil && s.Log != nil {
		s.Log("web: modelo %s: %v", nome, err)
	}
}

func (s *Servidor) erro(w http.ResponseWriter, r *http.Request, status int, titulo, msg, voltar string) {
	s.pagina(w, status, "erro", struct {
		base
		Mensagem, Voltar string
	}{base{Titulo: titulo, Usuario: usuarioDe(r)}, msg, voltar})
}

// falha traduz o erro da Central no status HTTP; o que não é do usuário vira 500 sem detalhe.
func (s *Servidor) falha(w http.ResponseWriter, r *http.Request, err error, voltar string) {
	e := output.AsError(err)
	switch {
	case e.Exit == output.ExitNotFound:
		s.erro(w, r, http.StatusNotFound, "Não encontrada", e.Cause, "/")
	case e.Code == "pendencia_resolvida":
		s.erro(w, r, http.StatusConflict, "Já resolvida", e.Cause, voltar)
	case e.Exit == output.ExitUsage:
		s.erro(w, r, http.StatusBadRequest, "Resposta recusada", e.Cause, voltar)
	case e.Code == "banco_somente_leitura" || e.Code == "versao_do_banco" || e.Code == "banco_ocupado":
		s.erro(w, r, http.StatusServiceUnavailable, "Indisponível", "A Central está indisponível agora; tente de novo em instantes.", voltar)
	default:
		if s.Log != nil {
			s.Log("web: erro interno: %s", e.Code)
		}
		s.erro(w, r, http.StatusInternalServerError, "Erro interno", "Algo falhou do nosso lado. A resposta não foi registrada.", voltar)
	}
}

func (s *Servidor) estilo(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write(s.css)
}

// saude confere que o banco responde e ainda aceita escrita deste orq.
func (s *Servidor) saude(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err := s.St.ConferirEscrita(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprintln(w, "indisponível")
		return
	}
	_, _ = fmt.Fprintln(w, "ok")
}

type itemLista struct {
	P      central.Pendencia
	Href   string
	Quando string
}

func (s *Servidor) item(p central.Pendencia) itemLista {
	u := p.RefUS()
	return itemLista{P: p, Href: refs.RotaPendencia(u.Projeto, u.Ref, p.Numero), Quando: s.quando(p.AbertaEm)}
}

// quando formata um instante RFC 3339 no fuso do servidor.
func (s *Servidor) quando(t string) string {
	v, err := time.Parse(time.RFC3339, t)
	if err != nil {
		return t
	}
	return v.Local().Format("02/01 15:04")
}

type filtro struct{ Coord, Projeto, Onda string }

func (s *Servidor) indice(w http.ResponseWriter, r *http.Request) {
	f := filtro{Coord: r.FormValue("coord"), Projeto: r.FormValue("projeto"), Onda: r.FormValue("onda")}
	todas, err := s.St.ListarPendencias(r.Context(), central.FiltroPendencias{Abertas: true})
	if err != nil {
		s.falha(w, r, err, "/")
		return
	}
	coords, projetos, ondas := map[string]bool{}, map[string]bool{}, map[int64]bool{}
	var abertas, respondidas []itemLista
	for _, p := range todas {
		coords[p.Coordenador], projetos[p.RefUS().Projeto], ondas[p.OndaID] = true, true, true
		if f.Coord != "" && p.Coordenador != f.Coord || f.Projeto != "" && p.RefUS().Projeto != f.Projeto || f.Onda != "" && strconv.FormatInt(p.OndaID, 10) != f.Onda {
			continue
		}
		if p.Estado == central.EstadoRespondida {
			respondidas = append(respondidas, s.item(p))
		} else {
			abertas = append(abertas, s.item(p))
		}
	}
	od := make([]int64, 0, len(ondas))
	for o := range ondas {
		od = append(od, o)
	}
	sort.Slice(od, func(i, j int) bool { return od[i] < od[j] })
	s.pagina(w, http.StatusOK, "indice", struct {
		base
		Filtro               filtro
		Coords, Projetos     []string
		Ondas                []int64
		Abertas, Respondidas []itemLista
	}{base{Titulo: "Pendências", Usuario: usuarioDe(r), Recarregar: true}, f, chaves(coords), chaves(projetos), od, abertas, respondidas})
}

func chaves(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *Servidor) historico(w http.ResponseWriter, r *http.Request) {
	pagina, _ := strconv.Atoi(r.FormValue("pagina"))
	if pagina < 1 {
		pagina = 1
	}
	f := central.FiltroPendencias{Resolvidas: true}
	total, err := s.St.ContarPendencias(r.Context(), f)
	if err != nil {
		s.falha(w, r, err, "/")
		return
	}
	paginas := max((total+itensPorPagina-1)/itensPorPagina, 1)
	pagina = min(pagina, paginas)
	f.Limite, f.Deslocamento = itensPorPagina, (pagina-1)*itensPorPagina
	ps, err := s.St.ListarPendencias(r.Context(), f)
	if err != nil {
		s.falha(w, r, err, "/")
		return
	}
	itens := make([]itemLista, len(ps))
	for i, p := range ps {
		itens[i] = s.item(p)
		if p.ResolvidaEm != nil {
			itens[i].Quando = "resolvida " + s.quando(*p.ResolvidaEm)
		}
	}
	d := struct {
		base
		Itens             []itemLista
		Pagina, Paginas   int
		Anterior, Proxima string
	}{base: base{Titulo: "Histórico", Usuario: usuarioDe(r)}, Itens: itens, Pagina: pagina, Paginas: paginas}
	if pagina > 1 {
		d.Anterior = "/historico?pagina=" + strconv.Itoa(pagina-1)
	}
	if pagina < paginas {
		d.Proxima = "/historico?pagina=" + strconv.Itoa(pagina+1)
	}
	s.pagina(w, http.StatusOK, "historico", d)
}

// pendDe lê a referência do caminho.
func pendDe(r *http.Request) (refs.Pend, bool) {
	projeto, err := refs.ValidarProjeto(r.PathValue("projeto"))
	ref, e1 := strconv.Atoi(r.PathValue("ref"))
	n, e2 := strconv.Atoi(r.PathValue("n"))
	if err != nil || e1 != nil || e2 != nil || ref <= 0 || n <= 0 || projeto != r.PathValue("projeto") {
		return refs.Pend{}, false
	}
	return refs.Pend{US: refs.US{Projeto: projeto, Ref: ref}, Numero: n}, true
}

type linkMR struct{ URL, Rotulo, Estado string }

type respostaVista struct{ Quando, Por, Texto string }

func (s *Servidor) detalhe(w http.ResponseWriter, r *http.Request) {
	s.mostrar(w, r, http.StatusOK, "")
}

// mostrar desenha a página da pendência; msg é o erro do formulário que acabou de falhar.
func (s *Servidor) mostrar(w http.ResponseWriter, r *http.Request, status int, msg string) {
	ref, ok := pendDe(r)
	if !ok {
		s.erro(w, r, http.StatusNotFound, "Não encontrada", "Endereço de pendência inválido.", "/")
		return
	}
	ctx := r.Context()
	p, err := s.St.VerPendencia(ctx, nil, ref)
	if err != nil {
		s.falha(w, r, err, "/")
		return
	}
	hist, err := s.St.HistoricoRespostas(ctx, ref)
	if err != nil {
		s.falha(w, r, err, "/")
		return
	}
	d := struct {
		base
		P                           central.Pendencia
		Detalhe                     template.HTML
		Aberta, ResolvidaEm         string
		RespondidaEm, RespostaAtual string
		TaigaURL, TaigaTitulo       string
		MRs                         []linkMR
		Historico                   []respostaVista
		Acao                        string
		Registrada                  bool
		Erro                        string
		LimiteTexto                 int
	}{base: base{Titulo: p.ID + " " + p.Texto, Usuario: usuarioDe(r)}, P: p, Aberta: s.quando(p.AbertaEm), Acao: refs.RotaPendencia(ref.US.Projeto, ref.US.Ref, ref.Numero) + "/responder",
		Registrada: r.FormValue("registrada") == "1", Erro: msg, LimiteTexto: LimiteTexto}
	if p.Detalhe != nil {
		d.Detalhe = RenderizarMarkdown(*p.Detalhe)
	}
	if p.ResolvidaEm != nil {
		d.ResolvidaEm = s.quando(*p.ResolvidaEm)
	}
	if p.RespondidaEm != nil {
		d.RespondidaEm = s.quando(*p.RespondidaEm)
		d.RespostaAtual = textoDaResposta(p.RespostaOpcao, p.Opcoes, p.Resposta)
	}
	if u, err := s.St.LerUS(ctx, ref.US); err == nil {
		if u.Taiga.URL != nil {
			d.TaigaURL = *u.Taiga.URL
		}
		if u.Taiga.Titulo != nil {
			d.TaigaTitulo = *u.Taiga.Titulo
		}
		for _, m := range u.MRs {
			est := ""
			if m.Estado != nil {
				est = *m.Estado
			}
			d.MRs = append(d.MRs, linkMR{URL: m.URL, Rotulo: m.RefCurta(), Estado: est})
		}
	}
	for _, h := range hist {
		d.Historico = append(d.Historico, respostaVista{Quando: s.quando(h.Em), Por: h.Por, Texto: textoDoHistorico(h)})
	}
	s.pagina(w, status, "detalhe", d)
}

func textoDaResposta(opcao *int, opcoes []central.Opcao, texto *string) string {
	var partes []string
	if opcao != nil {
		t := fmt.Sprintf("opção %d", *opcao)
		for _, o := range opcoes {
			if o.ID == *opcao {
				t += " (" + o.Titulo + ")"
			}
		}
		partes = append(partes, t)
	}
	if texto != nil && *texto != "" {
		partes = append(partes, "“"+*texto+"”")
	}
	return strings.Join(partes, " · ")
}

func textoDoHistorico(h central.RespostaRegistrada) string {
	titulo := ""
	if h.TituloOpcao != nil {
		titulo = *h.TituloOpcao
	}
	var partes []string
	if h.Opcao != nil {
		t := fmt.Sprintf("opção %d", *h.Opcao)
		if titulo != "" {
			t += " (" + titulo + ")"
		}
		partes = append(partes, t)
	}
	if h.Texto != nil && *h.Texto != "" {
		partes = append(partes, "“"+*h.Texto+"”")
	}
	return strings.Join(partes, " · ")
}

// responder grava a resposta e redireciona para a página (post/redirect/get): recarregar o
// resultado não reenvia o formulário.
func (s *Servidor) responder(w http.ResponseWriter, r *http.Request) {
	ref, ok := pendDe(r)
	if !ok {
		s.erro(w, r, http.StatusNotFound, "Não encontrada", "Endereço de pendência inválido.", "/")
		return
	}
	voltar := refs.RotaPendencia(ref.US.Projeto, ref.US.Ref, ref.Numero)
	if ct, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";"); strings.TrimSpace(ct) != "application/x-www-form-urlencoded" {
		s.erro(w, r, http.StatusUnsupportedMediaType, "Formato não aceito", "Envie o formulário da página.", voltar)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, LimiteCorpo)
	if err := r.ParseForm(); err != nil {
		var grande *http.MaxBytesError
		if errors.As(err, &grande) {
			s.erro(w, r, http.StatusRequestEntityTooLarge, "Resposta grande demais", fmt.Sprintf("O formulário passa de %d KiB.", LimiteCorpo>>10), voltar)
			return
		}
		s.erro(w, r, http.StatusBadRequest, "Formulário inválido", "Não consegui ler o formulário.", voltar)
		return
	}
	resp := central.Resposta{Texto: r.PostForm.Get("texto"), Por: usuarioDe(r)}
	if len(resp.Texto) > LimiteTexto {
		s.erro(w, r, http.StatusRequestEntityTooLarge, "Resposta grande demais", fmt.Sprintf("O texto passa de %d KiB.", LimiteTexto>>10), voltar)
		return
	}
	if v := strings.TrimSpace(r.PostForm.Get("opcao")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			s.erro(w, r, http.StatusBadRequest, "Opção inválida", "Escolha uma das opções da página.", voltar)
			return
		}
		resp.Opcao = &n
	}
	if _, err := s.St.ResponderPendencia(r.Context(), nil, ref, resp); err != nil {
		s.falha(w, r, err, voltar)
		return
	}
	http.Redirect(w, r, voltar+"?registrada=1", http.StatusSeeOther)
}

// Servir atende l até ctx acabar e então encerra com prazo. Devolve o erro do servidor, se
// houve um antes do cancelamento.
func (s *Servidor) Servir(ctx context.Context, l net.Listener) error {
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	fim := make(chan error, 1)
	go func() { fim <- srv.Serve(l) }()
	select {
	case err := <-fim:
		return err
	case <-ctx.Done():
		parar, cancelar := contextoDe(ctx, 5*time.Second)
		defer cancelar()
		_ = srv.Shutdown(parar)
		if err := <-fim; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
