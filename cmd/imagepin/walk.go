package main

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	kyaml "k8s.io/apimachinery/pkg/util/yaml"
)

type inventory struct {
	images map[string]*image // keyed by the reference exactly as rendered
	charts []string
	notes  []string
}

type image struct {
	sources []string // where the reference was found
	// setByNIC is true when NIC chose this reference: it appears in NIC's
	// own manifests, or NIC's values supply it. False means the chart's
	// default or a reference hard-coded in the chart. If the same reference
	// is found in several places, it is true only if NIC set it everywhere.
	setByNIC bool
}

func newInventory() *inventory {
	return &inventory{images: map[string]*image{}}
}

// origin describes where a rendered document came from, so each image found
// in it can be attributed.
type origin struct {
	source string
	// nicManifest is true for NIC's own manifests: every image in them is NIC's.
	nicManifest bool
	// nicValues holds every string leaf of the NIC values passed to the chart.
	nicValues map[string]bool
}

func (i *inventory) add(ref string, o origin) {
	img, ok := i.images[ref]
	if !ok {
		img = &image{setByNIC: true}
		i.images[ref] = img
	}
	if !o.nicManifest && !valuesSet(ref, o.nicValues) {
		img.setByNIC = false
	}
	for _, s := range img.sources {
		if s == o.source {
			return
		}
	}
	img.sources = append(img.sources, o.source)
}

// valuesSet reports whether NIC's values supply this reference. It matches
// the two ways NIC's values set images: a whole reference in one string
// (Envoy Gateway, init containers), or a repository plus a tag/digest in
// separate keys (most charts). This is a heuristic over string values, not a
// trace of the chart's templates; `list` shows the evidence for review.
func valuesSet(ref string, values map[string]bool) bool {
	if values[ref] {
		return true
	}
	name, tag, digest := splitRef(ref)
	pinned := (tag != "" && values[tag]) ||
		(digest != "" && (values[digest] || values[tag+"@"+digest]))
	if !pinned {
		return false
	}
	for v := range values {
		if strings.Contains(v, "/") && strings.HasSuffix(name, v) {
			return true
		}
	}
	return false
}

// splitRef splits name[:tag][@digest]. A ':' before the last '/' is a
// registry port, not a tag.
func splitRef(ref string) (name, tag, digest string) {
	name, digest, _ = strings.Cut(ref, "@")
	if i := strings.LastIndex(name, ":"); i > strings.LastIndex(name, "/") {
		name, tag = name[:i], name[i+1:]
	}
	return name, tag, digest
}

// stringLeaves collects every string value in a values tree.
func stringLeaves(v any, into map[string]bool) map[string]bool {
	switch t := v.(type) {
	case map[string]any:
		for _, c := range t {
			stringLeaves(c, into)
		}
	case []any:
		for _, c := range t {
			stringLeaves(c, into)
		}
	case string:
		into[t] = true
	}
	return into
}

// manifest walks every document in a rendered YAML stream.
func (i *inventory) manifest(data []byte, o origin) error {
	dec := kyaml.NewYAMLOrJSONDecoder(strings.NewReader(string(data)), 4096)
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err == io.EOF {
			return nil
		} else if err != nil {
			return fmt.Errorf("%s: %w", o.source, err)
		}
		if doc == nil || doc["kind"] == "CustomResourceDefinition" {
			continue // CRD schemas mention "image" fields but deploy nothing.
		}
		if doc["kind"] == "Kustomization" {
			return fmt.Errorf("%s: Kustomize image sources are not supported yet", o.source)
		}

		meta, _ := doc["metadata"].(map[string]any)
		annotations, _ := meta["annotations"].(map[string]any)
		switch annotations["helm.sh/hook"] {
		case "test", "test-success", "test-failure":
			continue // `helm test` pods; Argo CD never runs them.
		}

		apiVersion, _ := doc["apiVersion"].(string)
		if doc["kind"] == "Cluster" && strings.HasPrefix(apiVersion, "postgresql.cnpg.io/") {
			spec, _ := doc["spec"].(map[string]any)
			if img, _ := spec["imageName"].(string); img == "" {
				i.notes = append(i.notes, o.source+": CNPG Cluster without imageName uses the operator's built-in Postgres default.")
			}
		}

		i.walk(doc, o)
	}
}

// imageInText finds image references embedded in strings: controller flags,
// environment variables, and configuration held in ConfigMaps (Envoy Gateway
// puts its proxy image there). It requires a '/' so ordinary words don't match.
var imageInText = regexp.MustCompile(`(?:[a-z0-9.-]+(?::[0-9]+)?/)+[a-z0-9._/-]+(?::[a-zA-Z0-9._-]+(?:@sha256:[a-f0-9]{64})?|@sha256:[a-f0-9]{64})`)

func (i *inventory) walk(value any, o origin) {
	switch v := value.(type) {
	case map[string]any:
		// Some operators' custom resources (the GPU operator's ClusterPolicy)
		// split an image into repository, image and version fields and
		// assemble the reference at run time. Read those as one reference.
		if ref, isTriple := splitImage(v); isTriple {
			if ref != "" {
				i.add(ref, o)
			}
			for key, child := range v {
				if key != "repository" && key != "image" && key != "version" {
					i.walk(child, o)
				}
			}
			return
		}
		for key, child := range v {
			// image: (pods) and imageName: (CNPG Cluster) hold one reference.
			if key == "image" || key == "imageName" {
				if s, ok := child.(string); ok && s != "" {
					i.add(s, o)
					continue
				}
			}
			i.walk(child, o)
		}
	case []any:
		for _, child := range v {
			i.walk(child, o)
		}
	case string:
		for _, m := range imageInText.FindAllString(v, -1) {
			i.add(m, o)
		}
	}
}

// splitImage recognizes an operator component's image, written as separate
// repository, image and version fields. isTriple is true when the map is such
// a component. ref is the assembled reference, or "" when the component is
// disabled (enabled: false) or has no version of its own: the operator then
// supplies one at run time (the GPU operator uses its own version), and its
// own image is already listed from its Deployment.
//
// A disabled component is skipped even without a repository: charts may omit
// an empty one (the GPU operator's vGPU manager, whose image lives in a
// customer's own registry, renders as just enabled: false and image).
func splitImage(m map[string]any) (ref string, isTriple bool) {
	name, ok := m["image"].(string)
	if !ok || name == "" {
		return "", false
	}
	if enabled, ok := m["enabled"].(bool); ok && !enabled {
		return "", true
	}
	repo, ok := m["repository"].(string)
	if !ok || repo == "" {
		return "", false
	}
	version, _ := m["version"].(string)
	switch {
	case version == "":
		return "", true
	case strings.HasPrefix(version, "sha256:"):
		return repo + "/" + name + "@" + version, true
	default:
		return repo + "/" + name + ":" + version, true
	}
}
