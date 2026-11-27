package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry/remote/auth"
)

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		name, want string
	}{
		{"busybox", "docker.io/library/busybox"},
		{"otel/opentelemetry-collector-k8s", "docker.io/otel/opentelemetry-collector-k8s"},
		{"docker.io/busybox", "docker.io/library/busybox"},
		{"docker.io/library/busybox", "docker.io/library/busybox"},
		{"docker.io/otel/x", "docker.io/otel/x"},
		{"index.docker.io/busybox", "docker.io/library/busybox"},
		{"index.docker.io/otel/x", "docker.io/otel/x"},
		{"ghcr.io/org/app", "ghcr.io/org/app"},
		{"quay.io/jetstack/cert-manager-controller", "quay.io/jetstack/cert-manager-controller"},
		{"localhost/app", "localhost/app"},
		{"localhost:5000/app", "localhost:5000/app"},
		{"registry.k8s.io/autoscaling/cluster-autoscaler", "registry.k8s.io/autoscaling/cluster-autoscaler"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeName(tt.name); got != tt.want {
				t.Errorf("normalizeName(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// fakeRegistry answers manifest requests for one repository the way a
// registry does: tags point to digests, and each digest has a manifest.
type fakeRegistry struct {
	repo      string
	tags      map[string]string   // tag -> digest
	manifests map[string]manifest // digest -> manifest
	failTags  map[string]bool     // tags that answer 500
}

type manifest struct {
	mediaType string
	body      []byte
}

// addManifest stores body under its digest and returns the digest.
func (f *fakeRegistry) addManifest(mediaType, body string) string {
	sum := sha256.Sum256([]byte(body))
	d := "sha256:" + hex.EncodeToString(sum[:])
	f.manifests[d] = manifest{mediaType: mediaType, body: []byte(body)}
	return d
}

func (f *fakeRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ref, ok := strings.CutPrefix(r.URL.Path, "/v2/"+f.repo+"/manifests/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if f.failTags[ref] {
		http.Error(w, "registry unavailable", http.StatusInternalServerError)
		return
	}
	if d, isTag := f.tags[ref]; isTag {
		ref = d
	}
	m, ok := f.manifests[ref]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", m.mediaType)
	w.Header().Set("Docker-Content-Digest", ref)
	w.Header().Set("Content-Length", strconv.Itoa(len(m.body)))
	if r.Method == http.MethodGet {
		_, _ = w.Write(m.body)
	}
}

// startRegistry serves f over TLS and returns the repository's full name
// (host:port/repo) and a resolver that trusts the test server.
func startRegistry(t *testing.T, f *fakeRegistry) (string, *resolver) {
	t.Helper()
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String() + "/" + f.repo, &resolver{client: &auth.Client{Client: srv.Client()}}
}

func TestResolve(t *testing.T) {
	reg := &fakeRegistry{
		repo:      "org/app",
		tags:      map[string]string{},
		manifests: map[string]manifest{},
		failTags:  map[string]bool{"flaky": true},
	}
	d1 := reg.addManifest(ocispec.MediaTypeImageManifest, `{"schemaVersion":2,"build":1}`)
	d2 := reg.addManifest(ocispec.MediaTypeImageManifest, `{"schemaVersion":2,"build":2}`)
	idx := reg.addManifest(ocispec.MediaTypeImageIndex, `{"schemaVersion":2,"manifests":[]}`)
	reg.tags["1.0"] = d1
	reg.tags["2.0"] = d2
	reg.tags["multi"] = idx
	gone := "sha256:" + strings.Repeat("0", 64)

	name, r := startRegistry(t, reg)
	ctx := context.Background()

	t.Run("tag and digest agree", func(t *testing.T) {
		res := r.resolve(ctx, name+":1.0@"+d1)
		if res.err != nil {
			t.Fatalf("err = %v", res.err)
		}
		if res.digest != d1 || res.tagNow != d1 {
			t.Errorf("digest = %s, tagNow = %s, want both %s", res.digest, res.tagNow, d1)
		}
		if res.tagMismatch != "" || res.tagCheckErr != "" {
			t.Errorf("tagMismatch = %q, tagCheckErr = %q, want both empty", res.tagMismatch, res.tagCheckErr)
		}
	})

	// The reviewer's case: the tag was changed but the digest was not.
	t.Run("tag resolves to a different digest", func(t *testing.T) {
		res := r.resolve(ctx, name+":2.0@"+d1)
		if res.err != nil {
			t.Fatalf("err = %v", res.err)
		}
		if res.digest != d1 {
			t.Errorf("digest = %s, want the pinned %s: the pin is what gets deployed", res.digest, d1)
		}
		if res.tagNow != d2 {
			t.Errorf("tagNow = %s, want %s", res.tagNow, d2)
		}
		want := `tag "2.0" resolves to ` + d2 + `, but the pin records ` + d1
		if !strings.Contains(res.tagMismatch, want) {
			t.Errorf("tagMismatch = %q, want it to contain %q", res.tagMismatch, want)
		}
		if strings.Contains(res.tagMismatch, "upstream") {
			t.Errorf("tagMismatch = %q: it must not claim a cause the registry can't show", res.tagMismatch)
		}
		if res.tagCheckErr != "" {
			t.Errorf("tagCheckErr = %q, want empty", res.tagCheckErr)
		}
	})

	t.Run("tag lookup fails", func(t *testing.T) {
		res := r.resolve(ctx, name+":flaky@"+d1)
		if res.err != nil {
			t.Fatalf("err = %v: the pinned digest resolved, so this is not a lookup failure", res.err)
		}
		if res.digest != d1 {
			t.Errorf("digest = %s, want %s", res.digest, d1)
		}
		if !strings.Contains(res.tagCheckErr, `could not look up tag "flaky"`) {
			t.Errorf("tagCheckErr = %q, want a lookup failure for tag flaky", res.tagCheckErr)
		}
		if res.tagMismatch != "" {
			t.Errorf("tagMismatch = %q: a failed lookup is not a mismatch", res.tagMismatch)
		}
	})

	t.Run("pinned digest no longer exists", func(t *testing.T) {
		res := r.resolve(ctx, name+":1.0@"+gone)
		if res.err == nil || !res.pinnedGone {
			t.Errorf("err = %v, pinnedGone = %v, want an error with pinnedGone", res.err, res.pinnedGone)
		}
	})

	t.Run("tag only", func(t *testing.T) {
		res := r.resolve(ctx, name+":2.0")
		if res.err != nil {
			t.Fatalf("err = %v", res.err)
		}
		if res.digest != d2 || res.multiArch {
			t.Errorf("digest = %s, multiArch = %v, want %s, false", res.digest, res.multiArch, d2)
		}
		if res.tagNow != "" || res.tagMismatch != "" {
			t.Errorf("tagNow = %q, tagMismatch = %q: only tag@digest pins are checked", res.tagNow, res.tagMismatch)
		}
	})

	t.Run("multi-platform index", func(t *testing.T) {
		res := r.resolve(ctx, name+":multi")
		if res.err != nil {
			t.Fatalf("err = %v", res.err)
		}
		if res.digest != idx || !res.multiArch {
			t.Errorf("digest = %s, multiArch = %v, want %s, true", res.digest, res.multiArch, idx)
		}
	})

	t.Run("tag does not exist", func(t *testing.T) {
		if res := r.resolve(ctx, name+":9.9"); res.err == nil {
			t.Error("err = nil, want a lookup error")
		}
	})
}
