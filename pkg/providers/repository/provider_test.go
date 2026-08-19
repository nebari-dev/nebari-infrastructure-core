package repository

import "testing"

func TestLocalSource(t *testing.T) {
	src := LocalSource{
		Dir:    "/tmp/nebari-gitops-test",
		Branch: "main",
		Path:   "clusters/test",
	}

	if got, want := src.RepoURL(), "file:///tmp/nebari-gitops-test"; got != want {
		t.Errorf("RepoURL() = %q, want %q", got, want)
	}
	if got, want := src.GetBranch(), "main"; got != want {
		t.Errorf("GetBranch() = %q, want %q", got, want)
	}
	if got, want := src.RepoPath(), "clusters/test"; got != want {
		t.Errorf("RepoPath() = %q, want %q", got, want)
	}
}

func TestRemoteSource(t *testing.T) {
	src := RemoteSource{
		URL:    "git@github.com:org/repo.git",
		Branch: "develop",
		Path:   "clusters/prod",
	}

	if got, want := src.RepoURL(), "git@github.com:org/repo.git"; got != want {
		t.Errorf("RepoURL() = %q, want %q", got, want)
	}
	if got, want := src.GetBranch(), "develop"; got != want {
		t.Errorf("GetBranch() = %q, want %q", got, want)
	}
	if got, want := src.RepoPath(), "clusters/prod"; got != want {
		t.Errorf("RepoPath() = %q, want %q", got, want)
	}
}

func TestRemoteSourceArgoCDAuth(t *testing.T) {
	pushAuth := TokenAuth{Token: "push-token"}
	readAuth := TokenAuth{Token: "read-token"}

	t.Run("returns ReadAuth when set", func(t *testing.T) {
		src := RemoteSource{PushAuth: pushAuth, ReadAuth: readAuth}

		got, ok := src.ArgoCDAuth().(TokenAuth)
		if !ok {
			t.Fatalf("ArgoCDAuth() = %T, want TokenAuth", src.ArgoCDAuth())
		}
		if got.Token != "read-token" {
			t.Errorf("ArgoCDAuth().Token = %q, want %q", got.Token, "read-token")
		}
	})

	t.Run("falls back to PushAuth when ReadAuth is nil", func(t *testing.T) {
		src := RemoteSource{PushAuth: pushAuth}

		got, ok := src.ArgoCDAuth().(TokenAuth)
		if !ok {
			t.Fatalf("ArgoCDAuth() = %T, want TokenAuth", src.ArgoCDAuth())
		}
		if got.Token != "push-token" {
			t.Errorf("ArgoCDAuth().Token = %q, want %q", got.Token, "push-token")
		}
	})
}
