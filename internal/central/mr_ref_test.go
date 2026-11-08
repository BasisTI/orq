package central

import "testing"

func TestRefCurtaSempreComProjeto(t *testing.T) {
	for _, c := range []struct{ url, path, quer string }{
		{"https://gitlab.basis.com.br/basis/iac/argocd-apps/-/merge_requests/404", "basis/iac/argocd-apps", "argocd-apps!404"},
		{"https://gitlab.basis.com.br/basis/colaboradados/-/merge_requests/404", "basis/colaboradados", "colaboradados!404"},
		{"https://github.com/BasisTI/orq/pull/66", "BasisTI/orq", "orq#66"},
		{"https://gitlab.basis.com.br/solto/-/merge_requests/3", "solto", "solto!3"},
	} {
		iid := 404
		switch c.quer {
		case "orq#66":
			iid = 66
		case "solto!3":
			iid = 3
		}
		if got := (MR{URL: c.url, ProjetoPath: c.path, IID: iid}).RefCurta(); got != c.quer {
			t.Errorf("%s: %q, quer %q", c.url, got, c.quer)
		}
	}
}
