package central

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Limites de tamanho para mensagens enviadas ao coordenador e respostas dele.
const (
	LimiteMensagem         = LimiteResposta
	LimiteRespostaMensagem = 64 << 10
)

// Mensagem é uma mensagem enviada pela web ao coordenador de uma onda e a resposta única dele.
type Mensagem struct {
	ID             int64   `json:"id"`
	OndaID         int64   `json:"onda_id"`
	Coordenador    string  `json:"coordenador"`
	Texto          string  `json:"texto"`
	EnviadaPor     string  `json:"enviada_por"`
	EnviadaEm      string  `json:"enviada_em"`
	AvisadaEm      *string `json:"avisada_em"`
	AvisoFalhouEm  *string `json:"aviso_falhou_em"`
	Resposta       *string `json:"resposta"`
	RespondidaPor  *string `json:"respondida_por"`
	RespondidaEm   *string `json:"respondida_em"`
	SemCoordenador bool    `json:"sem_coordenador"`
}

// AvisoMensagem é uma mensagem ainda não entregue ao coordenador da onda.
type AvisoMensagem struct {
	Mensagem    Mensagem
	Coordenador string
	Pane        *string
}

// FiltroMensagens restringe ListarMensagens e `orq msg list`.
type FiltroMensagens struct {
	OndaID  int64
	Abertas bool // sem resposta
}

// EnviarMensagem grava uma nova mensagem ligada a uma onda aberta e registra o evento msg.enviar
// no diário.
func (s *Store) EnviarMensagem(ctx context.Context, ondaID int64, texto, por string) (Mensagem, error) {
	texto = strings.ReplaceAll(strings.TrimSpace(texto), "\r\n", "\n")
	if err := linhaValida("enviada_por", por, limiteAutor, true); err != nil {
		return Mensagem{}, err
	}
	if err := TextoLivreValido("mensagem", texto, LimiteMensagem); err != nil {
		return Mensagem{}, err
	}
	if texto == "" {
		return Mensagem{}, uso("mensagem vazia", "escreva a mensagem ao coordenador")
	}
	var id int64
	err := s.escrever(ctx, func(tx *sql.Tx) error {
		var dono string
		var encerrada *string
		err := tx.QueryRowContext(ctx, `SELECT coordenador, encerrada_em FROM onda WHERE id = ?`, ondaID).Scan(&dono, &encerrada)
		if semLinha(err) {
			return naoEncontrado(fmt.Sprintf("onda %d não existe", ondaID), "veja as ondas com orq onda list")
		}
		if err != nil {
			return err
		}
		if encerrada != nil {
			return conflito("onda_encerrada", fmt.Sprintf("a onda %d foi encerrada em %s", ondaID, *encerrada), "")
		}
		agora := s.agora()
		res, err := tx.ExecContext(ctx, `INSERT INTO mensagem (onda_id, texto, enviada_por, enviada_em) VALUES (?, ?, ?, ?)`,
			ondaID, texto, por, agora)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		return s.registrar(ctx, tx, evento{
			coordenador: dono,
			ondaID:      ondaID,
			tipo:        "msg.enviar",
			detalhe:     map[string]any{"id": id, "por": por, "texto": texto},
		})
	})
	if err != nil {
		return Mensagem{}, err
	}
	return s.VerMensagem(ctx, id)
}

// VerMensagem devolve a mensagem pelo id.
func (s *Store) VerMensagem(ctx context.Context, id int64) (Mensagem, error) {
	var ms []Mensagem
	err := s.ler(ctx, func(tx *sql.Tx) error {
		var err error
		ms, err = carregarMensagens(ctx, tx, `WHERE m.id = ?`, id)
		return err
	})
	if err != nil {
		return Mensagem{}, err
	}
	if len(ms) == 0 {
		return Mensagem{}, naoEncontrado(fmt.Sprintf("mensagem #%d não existe", id), "veja as mensagens com orq msg list")
	}
	return ms[0], nil
}

// ListarMensagens devolve as mensagens filtradas em ordem decrescente de envio (id DESC).
func (s *Store) ListarMensagens(ctx context.Context, f FiltroMensagens) ([]Mensagem, error) {
	var where []string
	var args []any
	if f.OndaID != 0 {
		where, args = append(where, "m.onda_id = ?"), append(args, f.OndaID)
	}
	if f.Abertas {
		where = append(where, "m.respondida_em IS NULL")
	}
	clause := ""
	if len(where) > 0 {
		clause = "WHERE " + strings.Join(where, " AND ")
	}
	clause += " ORDER BY m.id DESC"
	var ms []Mensagem
	err := s.ler(ctx, func(tx *sql.Tx) error {
		var err error
		ms, err = carregarMensagens(ctx, tx, clause, args...)
		return err
	})
	return ms, err
}

// ResponderMensagem grava a resposta única do coordenador à mensagem e registra msg.responder no
// diário. Tentar responder novamente falha com conflito mensagem_respondida.
func (s *Store) ResponderMensagem(ctx context.Context, coord *Coordenador, id int64, texto string) (Mensagem, error) {
	texto = strings.ReplaceAll(strings.TrimSpace(texto), "\r\n", "\n")
	if err := TextoLivreValido("resposta", texto, LimiteRespostaMensagem); err != nil {
		return Mensagem{}, err
	}
	if texto == "" {
		return Mensagem{}, uso("resposta vazia", "passe o texto da resposta ou --arquivo <caminho> (- para stdin)")
	}
	err := s.escrever(ctx, func(tx *sql.Tx) error {
		var ondaID int64
		var dono string
		var respondidaEm *string
		err := tx.QueryRowContext(ctx, `SELECT m.onda_id, o.coordenador, m.respondida_em FROM mensagem m JOIN onda o ON o.id = m.onda_id WHERE m.id = ?`, id).
			Scan(&ondaID, &dono, &respondidaEm)
		if semLinha(err) {
			return naoEncontrado(fmt.Sprintf("mensagem #%d não existe", id), "veja as mensagens com orq msg list")
		}
		if err != nil {
			return err
		}
		if respondidaEm != nil {
			return conflito("mensagem_respondida", fmt.Sprintf("mensagem #%d já foi respondida em %s", id, *respondidaEm), "")
		}
		por := dono
		if coord != nil && coord.Nome != "" {
			por = coord.Nome
		}
		agora := s.agora()
		if _, err := tx.ExecContext(ctx, `UPDATE mensagem SET resposta = ?, respondida_por = ?, respondida_em = ? WHERE id = ?`,
			texto, por, agora, id); err != nil {
			return err
		}
		return s.registrar(ctx, tx, evento{
			coordenador: por,
			ondaID:      ondaID,
			tipo:        "msg.responder",
			detalhe:     map[string]any{"id": id, "por": por, "resposta": texto},
		})
	})
	if err != nil {
		return Mensagem{}, err
	}
	return s.VerMensagem(ctx, id)
}

// MensagensParaAvisar lista as mensagens de ondas abertas ainda sem aviso nem resposta, da mais
// antiga para a mais nova.
func (s *Store) MensagensParaAvisar(ctx context.Context) ([]AvisoMensagem, error) {
	var avisos []AvisoMensagem
	err := s.ler(ctx, func(tx *sql.Tx) error {
		ms, err := carregarMensagens(ctx, tx, `WHERE m.avisada_em IS NULL AND m.respondida_em IS NULL AND o.encerrada_em IS NULL ORDER BY m.id ASC`)
		if err != nil {
			return err
		}
		for _, m := range ms {
			a := AvisoMensagem{Mensagem: m, Coordenador: m.Coordenador}
			if err := tx.QueryRowContext(ctx, `SELECT pane_id FROM coordenador WHERE nome = ?`, m.Coordenador).Scan(&a.Pane); err != nil && !semLinha(err) {
				return err
			}
			avisos = append(avisos, a)
		}
		return nil
	})
	return avisos, err
}

// MarcarMensagemAvisada grava avisada_em, limpa aviso_falhou_em e registra msg.avisada no diário.
func (s *Store) MarcarMensagemAvisada(ctx context.Context, id int64) error {
	return s.escrever(ctx, func(tx *sql.Tx) error {
		var ondaID int64
		var dono string
		err := tx.QueryRowContext(ctx, `SELECT m.onda_id, o.coordenador FROM mensagem m JOIN onda o ON o.id = m.onda_id WHERE m.id = ?`, id).
			Scan(&ondaID, &dono)
		if semLinha(err) {
			return nil
		}
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE mensagem SET avisada_em = ?, aviso_falhou_em = NULL WHERE id = ? AND avisada_em IS NULL`, s.agora(), id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		return s.registrar(ctx, tx, evento{
			coordenador: dono,
			ondaID:      ondaID,
			tipo:        "msg.avisada",
			detalhe:     map[string]any{"id": id, "coordenador": dono},
		})
	})
}

// RegistrarFalhaAvisoMensagem grava aviso_falhou_em e msg.aviso_falhou uma única vez enquanto
// aviso_falhou_em IS NULL e a mensagem ainda não foi avisada.
func (s *Store) RegistrarFalhaAvisoMensagem(ctx context.Context, id int64, motivo string) error {
	return s.escrever(ctx, func(tx *sql.Tx) error {
		var ondaID int64
		var dono string
		err := tx.QueryRowContext(ctx, `SELECT m.onda_id, o.coordenador FROM mensagem m JOIN onda o ON o.id = m.onda_id WHERE m.id = ?`, id).
			Scan(&ondaID, &dono)
		if semLinha(err) {
			return nil
		}
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE mensagem SET aviso_falhou_em = ? WHERE id = ? AND avisada_em IS NULL AND aviso_falhou_em IS NULL`, s.agora(), id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		return s.registrar(ctx, tx, evento{
			coordenador: dono,
			ondaID:      ondaID,
			tipo:        "msg.aviso_falhou",
			detalhe:     map[string]any{"id": id, "coordenador": dono, "motivo": motivo},
		})
	})
}

func carregarMensagens(ctx context.Context, q querier, clause string, args ...any) ([]Mensagem, error) {
	rows, err := q.QueryContext(ctx, `SELECT m.id, m.onda_id, o.coordenador, m.texto, m.enviada_por, m.enviada_em, m.avisada_em, m.aviso_falhou_em, m.resposta, m.respondida_por, m.respondida_em
		FROM mensagem m JOIN onda o ON o.id = m.onda_id `+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ms := []Mensagem{}
	for rows.Next() {
		var m Mensagem
		if err := rows.Scan(&m.ID, &m.OndaID, &m.Coordenador, &m.Texto, &m.EnviadaPor, &m.EnviadaEm, &m.AvisadaEm, &m.AvisoFalhouEm, &m.Resposta, &m.RespondidaPor, &m.RespondidaEm); err != nil {
			return nil, err
		}
		m.SemCoordenador = m.AvisoFalhouEm != nil && m.AvisadaEm == nil && m.RespondidaEm == nil
		ms = append(ms, m)
	}
	return ms, rows.Err()
}
