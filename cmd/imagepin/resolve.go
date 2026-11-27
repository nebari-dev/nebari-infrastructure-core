package main

import (
	"context"
	"fmt"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// resolution is what the registry says about one rendered reference, today.
// Nothing here is written anywhere; it is a report for review.
type resolution struct {
	digest     string // what the reference points to now
	multiArch  bool   // digest is a multi-platform index / manifest list
	pinnedGone bool   // the reference's own digest no longer exists
	tagNow     string // for tag@digest: the digest the tag resolves to

	// For tag@digest only. tagMismatch is set when the tag resolves to a
	// different digest than the one pinned; tagCheckErr is set when the tag
	// couldn't be looked up, so the pin's tag was not checked at all.
	tagMismatch string
	tagCheckErr string

	// platforms is what digest provides for each wanted platform; filled
	// only when the resolver was asked for platforms (see resolver.platforms).
	platforms []platformDigest

	err error
}

type resolver struct {
	client *auth.Client

	// platforms, when set, also reads which of these platforms each resolved
	// digest provides, and each one's own digest. Only the inventory needs
	// it, so other commands skip the extra registry reads.
	platforms []string
}

// newResolver uses your Docker credentials when present (useful for Docker Hub
// rate limits) and anonymous access otherwise. It never logs in or writes.
func newResolver() *resolver {
	c := &auth.Client{Client: retry.DefaultClient, Cache: auth.NewCache()}
	if store, err := credentials.NewStoreFromDocker(credentials.StoreOptions{}); err == nil {
		c.Credential = credentials.Credential(store)
	}
	return &resolver{client: c}
}

// resolve asks the registry what a rendered reference points to. Lookups are
// read-only HEAD requests on manifests; no image layers are downloaded.
//
//   - tag only:     resolve the tag (this is "today's" digest; tags can move)
//   - digest:       confirm the pinned digest still exists
//   - tag + digest: also resolve the tag, and report a mismatch if it
//     resolves to a different digest than the one pinned
func (r *resolver) resolve(ctx context.Context, ref string) resolution {
	name, tag, digest := splitRef(ref)
	repo, err := remote.NewRepository(normalizeName(name))
	if err != nil {
		return resolution{err: err}
	}
	repo.Client = r.client

	var res resolution
	if digest != "" {
		desc, err := repo.Resolve(ctx, digest)
		if err != nil {
			return resolution{err: fmt.Errorf("pinned digest: %w", err), pinnedGone: true}
		}
		res.digest = desc.Digest.String()
		res.multiArch = isIndex(desc.MediaType)
		if res.digest != digest {
			return resolution{err: fmt.Errorf("registry returned %s for pinned %s", res.digest, digest)}
		}
		if err := r.readPlatforms(ctx, repo, desc, &res); err != nil {
			return resolution{err: err}
		}
	}

	lookup := tag
	if lookup == "" && digest == "" {
		lookup = "latest" // what a container runtime pulls for a bare name
	}
	if lookup != "" {
		desc, err := repo.Resolve(ctx, lookup)
		if err != nil {
			if digest != "" {
				// The pinned digest resolved; only the tag lookup failed.
				res.tagCheckErr = fmt.Sprintf("could not look up tag %q to check it against the pinned digest: %v", lookup, err)
				return res
			}
			return resolution{err: err}
		}
		if digest == "" {
			res.digest = desc.Digest.String()
			res.multiArch = isIndex(desc.MediaType)
			if err := r.readPlatforms(ctx, repo, desc, &res); err != nil {
				return resolution{err: err}
			}
		} else {
			res.tagNow = desc.Digest.String()
			if res.tagNow != digest {
				res.tagMismatch = fmt.Sprintf("tag %q resolves to %s, but the pin records %s; the pinned digest is what gets deployed", tag, res.tagNow, digest)
			}
		}
	}
	return res
}

// normalizeName expands the Docker Hub forms a container runtime accepts into
// the full name a registry lookup needs: "busybox", "docker.io/busybox" and
// "index.docker.io/busybox" -> "docker.io/library/busybox"; "otel/x" ->
// "docker.io/otel/x". Otherwise the first path segment is a registry only if
// it looks like a host, and the name is returned unchanged.
func normalizeName(name string) string {
	first, rest, found := strings.Cut(name, "/")
	switch {
	case !found:
		return "docker.io/library/" + name
	case first == "docker.io" || first == "index.docker.io":
		if !strings.Contains(rest, "/") {
			rest = "library/" + rest // official images live under library/
		}
		return "docker.io/" + rest
	case strings.ContainsAny(first, ".:") || first == "localhost":
		return name
	default:
		return "docker.io/" + name
	}
}

func isIndex(mediaType string) bool {
	return mediaType == "application/vnd.oci.image.index.v1+json" ||
		mediaType == "application/vnd.docker.distribution.manifest.list.v2+json"
}

// readPlatforms fills res.platforms for the digest desc names, when the
// resolver was asked for platforms. A failure here is a failure to resolve.
func (r *resolver) readPlatforms(ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor, res *resolution) error {
	if len(r.platforms) == 0 {
		return nil
	}
	p, err := platformsOf(ctx, repo, desc, r.platforms)
	if err != nil {
		return fmt.Errorf("read platforms of %s: %w", desc.Digest, err)
	}
	res.platforms = p
	return nil
}
