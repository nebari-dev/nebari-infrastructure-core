package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
)

// defaultPlatforms are the platforms NIC can deploy on: the cloud providers'
// default x86 nodes, and arm64 (kind on Apple Silicon, Hetzner ARM servers,
// ARM AMIs on AWS). Images are inventoried and scanned for these only.
var defaultPlatforms = []string{"linux/amd64", "linux/arm64"}

// platformDigest is the image a reference provides for one platform.
type platformDigest struct {
	Platform string `json:"platform"` // os/architecture, e.g. linux/arm64
	Digest   string `json:"digest"`   // that platform's own image digest
}

// platformsOf lists the images desc provides for the wanted platforms, in the
// order of want. desc is either a multi-platform index (manifest list) or a
// single image manifest. Every document read is verified against its digest.
// A wanted platform missing from the result isn't published by the vendor.
func platformsOf(ctx context.Context, f content.Fetcher, desc ocispec.Descriptor, want []string) ([]platformDigest, error) {
	found := map[string]string{}

	if isIndex(desc.MediaType) {
		data, err := content.FetchAll(ctx, f, desc)
		if err != nil {
			return nil, fmt.Errorf("read index: %w", err)
		}
		var index struct {
			Manifests []ocispec.Descriptor `json:"manifests"`
		}
		if err := json.Unmarshal(data, &index); err != nil {
			return nil, fmt.Errorf("parse index: %w", err)
		}
		for _, m := range index.Manifests {
			if isIndex(m.MediaType) {
				continue // nested indexes aren't used for platform images
			}
			platform := ""
			if m.Platform != nil {
				platform = m.Platform.OS + "/" + m.Platform.Architecture
			} else {
				// No platform in the index entry: read it from the image itself.
				if platform, err = configPlatform(ctx, f, m); err != nil {
					return nil, err
				}
			}
			// Keep the first entry per platform (arm64 may also list variants).
			if _, seen := found[platform]; !seen && slices.Contains(want, platform) {
				found[platform] = m.Digest.String()
			}
		}
	} else {
		platform, err := configPlatform(ctx, f, desc)
		if err != nil {
			return nil, err
		}
		if slices.Contains(want, platform) {
			found[platform] = desc.Digest.String()
		}
	}

	var out []platformDigest
	for _, p := range want {
		if d, ok := found[p]; ok {
			out = append(out, platformDigest{Platform: p, Digest: d})
		}
	}
	return out, nil
}

// configPlatform reads an image manifest and its config to find the
// platform the image was built for. It returns "" for manifests that aren't
// container images (attestations, signatures and other artifacts).
func configPlatform(ctx context.Context, f content.Fetcher, manifest ocispec.Descriptor) (string, error) {
	data, err := content.FetchAll(ctx, f, manifest)
	if err != nil {
		return "", fmt.Errorf("read manifest %s: %w", manifest.Digest, err)
	}
	var m struct {
		Config ocispec.Descriptor `json:"config"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return "", fmt.Errorf("parse manifest %s: %w", manifest.Digest, err)
	}
	switch m.Config.MediaType {
	case ocispec.MediaTypeImageConfig, "application/vnd.docker.container.image.v1+json":
	default:
		return "", nil // not a container image
	}
	data, err = content.FetchAll(ctx, f, m.Config)
	if err != nil {
		return "", fmt.Errorf("read image config %s: %w", m.Config.Digest, err)
	}
	var config struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("parse image config %s: %w", m.Config.Digest, err)
	}
	return config.OS + "/" + config.Architecture, nil
}

// missingPlatforms returns the wanted platforms not in found.
func missingPlatforms(want []string, found []platformDigest) []string {
	var missing []string
	for _, p := range want {
		if !slices.ContainsFunc(found, func(f platformDigest) bool { return f.Platform == p }) {
			missing = append(missing, p)
		}
	}
	return missing
}
