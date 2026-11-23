package main

import (
	"context"
	"encoding/json"
	"io"
	"time"
)

// inventoryFile is the machine-readable image list the scanning pipeline
// consumes. Every image NIC's render produces is listed, pinned or not, with
// the digest to scan. Fields describe facts only; what to do about them
// (fail, warn, open an issue) is the consumer's decision.
type inventoryFile struct {
	Generated string           `json:"generated"` // UTC, RFC 3339: when digests were resolved
	Images    []inventoryImage `json:"images"`
	Charts    []string         `json:"charts"`
	Notes     []string         `json:"notes"` // known gaps in coverage
}

type inventoryImage struct {
	Ref    string `json:"ref"`    // exactly as rendered
	Status string `json:"status"` // digest | tag | latest | untagged
	SetBy  string `json:"setBy"`  // nic | chart

	// ScanRef is the immutable reference to scan: the fully qualified name
	// plus the digest the registry returned when this file was generated.
	// For a digest-pinned image it is the pinned digest.
	ScanRef   string `json:"scanRef,omitempty"`
	Digest    string `json:"digest,omitempty"`
	MultiArch bool   `json:"multiArch,omitempty"`

	// Mutable is true when the deployed reference isn't pinned by digest:
	// a later pull may return a different image than the one scanned.
	Mutable bool `json:"mutable"`

	// Drift is set when a digest pin's tag now names a different image.
	Drift string `json:"drift,omitempty"`

	Sources []string `json:"sources"`
	Error   string   `json:"error,omitempty"` // lookup failed; nothing verified
}

// digestResolver is what writeInventory needs from a registry client; the
// real one is *resolver, and tests supply canned answers.
type digestResolver interface {
	resolve(ctx context.Context, ref string) resolution
}

// writeInventory resolves every image and writes the inventory as JSON. It
// returns false if any lookup failed; the file is still written, with the
// error recorded on that image, so the failure is visible to the consumer.
func (i *inventory) writeInventory(ctx context.Context, w io.Writer, r digestResolver, lookupRef func(string) string) (bool, error) {
	out := inventoryFile{
		Generated: time.Now().UTC().Format(time.RFC3339),
		Images:    []inventoryImage{},
		Charts:    i.charts,
		Notes:     i.notes,
	}
	ok := true
	for _, ref := range i.sortedRefs() {
		img := i.images[ref]
		status := pinStatus(ref)
		e := inventoryImage{
			Ref:     ref,
			Status:  status,
			SetBy:   "chart",
			Mutable: status != statusDigest,
			Sources: img.sources,
		}
		if img.setByNIC {
			e.SetBy = "nic"
		}
		res := r.resolve(ctx, lookupRef(ref))
		if res.err != nil {
			e.Error = res.err.Error()
			ok = false
		} else {
			name, _, _ := splitRef(ref)
			e.Digest = res.digest
			e.MultiArch = res.multiArch
			e.ScanRef = normalizeName(name) + "@" + res.digest
			e.Drift = res.drift
		}
		out.Images = append(out.Images, e)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return ok, enc.Encode(out)
}
