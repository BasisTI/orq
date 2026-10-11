package enriquecer

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/BasisTI/orq/internal/central"
	"github.com/BasisTI/orq/internal/refs"
)

// ConsultarHerdr lê a sessão do Herdr com `herdr api snapshot` (session.snapshot): numa chamada
// só vêm os agentes (o mesmo registro de `herdr agent list`, com status e nome), os panes sem
// agente e os tokens dos panes e workspaces.
func (f Ferramentas) ConsultarHerdr(ctx context.Context) (central.SessaoHerdr, error) {
	out, err := f.Rodar(ctx, "herdr", "api", "snapshot")
	if err != nil {
		return central.SessaoHerdr{}, err
	}
	return ParseSessaoHerdr(out)
}

// ParseSessaoHerdr converte a resposta de `herdr api snapshot`.
func ParseSessaoHerdr(b []byte) (central.SessaoHerdr, error) {
	type pane struct {
		PaneID      string            `json:"pane_id"`
		WorkspaceID string            `json:"workspace_id"`
		Agent       *string           `json:"agent"`
		AgentStatus string            `json:"agent_status"`
		Name        *string           `json:"name"`
		Tokens      map[string]string `json:"tokens"`
	}
	var r struct {
		Result *struct {
			Snapshot *struct {
				Panes      []pane `json:"panes"`
				Agents     []pane `json:"agents"`
				Workspaces []struct {
					WorkspaceID string            `json:"workspace_id"`
					Tokens      map[string]string `json:"tokens"`
				} `json:"workspaces"`
			} `json:"snapshot"`
		} `json:"result"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return central.SessaoHerdr{}, &Falha{Codigo: ErrRespostaInvalida, Mensagem: "herdr api snapshot: " + err.Error()}
	}
	if r.Error != nil {
		return central.SessaoHerdr{}, &Falha{Codigo: ErrFalhaComando, Mensagem: "herdr api snapshot: " + r.Error.Code + ": " + r.Error.Message}
	}
	if r.Result == nil || r.Result.Snapshot == nil {
		return central.SessaoHerdr{}, &Falha{Codigo: ErrRespostaInvalida, Mensagem: "herdr api snapshot sem result.snapshot"}
	}
	snap := r.Result.Snapshot
	s := central.SessaoHerdr{Panes: map[string]central.PaneHerdr{}, Workspaces: map[string]central.WorkspaceHerdr{}}
	for _, w := range snap.Workspaces {
		s.Workspaces[w.WorkspaceID] = central.WorkspaceHerdr{Tokens: w.Tokens}
	}
	for _, p := range snap.Panes {
		s.Panes[p.PaneID] = central.PaneHerdr{Status: valorOu(p.AgentStatus, "unknown"), Agente: deref(p.Agent), Nome: deref(p.Name), WorkspaceID: p.WorkspaceID, Tokens: p.Tokens}
	}
	// O registro do agente é o de `herdr agent list`: vale mais que o do pane.
	for _, a := range snap.Agents {
		p, ok := s.Panes[a.PaneID]
		if !ok {
			p = central.PaneHerdr{WorkspaceID: a.WorkspaceID, Tokens: a.Tokens}
		}
		p.Status = valorOu(a.AgentStatus, p.Status)
		if a.Agent != nil {
			p.Agente = *a.Agent
		}
		if a.Name != nil {
			p.Nome = *a.Name
		}
		s.Panes[a.PaneID] = p
	}
	return s, nil
}

// Forja de uma MR.
type forja struct {
	ferramenta string // glab ou gh
	host       string
	projeto    string
	iid        int
}

func forjaDe(rawURL string) (forja, error) {
	m, err := refs.ParseMR(rawURL)
	if err != nil {
		return forja{}, &Falha{Codigo: ErrRespostaInvalida, Mensagem: err.Error()}
	}
	u, _ := url.Parse(m.URL)
	f := forja{ferramenta: "glab", host: u.Host, projeto: m.ProjetoPath, iid: m.IID}
	if !strings.Contains(u.Path, "/-/merge_requests/") {
		f.ferramenta = "gh"
	}
	return f, nil
}

// ConsultarMR lê a MR na forja (só leitura) e devolve o estado normalizado.
func (f Ferramentas) ConsultarMR(ctx context.Context, rawURL string) (central.ValorMR, error) {
	fj, err := forjaDe(rawURL)
	if err != nil {
		return central.ValorMR{}, err
	}
	if fj.ferramenta == "glab" {
		// O path do projeto vai inteiro codificado num segmento: basis%2Fiac%2Fplataforma.
		caminho := "projects/" + url.QueryEscape(fj.projeto) + "/merge_requests/" + strconv.Itoa(fj.iid)
		out, err := f.Rodar(ctx, "glab", "api", "--hostname", fj.host, caminho)
		if err != nil {
			return central.ValorMR{}, classificarForja(err)
		}
		var r struct {
			State               string  `json:"state"`
			Draft               bool    `json:"draft"`
			WorkInProgress      bool    `json:"work_in_progress"`
			DetailedMergeStatus string  `json:"detailed_merge_status"`
			MergedAt            *string `json:"merged_at"`
			WebURL              string  `json:"web_url"`
		}
		if err := json.Unmarshal(out, &r); err != nil || r.State == "" {
			return central.ValorMR{}, &Falha{Codigo: ErrRespostaInvalida, Mensagem: "glab api devolveu algo que não é uma MR: " + resumir(out)}
		}
		return central.ValorMR{Estado: NormalizarGitLab(r.State, r.Draft || r.WorkInProgress, r.DetailedMergeStatus, r.MergedAt != nil), WebURL: r.WebURL}, nil
	}
	args := []string{"api"}
	if fj.host != "github.com" {
		args = append(args, "--hostname", fj.host)
	}
	args = append(args, "repos/"+fj.projeto+"/pulls/"+strconv.Itoa(fj.iid))
	out, err := f.Rodar(ctx, "gh", args...)
	if err != nil {
		return central.ValorMR{}, classificarForja(err)
	}
	var r struct {
		State          string `json:"state"`
		Draft          bool   `json:"draft"`
		Merged         bool   `json:"merged"`
		MergeableState string `json:"mergeable_state"`
		HTMLURL        string `json:"html_url"`
	}
	if err := json.Unmarshal(out, &r); err != nil || r.State == "" {
		return central.ValorMR{}, &Falha{Codigo: ErrRespostaInvalida, Mensagem: "gh api devolveu algo que não é um PR: " + resumir(out)}
	}
	return central.ValorMR{Estado: NormalizarGitHub(r.State, r.Draft, r.Merged, r.MergeableState), WebURL: r.HTMLURL}, nil
}

// NormalizarGitLab traduz a MR do GitLab: mesclada, fechada, draft, pronta (aberta e
// mesclável) ou aberta (aberta com pendência: conflito, pipeline, discussão, ou o GitLab ainda
// não calculou).
func NormalizarGitLab(state string, draft bool, detailedMergeStatus string, mesclada bool) string {
	switch {
	case state == "merged" || mesclada:
		return "mesclada"
	case state == "closed":
		return "fechada"
	case draft:
		return "draft"
	case detailedMergeStatus == "mergeable":
		return "pronta"
	}
	return "aberta"
}

// NormalizarGitHub traduz o PR do GitHub. Pronta é aberta, não draft e com mergeable_state
// clean ou has_hooks; unstable (check falhando), blocked, behind, dirty e unknown ficam aberta.
func NormalizarGitHub(state string, draft, merged bool, mergeableState string) string {
	switch {
	case merged:
		return "mesclada"
	case state == "closed":
		return "fechada"
	case draft:
		return "draft"
	case mergeableState == "clean" || mergeableState == "has_hooks":
		return "pronta"
	}
	return "aberta"
}

// classificarForja separa autenticação e "não encontrado" das outras falhas do glab e do gh.
func classificarForja(err error) error {
	f := comoFalha(err)
	if f.Codigo != ErrFalhaComando {
		return f
	}
	saida := string(f.Erro) + "\n" + string(f.Saida)
	baixa := strings.ToLower(saida)
	// Só o status HTTP conta: um "401" solto pode ser o número do PR na URL do erro.
	switch {
	case strings.Contains(saida, "HTTP 401") || strings.Contains(baixa, "401 unauthorized") || strings.Contains(baixa, "bad credentials") ||
		strings.Contains(baixa, "auth login") || strings.Contains(baixa, "not logged in"):
		f.Codigo = ErrAutenticacao
	case strings.Contains(saida, "HTTP 404") || strings.Contains(baixa, "404 not found"):
		f.Codigo = ErrNaoEncontrado
	}
	return f
}

// ConsultarTaiga lê a US no Taiga (só leitura) pela CLI taiga.
func (f Ferramentas) ConsultarTaiga(ctx context.Context, projeto string, ref int) (central.ValorTaiga, error) {
	out, err := f.Rodar(ctx, "taiga", "story", "get", strconv.Itoa(ref), "--project", projeto, "--output", "json")
	if err != nil {
		return central.ValorTaiga{}, classificarTaiga(err)
	}
	var r struct {
		Subject         string `json:"subject"`
		URL             string `json:"url"`
		StatusExtraInfo struct {
			Name string `json:"name"`
		} `json:"status_extra_info"`
	}
	if err := json.Unmarshal(out, &r); err != nil || r.Subject == "" {
		return central.ValorTaiga{}, &Falha{Codigo: ErrRespostaInvalida, Mensagem: "taiga story get devolveu algo que não é uma story: " + resumir(out)}
	}
	return central.ValorTaiga{Titulo: r.Subject, Status: r.StatusExtraInfo.Name, URL: r.URL}, nil
}

// classificarTaiga separa autenticação e "não encontrado" das outras falhas da CLI taiga. A
// taiga-cli sai com 3 em erro de autenticação ou credencial (auth_*, session_*, secret_*,
// keyring_*, *_secret_*, password_file_*); os prefixos do código no envelope
// {"error":{"code"}} reforçam quando o código de saída se perde. not_found é não encontrado.
func classificarTaiga(err error) error {
	f := comoFalha(err)
	if f.Codigo != ErrFalhaComando {
		return f
	}
	for _, s := range [][]byte{f.Saida, f.Erro} {
		var env struct {
			Error *struct {
				Code  string `json:"code"`
				Cause string `json:"cause"`
			} `json:"error"`
		}
		if json.Unmarshal(s, &env) != nil || env.Error == nil {
			continue
		}
		c := env.Error.Code
		f.Mensagem = "taiga: " + c
		if env.Error.Cause != "" {
			f.Mensagem += ": " + resumir([]byte(env.Error.Cause))
		}
		switch {
		case codigoCredencialTaiga(c):
			f.Codigo = ErrAutenticacao
		case c == "not_found":
			f.Codigo = ErrNaoEncontrado
		}
		break
	}
	if f.Status == 3 {
		f.Codigo = ErrAutenticacao
	}
	return f
}

func codigoCredencialTaiga(c string) bool {
	for _, p := range []string{"auth_", "session_", "secret_", "keyring_", "password_file_"} {
		if strings.HasPrefix(c, p) {
			return true
		}
	}
	return strings.Contains(c, "_secret_")
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func valorOu(s, padrao string) string {
	if s == "" {
		return padrao
	}
	return s
}
