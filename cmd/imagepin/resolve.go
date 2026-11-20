package main

import (
	"context"
	"fmt"
	"strings"

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
	drift      string // tag now points elsewhere than the pinned digest
	tagNow     string // for tag@digest: what the tag resolves to now
	err        error
}

type resolver struct {
	client *auth.Client
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
//   - tag + digest: also resolve the tag, and report drift if it moved
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
	}

	lookup := tag
	if lookup == "" && digest == "" {
		lookup = "latest" // what a container runtime pulls for a bare name
	}
	if lookup != "" {
		desc, err := repo.Resolve(ctx, lookup)
		if err != nil {
			if digest != "" {
				// The pin still works; only the drift check failed.
				res.drift = fmt.Sprintf("could not resolve tag %q to check for drift: %v", lookup, err)
				return res
			}
			return resolution{err: err}
		}
		if digest == "" {
			res.digest = desc.Digest.String()
			res.multiArch = isIndex(desc.MediaType)
		} else {
			res.tagNow = desc.Digest.String()
			if res.tagNow != digest {
				res.drift = fmt.Sprintf("tag %q has moved upstream: now %s", tag, desc.Digest)
			}
		}
	}
	return res
}

// normalizeName expands the Docker short forms a container runtime accepts:
// "busybox" -> "docker.io/library/busybox", "otel/x" -> "docker.io/otel/x".
// The first path segment is a registry only if it looks like a host.
func normalizeName(name string) string {
	first, rest, found := strings.Cut(name, "/")
	if found && (strings.ContainsAny(first, ".:") || first == "localhost") {
		return name
	}
	if !found {
		return "docker.io/library/" + name
	}
	return "docker.io/" + first + "/" + rest
}

func isIndex(mediaType string) bool {
	return mediaType == "application/vnd.oci.image.index.v1+json" ||
		mediaType == "application/vnd.docker.distribution.manifest.list.v2+json"
}
