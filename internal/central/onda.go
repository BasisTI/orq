package central

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/BasisTI/orq/internal/refs"
)

// Valores aceitos para a autorização e a ordem de uma onda.
var (
	Autorizacoes = []string{"plano", "mr-pronta", "merge"}
	Ordens       = []string{"estrita", "livre"}
)

// Limites explica, para quem despacha, até onde cada autorização deixa ir. `done` numa US quer
// dizer que ela chegou a esse limite, e é isso que libera quem depende dela.
var Limites = map[string]string{
	"plano":     "só o planejamento: marque done quando o plano estiver aprovado",
	"mr-pronta": "até a MR pronta para revisão humana, sem merge: marque done com a MR pronta",
	"merge":     "até o merge: marque done com a MR mesclada",
}

// Onda é um grupo de US conduzido por um coordenador com uma autorização declarada.
type Onda struct {
	ID             int64   `json:"id"`
	Coordenador    string  `json:"coordenador"`
	Titulo         string  `json:"titulo"`
	AutorizacaoAte string  `json:"autorizacao_ate"`
	Ordem          string  `json:"ordem"`
	CriadaEm       string  `json:"criada_em"`
	EncerradaEm    *string `json:"encerrada_em"`
	US             []US    `json:"us"`
}

// NovaOnda são os dados de `orq onda criar`.
type NovaOnda struct {
	Titulo  string
	Projeto string // completa as US sem projeto; vazio usa o projeto padrão do coordenador
	Ate     string // vazio = mr-pronta
	Ordem   string // vazio = livre
	US      []refs.US
	Deps    [][2]refs.US // {dependente, dependência}
}

// CriarOnda grava a onda e as US dela em wait, na ordem dada. Uma US numa onda aberta não
// entra em outra. Uma US de onda encerrada muda de onda: guarda o status se a onda nova tem o
// mesmo limite (--ate) e volta a wait se o limite é outro, com evento us.status explicando.
func (s *Store) CriarOnda(ctx context.Context, coord Coordenador, n NovaOnda) (Onda, error) {
	titulo := strings.TrimSpace(n.Titulo)
	if titulo == "" || temControle(titulo) {
		return Onda{}, uso("título da onda vazio ou com caractere de controle", "")
	}
	ate, ordem := valorOu(n.Ate, "mr-pronta"), valorOu(n.Ordem, "livre")
	if !contem(Autorizacoes, ate) {
		return Onda{}, uso(fmt.Sprintf("--ate %q inválido", ate), "use "+strings.Join(Autorizacoes, ", "))
	}
	if !contem(Ordens, ordem) {
		return Onda{}, uso(fmt.Sprintf("--ordem %q inválida", ordem), "use "+strings.Join(Ordens, " ou "))
	}
	if len(n.US) == 0 {
		return Onda{}, uso("onda sem US", "passe --us 215,216")
	}
	projeto := n.Projeto
	if projeto == "" && coord.ProjetoPadrao != nil {
		projeto = *coord.ProjetoPadrao
	}
	completar := func(u refs.US) (refs.US, error) {
		if u.Projeto != "" {
			return u, nil
		}
		if projeto == "" {
			return u, uso(refs.Exibir(u.Ref)+" sem projeto", "passe --projeto <slug> ou registre o coordenador com --projeto")
		}
		p, err := refs.ValidarProjeto(projeto)
		if err != nil {
			return u, uso(err.Error(), "")
		}
		return refs.US{Projeto: p, Ref: u.Ref}, nil
	}
	posicao := map[refs.US]int{}
	lista := make([]refs.US, 0, len(n.US))
	for _, u := range n.US {
		u, err := completar(u)
		if err != nil {
			return Onda{}, err
		}
		if _, dup := posicao[u]; dup {
			return Onda{}, uso(u.Chave()+" repetida em --us", "")
		}
		posicao[u] = len(lista) + 1
		lista = append(lista, u)
	}
	deps := map[refs.US][]refs.US{}
	for _, d := range n.Deps {
		de, err := completar(d[0])
		if err != nil {
			return Onda{}, err
		}
		para, err := completar(d[1])
		if err != nil {
			return Onda{}, err
		}
		for _, u := range []refs.US{de, para} {
			if _, ok := posicao[u]; !ok {
				return Onda{}, uso(u.Chave()+" está em --depende mas não em --us", "dependências ligam US da mesma onda")
			}
		}
		if de == para {
			return Onda{}, uso(de.Chave()+" depende de si mesma", "")
		}
		if ordem == "estrita" && posicao[para] > posicao[de] {
			return Onda{}, uso(fmt.Sprintf("em ordem estrita, %s não pode depender de %s, que vem depois", de.Chave(), para.Chave()), "reordene --us")
		}
		deps[de] = append(deps[de], para)
	}
	if ciclo := acharCiclo(lista, deps); ciclo != "" {
		return Onda{}, uso("dependência circular: "+ciclo, "")
	}

	var id int64
	err := s.escrever(ctx, func(tx *sql.Tx) error {
		agora := s.agora()
		res, err := tx.ExecContext(ctx, `INSERT INTO onda (coordenador, titulo, autorizacao_ate, ordem, criada_em) VALUES (?, ?, ?, ?, ?)`,
			coord.Nome, titulo, ate, ordem, agora)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		chaves := make([]string, len(lista))
		movidas := []string{}
		var reiniciadas []evento
		for i, u := range lista {
			chaves[i] = u.Chave()
			var ondaAtual int64
			var encerrada *string
			var ateAntigo, status string
			err := tx.QueryRowContext(ctx, `SELECT us.onda_id, onda.encerrada_em, onda.autorizacao_ate, us.status FROM us JOIN onda ON onda.id = us.onda_id WHERE us.projeto = ? AND us.ref = ?`, u.Projeto, u.Ref).
				Scan(&ondaAtual, &encerrada, &ateAntigo, &status)
			switch {
			case semLinha(err):
				_, err = tx.ExecContext(ctx, `INSERT INTO us (projeto, ref, onda_id, posicao, status, atualizado_em) VALUES (?, ?, ?, ?, 'wait', ?)`, u.Projeto, u.Ref, id, i+1, agora)
			case err != nil:
			case encerrada == nil:
				err = conflito("us_em_outra_onda", fmt.Sprintf("%s já está na onda %d, aberta", u.Chave(), ondaAtual), fmt.Sprintf("encerre a onda %d ou tire %s de --us", ondaAtual, u.Chave()))
			default:
				movidas = append(movidas, fmt.Sprintf("%s (da onda %d)", u.Chave(), ondaAtual))
				if _, err = tx.ExecContext(ctx, `DELETE FROM dependencia WHERE projeto = ? AND ref = ?`, u.Projeto, u.Ref); err == nil {
					_, err = tx.ExecContext(ctx, `UPDATE us SET onda_id = ?, posicao = ?, atualizado_em = ? WHERE projeto = ? AND ref = ?`, id, i+1, agora, u.Projeto, u.Ref)
				}
				// O status vale para o limite da onda de onde a US veio: com outro limite, ela
				// recomeça em wait (o done de um plano aprovado não é a MR pronta da onda nova).
				if err == nil && ateAntigo != ate && status != "wait" {
					_, err = tx.ExecContext(ctx, `UPDATE us SET status = 'wait' WHERE projeto = ? AND ref = ?`, u.Projeto, u.Ref)
					reiniciadas = append(reiniciadas, evento{coordenador: coord.Nome, ondaID: id, projeto: u.Projeto, ref: u.Ref, tipo: "us.status",
						detalhe: map[string]any{"de": status, "para": "wait", "status": "wait",
							"motivo": fmt.Sprintf("veio da onda %d (até %s) para a onda %d (até %s): o status valia para o limite anterior", ondaAtual, ateAntigo, id, ate)}})
				}
			}
			if err != nil {
				return err
			}
		}
		depende := map[string][]string{}
		for de, paras := range deps {
			for _, para := range paras {
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO dependencia (projeto, ref, dep_projeto, dep_ref) VALUES (?, ?, ?, ?)`, de.Projeto, de.Ref, para.Projeto, para.Ref); err != nil {
					return err
				}
				depende[de.Chave()] = append(depende[de.Chave()], para.Chave())
			}
		}
		detalhe := map[string]any{"titulo": titulo, "autorizacao_ate": ate, "ordem": ordem, "us": chaves, "depende_de": depende}
		if len(movidas) > 0 {
			detalhe["movidas"] = movidas
		}
		if err := s.registrar(ctx, tx, evento{coordenador: coord.Nome, ondaID: id, tipo: "onda.criar", detalhe: detalhe}); err != nil {
			return err
		}
		for _, e := range reiniciadas {
			if err := s.registrar(ctx, tx, e); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Onda{}, err
	}
	return s.VerOnda(ctx, id)
}

// acharCiclo devolve um ciclo do grafo de dependências como "a → b → a", ou "".
func acharCiclo(nos []refs.US, deps map[refs.US][]refs.US) string {
	const (
		branco = iota
		cinza
		preto
	)
	cor := map[refs.US]int{}
	var pilha []refs.US
	var visitar func(u refs.US) string
	visitar = func(u refs.US) string {
		cor[u] = cinza
		pilha = append(pilha, u)
		for _, v := range deps[u] {
			switch cor[v] {
			case cinza:
				var partes []string
				dentro := false
				for _, p := range pilha {
					if p == v {
						dentro = true
					}
					if dentro {
						partes = append(partes, p.Chave())
					}
				}
				return strings.Join(append(partes, v.Chave()), " → ")
			case branco:
				if c := visitar(v); c != "" {
					return c
				}
			}
		}
		pilha = pilha[:len(pilha)-1]
		cor[u] = preto
		return ""
	}
	for _, u := range nos {
		if cor[u] == branco {
			if c := visitar(u); c != "" {
				return c
			}
		}
	}
	return ""
}

// VerOnda devolve a onda com as US, MRs e pendências.
func (s *Store) VerOnda(ctx context.Context, id int64) (Onda, error) {
	var o Onda
	err := s.ler(ctx, func(tx *sql.Tx) error {
		os, err := carregarOndas(ctx, tx, `WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if len(os) == 0 {
			return naoEncontrado(fmt.Sprintf("onda %d não existe", id), "veja as ondas com orq onda list")
		}
		o = os[0]
		return nil
	})
	return o, err
}

// OndaAberta devolve a única onda aberta do coordenador; com nenhuma ou várias, pede o id.
func (s *Store) OndaAberta(ctx context.Context, coord string) (Onda, error) {
	var o Onda
	err := s.ler(ctx, func(tx *sql.Tx) error {
		os, err := carregarOndas(ctx, tx, `WHERE coordenador = ? AND encerrada_em IS NULL`, coord)
		if err != nil {
			return err
		}
		switch len(os) {
		case 0:
			return naoEncontrado("o coordenador "+coord+" não tem onda aberta", "crie uma com orq onda criar")
		case 1:
			o = os[0]
			return nil
		}
		ids := make([]string, len(os))
		for i, x := range os {
			ids[i] = fmt.Sprint(x.ID)
		}
		return uso("o coordenador "+coord+" tem várias ondas abertas: "+strings.Join(ids, ", "), "passe o id: orq onda ver <id>")
	})
	return o, err
}

// FiltroOndas restringe orq onda list.
type FiltroOndas struct {
	Coord   string
	Abertas bool
}

// ListarOndas devolve as ondas por id, com as US.
func (s *Store) ListarOndas(ctx context.Context, f FiltroOndas) ([]Onda, error) {
	var where []string
	var args []any
	if f.Coord != "" {
		where, args = append(where, "coordenador = ?"), append(args, f.Coord)
	}
	if f.Abertas {
		where = append(where, "encerrada_em IS NULL")
	}
	clause := ""
	if len(where) > 0 {
		clause = "WHERE " + strings.Join(where, " AND ")
	}
	var os []Onda
	err := s.ler(ctx, func(tx *sql.Tx) error {
		var err error
		os, err = carregarOndas(ctx, tx, clause, args...)
		return err
	})
	return os, err
}

// EncerrarOnda marca a onda como encerrada; as US ficam como estão.
func (s *Store) EncerrarOnda(ctx context.Context, coord string, id int64) (Onda, error) {
	err := s.escrever(ctx, func(tx *sql.Tx) error {
		var encerrada *string
		var dono string
		err := tx.QueryRowContext(ctx, `SELECT coordenador, encerrada_em FROM onda WHERE id = ?`, id).Scan(&dono, &encerrada)
		if semLinha(err) {
			return naoEncontrado(fmt.Sprintf("onda %d não existe", id), "veja as ondas com orq onda list")
		}
		if err != nil {
			return err
		}
		if encerrada != nil {
			return conflito("onda_encerrada", fmt.Sprintf("a onda %d já foi encerrada em %s", id, *encerrada), "")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE onda SET encerrada_em = ? WHERE id = ?`, s.agora(), id); err != nil {
			return err
		}
		return s.registrar(ctx, tx, evento{coordenador: valorOu(coord, dono), ondaID: id, tipo: "onda.encerrar", detalhe: map[string]any{}})
	})
	if err != nil {
		return Onda{}, err
	}
	return s.VerOnda(ctx, id)
}

// Bloqueio é uma US em wait que ainda não pode sair, com o motivo.
type Bloqueio struct {
	US     string `json:"us"`
	Motivo string `json:"motivo"`
}

// Proxima é a resposta de orq onda proxima. US nula quer dizer que nada é elegível agora, e
// Motivo diz por quê.
type Proxima struct {
	OndaID         int64      `json:"onda_id"`
	AutorizacaoAte string     `json:"autorizacao_ate"`
	Limite         string     `json:"limite"`
	Ordem          string     `json:"ordem"`
	US             *US        `json:"us"`
	Elegiveis      []string   `json:"elegiveis"`
	Bloqueadas     []Bloqueio `json:"bloqueadas"`
	Motivo         string     `json:"motivo"`
	// Encerramento é o próximo passo da onda aberta com todas as US no limite; nulo antes disso.
	Encerramento *Encerramento `json:"encerramento"`
}

// Encerramento diz se a onda pode ser encerrada. Pronta quer dizer toda US no limite (done),
// toda MR mesclada ou fechada e nenhuma pendência aberta; aí Comando traz o `orq onda
// encerrar`. Senão, Faltam lista o que impede, com a chave completa da US (`projeto/312`) e
// da pendência (`projeto/312#P2`): o id curto (`TG-312#P2`) resolve pelo projeto padrão do
// coordenador e erra a US numa onda de outro projeto ou com dois projetos de mesmo número. A onda `plano` não olha as MRs: o plano
// aprovado encerra a onda, e as US seguem para a onda de implementação. O orq só sugere: quem
// encerra é o coordenador.
type Encerramento struct {
	Pronta  bool     `json:"pronta"`
	Comando *string  `json:"comando"`
	Faltam  []string `json:"faltam"`
	Passo   string   `json:"passo"`
}

// EncerramentoDe aplica a regra de encerramento à onda. Nulo se a onda está encerrada, vazia
// ou com alguma US antes do limite. MR sem estado (o daemon ainda não consultou a forja, ou
// não roda) entra em Faltam como "falta conferir": não conta como mesclada.
func EncerramentoDe(o Onda) *Encerramento {
	if o.EncerradaEm != nil || len(o.US) == 0 {
		return nil
	}
	e := &Encerramento{Faltam: []string{}}
	for _, u := range o.US {
		if u.Status != "done" {
			return nil
		}
		if o.AutorizacaoAte != "plano" {
			for _, m := range u.MRs {
				switch {
				case m.Estado == nil:
					e.Faltam = append(e.Faltam, fmt.Sprintf("%s: MR %s sem estado conhecido (falta conferir)", u.Chave, m.URL))
				case *m.Estado != "mesclada" && *m.Estado != "fechada":
					e.Faltam = append(e.Faltam, fmt.Sprintf("%s: MR %s %s (falta merge ou fechamento)", u.Chave, m.URL, *m.Estado))
				}
			}
		}
		for _, p := range u.Pendencias {
			if p.Aberta {
				e.Faltam = append(e.Faltam, fmt.Sprintf("%s#P%d: pendência aberta (resolva ou leve para outra onda ou para o Taiga, registrando no diário)", u.Chave, p.Numero))
			}
		}
	}
	if len(e.Faltam) > 0 {
		e.Passo = "falta concluir antes de encerrar a onda"
		return e
	}
	cmd := fmt.Sprintf("orq onda encerrar %d", o.ID)
	e.Pronta, e.Comando = true, &cmd
	e.Passo = "encerre a onda: " + cmd
	if o.AutorizacaoAte == "plano" {
		e.Passo += ", e crie a onda de implementação com as mesmas US"
	}
	return e
}

// ProximaUS escolhe a próxima US em wait que pode ser despachada. Uma US está liberada quando
// todas as dependências estão em done; em ordem estrita, só a primeira US ainda não concluída
// é candidata, e só depois que a anterior chegou a done. Done é o limite da autorização da
// onda (veja Limites), que vem junto na resposta para quem despacha saber até onde ir.
func (s *Store) ProximaUS(ctx context.Context, id int64) (Proxima, error) {
	o, err := s.VerOnda(ctx, id)
	if err != nil {
		return Proxima{}, err
	}
	return proxima(o), nil
}

func proxima(o Onda) Proxima {
	p := Proxima{OndaID: o.ID, AutorizacaoAte: o.AutorizacaoAte, Limite: Limites[o.AutorizacaoAte], Ordem: o.Ordem, Elegiveis: []string{}, Bloqueadas: []Bloqueio{}}
	if o.EncerradaEm != nil {
		p.Motivo = "onda encerrada em " + *o.EncerradaEm
		return p
	}
	status := map[string]string{}
	for _, u := range o.US {
		status[u.Chave] = u.Status
	}
	pendentes := func(u US) []string {
		var faltam []string
		for _, d := range u.DependeDe {
			if st, ok := status[d]; !ok || st != "done" {
				if !ok {
					st = "fora da onda"
				}
				faltam = append(faltam, fmt.Sprintf("%s (%s)", d, st))
			}
		}
		return faltam
	}
	var andamento []string
	for _, u := range o.US {
		switch u.Status {
		case "done":
			continue
		case "wait":
		default:
			andamento = append(andamento, fmt.Sprintf("%s em %s", u.ID, u.Status))
			if o.Ordem == "estrita" {
				p.Motivo = fmt.Sprintf("ordem estrita: %s ainda em %s", u.ID, u.Status)
				return p
			}
			continue
		}
		if faltam := pendentes(u); len(faltam) > 0 {
			b := Bloqueio{US: u.ID, Motivo: "depende de " + strings.Join(faltam, ", ")}
			p.Bloqueadas = append(p.Bloqueadas, b)
			if o.Ordem == "estrita" {
				p.Motivo = u.ID + " " + b.Motivo
				return p
			}
			continue
		}
		p.Elegiveis = append(p.Elegiveis, u.Chave)
		if p.US == nil {
			x := u
			p.US = &x
		}
		if o.Ordem == "estrita" {
			return p
		}
	}
	if p.US != nil {
		return p
	}
	switch {
	case len(andamento) == 0 && len(p.Bloqueadas) == 0:
		p.Motivo = "onda concluída até " + o.AutorizacaoAte
		p.Encerramento = EncerramentoDe(o)
	case len(p.Bloqueadas) == 0:
		p.Motivo = "nenhuma US em wait; em andamento: " + strings.Join(andamento, ", ")
	default:
		p.Motivo = "nenhuma US em wait está liberada"
	}
	return p
}

func carregarOndas(ctx context.Context, q querier, clause string, args ...any) ([]Onda, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, coordenador, titulo, autorizacao_ate, ordem, criada_em, encerrada_em FROM onda `+clause+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	os := []Onda{}
	for rows.Next() {
		var o Onda
		if err := rows.Scan(&o.ID, &o.Coordenador, &o.Titulo, &o.AutorizacaoAte, &o.Ordem, &o.CriadaEm, &o.EncerradaEm); err != nil {
			rows.Close()
			return nil, err
		}
		os = append(os, o)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range os {
		us, err := carregarUS(ctx, q, `WHERE onda_id = ?`, os[i].ID)
		if err != nil {
			return nil, err
		}
		os[i].US = us
	}
	return os, nil
}

func valorOu(s, padrao string) string {
	if s == "" {
		return padrao
	}
	return s
}

func contem(xs []string, x string) bool {
	for _, y := range xs {
		if x == y {
			return true
		}
	}
	return false
}
