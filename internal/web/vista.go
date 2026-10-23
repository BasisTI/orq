package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// A tela sai do snapshot (spec §2.1): vistaCentral não abre banco, não chama ferramenta e não lê
// relógio. O que o modelo desenha está todo aqui, em texto puro; o escape é do html/template e
// nenhum campo é template.HTML.

// Filtro é o que a rota pede da Central. Projeto vazio é a aba Todos.
type Filtro struct {
	Projeto string
}

// Selo é uma pílula com texto sempre; Icone é o nome de um símbolo do sprite (vazio: sem ícone).
type Selo struct {
	Cls, Texto, Icone, Title string
}

// Aba é uma aba da faixa do topo. ID é o do <span> da contagem, que o /vivo troca por oob.
type Aba struct {
	Rotulo, Href, ID string
	N                int
	SemN             bool // sem contagem (o snapshot não pôde ser lido)
	Atual            bool
	Aviso            bool // contagem âmbar: há pendência esperando
	Tom              int
	TemTom           bool
	Fim              bool // à direita, a partir de 900 px
	Oob              bool
}

// Identificadores das abas (o que as páginas passam como "ativa").
const (
	abaTodos      = "todos"
	abaPendencias = "pendencias"
	abaProjeto    = "projeto:"
)

// ProjetoTag é o slug do projeto com o tom do ponto (4 tons, só ali).
type ProjetoTag struct {
	Slug string
	Tom  int
}

// AgenteVista é o estado do agente em português; o código do snapshot vai no title. Nome, pane e
// workspace do Herdr nunca chegam aqui.
type AgenteVista struct {
	Texto, Cls, Title string
	Vencido           bool
}

// MRVista é uma MR dentro da US.
type MRVista struct {
	URL, Rotulo, Caminho string
	Estado               Selo
	IaC                  bool
	Pipeline, Aprovacao  Selo
	Vencido              bool
	// ExtraFalhou é o código da falha da consulta extra (vazio: não falhou); ValoresLidos diz
	// há quanto tempo pipeline e aprovação foram lidos.
	ExtraFalhou, ValoresLidos string
	// Erro é o código da última consulta à forja, se falhou (o estado e os valores são os últimos bons).
	Erro string
}

// PendVista é o link de uma pendência dentro da US (a pendência no lugar é da story 3).
type PendVista struct {
	ID, Href, Tipo, Estado string
}

// USVista é uma linha de US.
type USVista struct {
	ID, Anchor, Titulo string
	Status             Selo
	Agente             AgenteVista
	TaigaStatus        string
	TaigaURL           string
	TaigaIdade         string
	TaigaNunca         bool
	TaigaVencido       bool
	TaigaErro          string
	Projeto            ProjetoTag
	Dependencias       []string
	Papel              string
	MRs                []MRVista
	ResumoMRs          []Selo
	SemMRs             bool
	MRsQtd             int
	Pendencias         []PendVista
	Esperam            int
	Oob                bool
}

// OndaVista é a barra e o corpo de uma onda.
type OndaVista struct {
	ID                       int64
	Anchor, BarraID          string
	Titulo                   string
	Coordenador              string
	Projetos                 []ProjetoTag
	Limite                   string // "até mr-pronta · estrita"
	Href                     string // diário
	Criada                   string
	Feitas, Total            int
	Esperam                  int
	Sinais                   []Selo
	US                       []USVista
	Atencao                  int
	Concluida                bool
	Oob                      bool
	falhando, prontas        int
	esperaMerge, paraRevisao int
}

// MaxProg é o máximo do <progress>: nunca zero, para a onda sem US não dar um elemento inválido.
func (o OndaVista) MaxProg() int { return max(o.Total, 1) }

// FonteVista é uma linha de "Idade dos dados".
type FonteVista struct {
	Nome, Idade, Erro string
	Vencido, Nunca    bool
}

// ItemSituacao é um fato da linha de situação.
type ItemSituacao struct {
	Cls, Icone, Texto, Href string
}

// SituacaoVista é a linha de situação: uma frase, os fatos e o aviso de fonte vencida.
type SituacaoVista struct {
	Ondas    int
	OndasRot string
	Itens    []ItemSituacao
	Vencidas string // "herdr (há 2 min), taiga (há 1 h)"; vazio: nenhuma
	Oob      bool
}

// Central é a tela inteira, pronta para o modelo.
type Central struct {
	Projeto   string // slug da aba; vazio: Todos
	H1        string
	Abas      []Aba
	Situacao  SituacaoVista
	Ondas     []OndaVista
	Fontes    []FonteVista
	FontesOob bool
	// Hash identifica a lista (ids e ordem das ondas, das US e das abas): o /vivo compara o que a
	// página tem com o do servidor e, se mudou, avisa em vez de mexer na lista.
	Hash string
	// Coordenadores são os nomes registrados (a tela vazia os cita: a ausência de ondas é global).
	Coordenadores []string
	// ProjetoSemOnda: a aba de projeto pedida não tem onda aberta (a rota responde 404).
	ProjetoSemOnda bool
}

func plural(n int, um, varios string) string {
	if n == 1 {
		return um
	}
	return varios
}

// vistaCentral monta a Central (aba Todos ou a de um projeto) do snapshot.
func vistaCentral(snap central.Snapshot, agora time.Time, f Filtro) Central {
	ativa := abaTodos
	if f.Projeto != "" {
		ativa = abaProjeto + f.Projeto
	}
	c := Central{Projeto: f.Projeto, Abas: abasDe(snap, ativa)}
	c.H1 = "Ondas abertas"
	if f.Projeto != "" {
		c.H1 = "Ondas com US de " + f.Projeto
	}

	tons := tonsDe(snap)
	esperam := esperamPorOnda(snap)
	for _, co := range snap.Coordenadores {
		for _, o := range co.Ondas {
			if o.EncerradaEm != nil {
				continue
			}
			ov := vistaOnda(o, esperam[o.ID], tons, agora)
			if f.Projeto != "" && !temProjeto(ov, f.Projeto) {
				continue
			}
			c.Ondas = append(c.Ondas, ov)
		}
	}
	sort.SliceStable(c.Ondas, func(i, j int) bool {
		if c.Ondas[i].Atencao != c.Ondas[j].Atencao {
			return c.Ondas[i].Atencao > c.Ondas[j].Atencao
		}
		return c.Ondas[i].ID < c.Ondas[j].ID
	})
	c.ProjetoSemOnda = f.Projeto != "" && len(c.Ondas) == 0
	c.Situacao = vistaSituacao(c.Ondas, snap, agora)
	for _, nome := range central.Fontes {
		if fs, ok := snap.Fontes[nome]; ok {
			fr := frescorEm(agora, nome, fs.AtualizadoEm, fs.Erro)
			c.Fontes = append(c.Fontes, FonteVista{Nome: nome, Idade: fr.Idade, Vencido: fr.Vencido, Nunca: fr.Nunca, Erro: fr.Erro})
		}
	}
	c.Hash = hashLista(c.Ondas, c.Abas)
	for _, co := range snap.Coordenadores {
		c.Coordenadores = append(c.Coordenadores, co.Nome)
	}
	return c
}

func temProjeto(o OndaVista, slug string) bool {
	for _, p := range o.Projetos {
		if p.Slug == slug {
			return true
		}
	}
	return false
}

// hashLista é o começo do SHA-256 dos ids e da ordem das ondas, das US de cada uma e das abas.
// Ele muda quando a lista que a página mostra deixa de ser a do servidor.
func hashLista(ondas []OndaVista, abas []Aba) string {
	h := sha256.New()
	for _, o := range ondas {
		fmt.Fprintf(h, "o%d:", o.ID)
		for _, u := range o.US {
			fmt.Fprintf(h, "%s,", u.Anchor)
		}
		h.Write([]byte{'\n'})
	}
	for _, a := range abas {
		fmt.Fprintf(h, "a%s\n", a.ID)
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// tonsDe é o índice de tom de cada projeto; os 4 tons do design se repetem a partir do quinto.
func tonsDe(snap central.Snapshot) map[string]int {
	m := make(map[string]int, len(snap.Projetos))
	for _, p := range snap.Projetos {
		m[p.Projeto] = p.Indice % 4
	}
	return m
}

// esperamPorOnda conta as pendências sem resposta de cada onda. A respondida sai da contagem
// (mas segura o "concluída": isso é do EncerramentoDe).
func esperamPorOnda(snap central.Snapshot) map[int64]int {
	m := map[int64]int{}
	for _, p := range snap.PendenciasAbertas {
		if p.Estado == central.EstadoAberta {
			m[p.OndaID]++
		}
	}
	return m
}

// abasDe monta a faixa de abas: Todos, uma por projeto com onda aberta e Pendências.
func abasDe(snap central.Snapshot, ativa string) []Aba {
	tons := tonsDe(snap)
	porProjeto := map[string]int{}
	total := 0
	for _, co := range snap.Coordenadores {
		for _, o := range co.Ondas {
			if o.EncerradaEm != nil {
				continue
			}
			total++
			vistos := map[string]bool{}
			for _, u := range o.US {
				if !vistos[u.Projeto] {
					vistos[u.Projeto] = true
					porProjeto[u.Projeto]++
				}
			}
		}
	}
	abas := []Aba{{Rotulo: "Todos", Href: "/central", ID: "aba-n-todos", N: total, Atual: ativa == abaTodos}}
	slugs := make([]string, 0, len(porProjeto))
	for s := range porProjeto {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	for _, s := range slugs {
		abas = append(abas, Aba{Rotulo: s, Href: "/projeto/" + s, ID: "aba-n-p-" + s, N: porProjeto[s], Atual: ativa == abaProjeto+s, Tom: tons[s], TemTom: true})
	}
	pend := 0
	for _, p := range snap.PendenciasAbertas {
		if p.Estado == central.EstadoAberta {
			pend++
		}
	}
	return append(abas, Aba{Rotulo: "Pendências", Href: "/", ID: "aba-n-pendencias", N: pend, Aviso: pend > 0, Atual: ativa == abaPendencias, Fim: true})
}

var (
	statusUS = map[string]Selo{
		"wait": {Cls: "neutro", Texto: "espera"},
		"plan": {Cls: "info", Texto: "plano"},
		"exec": {Cls: "info", Texto: "execução"},
		"rev":  {Cls: "info", Texto: "revisão"},
		"done": {Cls: "ok", Texto: "pronta"},
	}
	agentes = map[string]AgenteVista{
		"working": {Texto: "trabalhando", Cls: "info"},
		"idle":    {Texto: "ocioso", Cls: "neutro"},
		"done":    {Texto: "concluído", Cls: "ok"},
		"blocked": {Texto: "bloqueado", Cls: "aviso"},
		"unknown": {Texto: "sem agente", Cls: "apagado"},
		"ausente": {Texto: "pane ausente", Cls: "apagado"},
	}
	selosPipeline = map[string]Selo{
		"sucesso":  {Cls: "ok", Texto: "sucesso", Icone: "check"},
		"falhou":   {Cls: "erro", Texto: "falhou", Icone: "x"},
		"rodando":  {Cls: "info", Texto: "rodando", Icone: "rodando"},
		"pendente": {Cls: "aviso", Texto: "pendente", Icone: "relogio"},
		"sem":      {Cls: "neutro", Texto: "sem pipeline"},
	}
	selosAprovacao = map[string]Selo{
		"aprovada":    {Cls: "ok", Texto: "aprovada", Icone: "check"},
		"pendente":    {Cls: "aviso", Texto: "pendente", Icone: "relogio"},
		"nao_exigida": {Cls: "neutro", Texto: "não exigida"},
	}
	selosEstadoMR = map[string]Selo{
		"draft":    {Cls: "neutro", Texto: "draft"},
		"aberta":   {Cls: "info", Texto: "aberta"},
		"pronta":   {Cls: "ok", Texto: "pronta"},
		"mesclada": {Cls: "neutro", Texto: "mesclada"},
		"fechada":  {Cls: "apagado", Texto: "fechada"},
	}
	papeis = map[string]string{"planejamento": "planejamento", "implementacao": "implementação", "revisao": "revisão"}
)

// seloDe devolve o selo do valor, ou o selo tracejado "desconhecido" quando o snapshot não o deu
// ou esta versão não o conhece.
func seloDe(v *string, tabela map[string]Selo) Selo {
	if v != nil {
		if s, ok := tabela[*v]; ok {
			return s
		}
	}
	return Selo{Cls: "tracejado", Texto: "desconhecido", Icone: "duvida"}
}

// seloVencido é o selo de dado que passou do prazo da fonte.
var seloVencido = Selo{Cls: "aviso", Texto: "vencido", Icone: "relogio", Title: "o cache passou do prazo da fonte"}

// agenteDe traduz o estado do pane. Sem pane (Herdr nulo) é "sem agente"; pane conhecido e
// Herdr que nunca respondeu (status nulo) é "sem resposta".
func agenteDe(h *central.Herdr, agora time.Time) AgenteVista {
	if h == nil {
		return AgenteVista{Texto: "sem agente", Cls: "apagado", Title: "estado do agente: sem pane"}
	}
	if h.Status == nil {
		return AgenteVista{Texto: "sem resposta", Cls: "apagado", Title: "estado do agente: o Herdr não respondeu"}
	}
	a, ok := agentes[*h.Status]
	if !ok {
		a = AgenteVista{Texto: "sem resposta", Cls: "apagado"}
	}
	a.Title = "estado do agente: " + *h.Status
	a.Vencido = central.Vencido(central.FonteHerdr, h.AtualizadoEm, agora)
	return a
}

// frescorEm é a idade de um valor em cache, como o rodapé do TUI a escreve, e se ela passou do
// prazo da fonte.
func frescorEm(agora time.Time, fonte string, atualizadoEm *string, erro *central.Erro) Frescor {
	f := Frescor{}
	if atualizadoEm == nil {
		f.Nunca = true
	} else {
		f.Idade = central.Idade(*atualizadoEm, agora)
		f.Vencido = central.Vencido(fonte, atualizadoEm, agora)
		// "agora" ao lado de "vencido" se contradiz (o prazo do Herdr é de 15 s): dá os segundos.
		if t, err := time.Parse(time.RFC3339, *atualizadoEm); err == nil && f.Vencido && agora.Sub(t) < time.Minute {
			f.Idade = fmt.Sprintf("há %d s", int(agora.Sub(t)/time.Second))
		}
	}
	if erro != nil {
		f.Erro = erro.Codigo
	}
	return f
}

func vistaOnda(o central.Onda, esperam int, tons map[string]int, agora time.Time) OndaVista {
	id := strconv.FormatInt(o.ID, 10)
	ov := OndaVista{
		ID: o.ID, Anchor: "onda-" + id, BarraID: "onda-" + id + "-barra", Titulo: o.Titulo, Coordenador: o.Coordenador,
		Limite: "até " + o.AutorizacaoAte + " · " + o.Ordem, Href: "/onda/" + id, Total: len(o.US), Esperam: esperam,
	}
	if len(o.CriadaEm) >= 10 {
		ov.Criada = o.CriadaEm[:10]
	}
	if ov.Titulo == "" {
		ov.Titulo = "onda " + id
	}
	visto := map[string]bool{}
	nomes := map[string]string{}
	for _, u := range o.US {
		nomes[u.Chave] = u.ID
	}
	merge := o.AutorizacaoAte == "merge"
	for _, u := range o.US {
		if u.Status == "done" {
			ov.Feitas++
		}
		if !visto[u.Projeto] {
			visto[u.Projeto] = true
			ov.Projetos = append(ov.Projetos, ProjetoTag{Slug: u.Projeto, Tom: tons[u.Projeto]})
		}
		uv := vistaUS(u, merge, tons, nomes, agora)
		ov.US = append(ov.US, uv)
		for _, m := range u.MRs {
			if m.Estado != nil && (*m.Estado == "mesclada" || *m.Estado == "fechada") {
				continue
			}
			if m.Pipeline != nil && *m.Pipeline == "falhou" {
				ov.falhando++
			}
			if m.Estado != nil && *m.Estado == "pronta" {
				ov.prontas++
				if merge {
					ov.esperaMerge++
				} else {
					ov.paraRevisao++
				}
			}
		}
	}
	if enc := central.EncerramentoDe(o); enc != nil && enc.Pronta {
		ov.Concluida = true
	}
	switch {
	case esperam > 0:
		ov.Atencao = 3
	case ov.falhando > 0:
		ov.Atencao = 2
	case ov.prontas > 0:
		ov.Atencao = 1
	}
	if esperam > 0 {
		ov.Sinais = append(ov.Sinais, Selo{Cls: "aviso-forte", Texto: fmt.Sprintf("%d %s", esperam, plural(esperam, "espera você", "esperam você")), Icone: "user"})
	}
	if ov.falhando > 0 {
		ov.Sinais = append(ov.Sinais, Selo{Cls: "erro", Texto: fmt.Sprintf("%d %s", ov.falhando, plural(ov.falhando, "pipeline falhou", "pipelines falharam")), Icone: "x"})
	}
	if ov.esperaMerge > 0 {
		ov.Sinais = append(ov.Sinais, Selo{Cls: "info", Texto: fmt.Sprintf("%d %s", ov.esperaMerge, plural(ov.esperaMerge, "MR espera merge", "MRs esperam merge")), Icone: "merge"})
	}
	if ov.paraRevisao > 0 {
		ov.Sinais = append(ov.Sinais, Selo{Cls: "info", Texto: fmt.Sprintf("%d %s", ov.paraRevisao, plural(ov.paraRevisao, "MR pronta", "MRs prontas")), Icone: "merge", Title: "pronta para a revisão humana; esta onda não autoriza merge"})
	}
	if ov.Concluida {
		ov.Sinais = append(ov.Sinais, Selo{Cls: "ok", Texto: "concluída", Icone: "check"})
	}
	return ov
}

func vistaUS(u central.US, merge bool, tons map[string]int, nomes map[string]string, agora time.Time) USVista {
	uv := USVista{
		ID: u.ID, Anchor: "us-" + u.Projeto + "-" + strconv.Itoa(u.Ref), Projeto: ProjetoTag{Slug: u.Projeto, Tom: tons[u.Projeto]},
		Agente: agenteDe(u.Herdr, agora), SemMRs: len(u.MRs) == 0, MRsQtd: len(u.MRs),
	}
	if s, ok := statusUS[u.Status]; ok {
		uv.Status = s
	} else {
		uv.Status = Selo{Cls: "neutro", Texto: u.Status}
	}
	if u.Taiga.Titulo != nil {
		uv.Titulo = *u.Taiga.Titulo
	}
	if u.Taiga.Status != nil {
		uv.TaigaStatus = *u.Taiga.Status
	}
	if u.Taiga.URL != nil {
		uv.TaigaURL = *u.Taiga.URL
	}
	fr := frescorEm(agora, central.FonteTaiga, u.Taiga.AtualizadoEm, u.Taiga.Erro)
	uv.TaigaIdade, uv.TaigaNunca, uv.TaigaVencido, uv.TaigaErro = fr.Idade, fr.Nunca, fr.Vencido, fr.Erro
	for _, d := range u.DependeDe {
		if n, ok := nomes[d]; ok {
			d = n
		}
		uv.Dependencias = append(uv.Dependencias, d)
	}
	if p, ok := papeis[derefStr(u.Papel)]; ok {
		uv.Papel = p
		if u.Rodada != nil {
			uv.Papel += ", rodada " + strconv.Itoa(*u.Rodada)
		}
	}
	var falhando, prontas int
	desconhecido := false
	for _, m := range u.MRs {
		mv := vistaMR(m, merge, agora)
		uv.MRs = append(uv.MRs, mv)
		if m.Estado == nil || m.Pipeline == nil {
			desconhecido = true
		}
		if m.Estado != nil && (*m.Estado == "mesclada" || *m.Estado == "fechada") {
			continue
		}
		if m.Pipeline != nil && *m.Pipeline == "falhou" {
			falhando++
		}
		if m.Estado != nil && *m.Estado == "pronta" {
			prontas++
		}
	}
	if falhando > 0 {
		uv.ResumoMRs = append(uv.ResumoMRs, Selo{Cls: "erro", Texto: fmt.Sprintf("%d %s", falhando, plural(falhando, "pipeline falhou", "pipelines falharam")), Icone: "x"})
	}
	if prontas > 0 {
		t := plural(prontas, "MR pronta", "MRs prontas")
		if merge {
			t = plural(prontas, "MR espera merge", "MRs esperam merge")
		}
		uv.ResumoMRs = append(uv.ResumoMRs, Selo{Cls: "info", Texto: fmt.Sprintf("%d %s", prontas, t), Icone: "merge"})
	}
	if desconhecido {
		uv.ResumoMRs = append(uv.ResumoMRs, Selo{Cls: "tracejado", Texto: "desconhecido"})
	}
	for _, p := range u.Pendencias {
		switch p.Estado {
		case central.EstadoAberta:
			uv.Esperam++
		case central.EstadoRespondida:
		default:
			continue
		}
		r := p.RefUS()
		uv.Pendencias = append(uv.Pendencias, PendVista{ID: p.ID, Href: refs.RotaPendencia(r.Projeto, r.Ref, p.Numero), Tipo: p.Tipo, Estado: p.Estado})
	}
	return uv
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func vistaMR(m central.MR, merge bool, agora time.Time) MRVista {
	mv := MRVista{URL: m.URL, Rotulo: m.RefCurta(), Caminho: m.ProjetoPath, IaC: m.IaC, Pipeline: seloDe(m.Pipeline, selosPipeline), Aprovacao: seloDe(m.Aprovacao, selosAprovacao)}
	if m.Estado == nil {
		mv.Estado = Selo{Cls: "tracejado", Texto: "sem estado"}
	} else if s, ok := selosEstadoMR[*m.Estado]; ok {
		mv.Estado = s
		if *m.Estado == "pronta" && merge {
			mv.Estado = Selo{Cls: "info", Texto: "espera merge"}
		}
	} else {
		mv.Estado = Selo{Cls: "tracejado", Texto: "desconhecido"}
	}
	mv.Vencido = central.Vencido(central.FonteMR, m.AtualizadoEm, agora)
	if m.Erro != nil {
		mv.Erro = m.Erro.Codigo
	}
	// Pipeline e aprovação valem desde SituacaoEm; sem ele (e sem falha) valem desde AtualizadoEm.
	lidosEm := m.SituacaoEm
	if lidosEm == nil && m.SituacaoErro == nil {
		lidosEm = m.AtualizadoEm
	}
	if central.Vencido(central.FonteMR, lidosEm, agora) {
		mv.Vencido = true
	}
	if m.SituacaoErro != nil {
		mv.ExtraFalhou = m.SituacaoErro.Codigo
		if m.SituacaoEm == nil {
			mv.ValoresLidos = "nunca"
		} else {
			mv.ValoresLidos = central.Idade(*m.SituacaoEm, agora)
		}
	}
	return mv
}

func vistaSituacao(ondas []OndaVista, snap central.Snapshot, agora time.Time) SituacaoVista {
	var s SituacaoVista
	s.Ondas, s.OndasRot = len(ondas), plural(len(ondas), "onda aberta", "ondas abertas")
	var esperam, falhando, merge, revisao, concluidas int
	for _, o := range ondas {
		esperam += o.Esperam
		falhando += o.falhando
		if o.Concluida {
			concluidas++
		}
		merge += o.esperaMerge
		revisao += o.paraRevisao
	}
	if esperam > 0 {
		s.Itens = append(s.Itens, ItemSituacao{Cls: "aviso", Icone: "user", Href: "/", Texto: fmt.Sprintf("%d %s", esperam, plural(esperam, "pendência espera você", "pendências esperam você"))})
	}
	if falhando > 0 {
		s.Itens = append(s.Itens, ItemSituacao{Cls: "erro", Icone: "x", Texto: fmt.Sprintf("%d %s", falhando, plural(falhando, "pipeline falhou", "pipelines falharam"))})
	}
	if merge > 0 {
		s.Itens = append(s.Itens, ItemSituacao{Cls: "info", Icone: "merge", Texto: fmt.Sprintf("%d %s", merge, plural(merge, "MR espera merge", "MRs esperam merge"))})
	}
	if revisao > 0 {
		s.Itens = append(s.Itens, ItemSituacao{Cls: "info", Icone: "merge", Texto: fmt.Sprintf("%d %s", revisao, plural(revisao, "MR pronta para revisão", "MRs prontas para revisão"))})
	}
	if concluidas > 0 {
		s.Itens = append(s.Itens, ItemSituacao{Cls: "ok", Icone: "check", Texto: fmt.Sprintf("%d %s", concluidas, plural(concluidas, "onda concluída", "ondas concluídas"))})
	}
	var venc []string
	for _, nome := range central.Fontes {
		if f, ok := snap.Fontes[nome]; ok && central.Vencido(nome, f.AtualizadoEm, agora) {
			venc = append(venc, nome+" ("+strings.TrimPrefix(frescorEm(agora, nome, f.AtualizadoEm, nil).Idade, "há ")+")")
		}
	}
	s.Vencidas = strings.Join(venc, ", ")
	return s
}
