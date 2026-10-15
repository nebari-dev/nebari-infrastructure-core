package output

import (
	"bytes"
	"testing"
)

func TestWriteBanners(t *testing.T) {
	section := Section{
		Title: "ARGO CD INSTALLED",
		Body: []string{
			"",
			"To access Argo CD:",
			"",
			"",
			"  kubectl port-forward svc/argocd-server -n argocd 8080:443",
			"",
		},
	}
	want := "\n" + bannerRule + "\n  ARGO CD INSTALLED\n" + bannerRule + "\n" +
		"\n  To access Argo CD:\n\n\n    kubectl port-forward svc/argocd-server -n argocd 8080:443\n\n" +
		bannerRule + "\n\n"

	var buf bytes.Buffer
	WriteBanners(&buf, []Section{section})
	if buf.String() != want {
		t.Errorf("got\n%q\nwant\n%q", buf.String(), want)
	}
}
