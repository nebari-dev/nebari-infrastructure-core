package openshift

import "testing"

func TestParseAdminLogin(t *testing.T) {
	// Representative `rosa create admin` output (encoded from the Phase A run).
	out := `INFO: Admin account has been added to cluster 'nebari-ocp-poc'.
INFO: Please securely store this generated password.
INFO: To login, run the following command:

   oc login https://api.nebari-ocp-poc.s4gl.p3.openshiftapps.com:443 --username cluster-admin --password AbCd1-EfGh2-IjKl3-MnOp4

INFO: It may take several minutes for this access to become active.`

	apiURL, username, password, err := parseAdminLogin(out)
	if err != nil {
		t.Fatalf("parseAdminLogin: %v", err)
	}
	if apiURL != "https://api.nebari-ocp-poc.s4gl.p3.openshiftapps.com:443" {
		t.Errorf("apiURL = %q", apiURL)
	}
	if username != "cluster-admin" {
		t.Errorf("username = %q, want cluster-admin", username)
	}
	if password != "AbCd1-EfGh2-IjKl3-MnOp4" {
		t.Errorf("password = %q", password)
	}
}

func TestParseAdminLoginNoMatch(t *testing.T) {
	if _, _, _, err := parseAdminLogin("ERR: something went wrong"); err == nil {
		t.Error("expected error when no oc login line present, got nil")
	}
}

func TestRequireCLIsMissing(t *testing.T) {
	// A binary that cannot exist on PATH should be reported.
	err := requireCLIs("definitely-not-a-real-binary-xyz")
	if err == nil {
		t.Fatal("requireCLIs = nil for missing binary, want error")
	}
	if got := err.Error(); !contains(got, "definitely-not-a-real-binary-xyz") {
		t.Errorf("error %q does not name the missing binary", got)
	}
}

// contains is a tiny substring helper local to the test (the production
// substring check in the aws package is not exported here).
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
