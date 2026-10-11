package refs

import "testing"

func TestParseUS(t *testing.T) {
	cases := []struct {
		in    string
		want  US
		falha bool
	}{
		{in: "312", want: US{Ref: 312}},
		{in: "TG-312", want: US{Ref: 312}},
		{in: "tg-312", want: US{Ref: 312}},
		{in: "#312", want: US{Ref: 312}},
		{in: "infra-2025/312", want: US{Projeto: "infra-2025", Ref: 312}},
		{in: "Infra-2025/TG-312", want: US{Projeto: "infra-2025", Ref: 312}},
		{in: "", falha: true},
		{in: "TG-", falha: true},
		{in: "0", falha: true},
		{in: "-3", falha: true},
		{in: "+3", falha: true},
		{in: "x/", falha: true},
		{in: "/312", falha: true},
		{in: "a b/312", falha: true},
		{in: "TG-31x", falha: true},
	}
	for _, c := range cases {
		got, err := ParseUS(c.in)
		if c.falha {
			if err == nil {
				t.Errorf("ParseUS(%q) = %+v, queria erro", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("ParseUS(%q) = %+v, %v; queria %+v", c.in, got, err, c.want)
		}
	}
}

func TestParsePend(t *testing.T) {
	cases := []struct {
		in    string
		want  Pend
		falha bool
	}{
		{in: "TG-312#P2", want: Pend{US: US{Ref: 312}, Numero: 2}},
		{in: "312#2", want: Pend{US: US{Ref: 312}, Numero: 2}},
		{in: "infra/312#p10", want: Pend{US: US{Projeto: "infra", Ref: 312}, Numero: 10}},
		{in: "TG-312", falha: true},
		{in: "#P2", falha: true},
		{in: "TG-312#P", falha: true},
		{in: "TG-312#P0", falha: true},
		{in: "TG-312#Px", falha: true},
	}
	for _, c := range cases {
		got, err := ParsePend(c.in)
		if c.falha {
			if err == nil {
				t.Errorf("ParsePend(%q) = %+v, queria erro", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("ParsePend(%q) = %+v, %v; queria %+v", c.in, got, err, c.want)
		}
	}
	if id := PendID(312, 2); id != "TG-312#P2" {
		t.Fatalf("PendID = %q", id)
	}
}

func TestParseMR(t *testing.T) {
	cases := []struct {
		in    string
		want  MR
		falha bool
	}{
		{in: "https://gitlab.basis.com.br/basis/convey/sgo-cli/-/merge_requests/32",
			want: MR{URL: "https://gitlab.basis.com.br/basis/convey/sgo-cli/-/merge_requests/32", ProjetoPath: "basis/convey/sgo-cli", IID: 32}},
		{in: "https://GitLab.basis.com.br/basis/iac/plataforma/-/merge_requests/412/diffs?commit_id=x#note_1",
			want: MR{URL: "https://gitlab.basis.com.br/basis/iac/plataforma/-/merge_requests/412", ProjetoPath: "basis/iac/plataforma", IID: 412, IaC: true}},
		{in: "https://gitlab.basis.com.br/basis/iacx/foo/-/merge_requests/1",
			want: MR{URL: "https://gitlab.basis.com.br/basis/iacx/foo/-/merge_requests/1", ProjetoPath: "basis/iacx/foo", IID: 1}},
		{in: "https://github.com/BasisTI/orq/pull/7",
			want: MR{URL: "https://github.com/BasisTI/orq/pull/7", ProjetoPath: "BasisTI/orq", IID: 7}},
		{in: "https://github.com/BasisTI/orq/pull/7/files",
			want: MR{URL: "https://github.com/BasisTI/orq/pull/7", ProjetoPath: "BasisTI/orq", IID: 7}},
		{in: "https://github.com/BasisTI/orq/issues/7", falha: true},
		{in: "https://gitlab.basis.com.br/-/merge_requests/3", falha: true},
		{in: "https://gitlab.basis.com.br/basis/x/-/merge_requests/", falha: true},
		{in: "https://gitlab.basis.com.br/basis/x/-/merge_requests/abc", falha: true},
		{in: "ftp://gitlab.basis.com.br/basis/x/-/merge_requests/3", falha: true},
		{in: "gitlab.basis.com.br/basis/x/-/merge_requests/3", falha: true},
		{in: "https://user:pw@gitlab.basis.com.br/basis/x/-/merge_requests/3", falha: true},
	}
	for _, c := range cases {
		got, err := ParseMR(c.in)
		if c.falha {
			if err == nil {
				t.Errorf("ParseMR(%q) = %+v, queria erro", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("ParseMR(%q) = %+v, %v; queria %+v", c.in, got, err, c.want)
		}
	}
}
