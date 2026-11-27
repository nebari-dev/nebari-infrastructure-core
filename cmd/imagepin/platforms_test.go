package main

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
)

// push stores data in s and returns its descriptor.
func push(t *testing.T, s *memory.Store, mediaType string, data []byte) ocispec.Descriptor {
	t.Helper()
	desc := content.NewDescriptorFromBytes(mediaType, data)
	if err := s.Push(context.Background(), desc, bytes.NewReader(data)); err != nil {
		t.Fatalf("push %s: %v", mediaType, err)
	}
	return desc
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// pushImage stores an image manifest whose config names os/arch, and
// returns the manifest's descriptor (without a platform, as a registry
// returns it).
func pushImage(t *testing.T, s *memory.Store, os, arch, build string) ocispec.Descriptor {
	t.Helper()
	config := push(t, s, ocispec.MediaTypeImageConfig, mustJSON(t, map[string]string{"os": os, "architecture": arch, "build": build}))
	return push(t, s, ocispec.MediaTypeImageManifest, mustJSON(t, map[string]any{"schemaVersion": 2, "config": config}))
}

// pushArtifact stores a manifest that isn't a container image, such as a
// signature or SBOM.
func pushArtifact(t *testing.T, s *memory.Store) ocispec.Descriptor {
	t.Helper()
	config := push(t, s, "application/vnd.example.signature.config+json", []byte(`{}`))
	return push(t, s, ocispec.MediaTypeImageManifest, mustJSON(t, map[string]any{"schemaVersion": 2, "config": config}))
}

func pushIndex(t *testing.T, s *memory.Store, entries ...ocispec.Descriptor) ocispec.Descriptor {
	t.Helper()
	return push(t, s, ocispec.MediaTypeImageIndex, mustJSON(t, map[string]any{"schemaVersion": 2, "manifests": entries}))
}

func withPlatform(d ocispec.Descriptor, os, arch, variant string) ocispec.Descriptor {
	d.Platform = &ocispec.Platform{OS: os, Architecture: arch, Variant: variant}
	return d
}

func TestPlatformsOf(t *testing.T) {
	ctx := context.Background()
	want := []string{"linux/amd64", "linux/arm64"}

	t.Run("index as vendors publish it", func(t *testing.T) {
		s := memory.New()
		amd64 := pushImage(t, s, "linux", "amd64", "a")      // no platform in the index entry
		arm64 := pushImage(t, s, "linux", "arm64", "b")      // the one that should be kept
		arm64Again := pushImage(t, s, "linux", "arm64", "c") // a second arm64 variant
		s390x := pushImage(t, s, "linux", "s390x", "d")      // not a platform NIC runs on
		attestation := pushImage(t, s, "unknown", "unknown", "e")
		signature := pushArtifact(t, s)
		// A nested index is skipped without being read, so it isn't pushed:
		// reading it would fail the test.
		nested := content.NewDescriptorFromBytes(ocispec.MediaTypeImageIndex, []byte(`{"never":"read"}`))

		index := pushIndex(t, s,
			withPlatform(s390x, "linux", "s390x", ""),
			amd64,
			withPlatform(arm64, "linux", "arm64", "v8"),
			withPlatform(arm64Again, "linux", "arm64", ""),
			withPlatform(attestation, "unknown", "unknown", ""),
			signature,
			nested,
		)

		got, err := platformsOf(ctx, s, index, want)
		if err != nil {
			t.Fatal(err)
		}
		wantGot := []platformDigest{
			{Platform: "linux/amd64", Digest: amd64.Digest.String()},
			{Platform: "linux/arm64", Digest: arm64.Digest.String()},
		}
		if !reflect.DeepEqual(got, wantGot) {
			t.Errorf("platformsOf = %+v, want %+v", got, wantGot)
		}
	})

	t.Run("single-platform image", func(t *testing.T) {
		s := memory.New()
		arm64 := pushImage(t, s, "linux", "arm64", "a")
		got, err := platformsOf(ctx, s, arm64, want)
		if err != nil {
			t.Fatal(err)
		}
		wantGot := []platformDigest{{Platform: "linux/arm64", Digest: arm64.Digest.String()}}
		if !reflect.DeepEqual(got, wantGot) {
			t.Errorf("platformsOf = %+v, want %+v", got, wantGot)
		}
	})

	t.Run("index entry that can't be read", func(t *testing.T) {
		s := memory.New()
		// Not pushed: the entry has no platform, so its manifest must be
		// read to find one, and that read fails.
		missing := content.NewDescriptorFromBytes(ocispec.MediaTypeImageManifest, []byte(`{"schemaVersion":2}`))
		index := pushIndex(t, s, missing)
		if _, err := platformsOf(ctx, s, index, want); err == nil {
			t.Error("err = nil, want an error for the unreadable manifest")
		}
	})
}

func TestMissingPlatforms(t *testing.T) {
	want := []string{"linux/amd64", "linux/arm64"}
	amd64 := platformDigest{Platform: "linux/amd64", Digest: "sha256:a"}
	arm64 := platformDigest{Platform: "linux/arm64", Digest: "sha256:b"}
	tests := []struct {
		name  string
		found []platformDigest
		want  []string
	}{
		{"all found", []platformDigest{amd64, arm64}, nil},
		{"none found", nil, []string{"linux/amd64", "linux/arm64"}},
		{"arm64 missing", []platformDigest{amd64}, []string{"linux/arm64"}},
		{"amd64 missing", []platformDigest{arm64}, []string{"linux/amd64"}},
		{"extra platforms ignored", []platformDigest{amd64, arm64, {Platform: "linux/s390x"}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := missingPlatforms(want, tt.found); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("missingPlatforms = %v, want %v", got, tt.want)
			}
		})
	}
}
