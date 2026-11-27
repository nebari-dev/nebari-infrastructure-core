package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// cannedResolver answers each reference with a fixed resolution.
type cannedResolver map[string]resolution

func (c cannedResolver) resolve(_ context.Context, ref string) resolution {
	if res, ok := c[ref]; ok {
		return res
	}
	return resolution{err: errors.New("unexpected lookup of " + ref)}
}

func TestWriteInventory(t *testing.T) {
	digest := func(c string) string { return "sha256:" + strings.Repeat(c, 64) }
	platforms := []string{"linux/amd64", "linux/arm64"}
	amd64 := platformDigest{Platform: "linux/amd64", Digest: digest("1")}
	arm64 := platformDigest{Platform: "linux/arm64", Digest: digest("2")}

	const (
		chartTag  = "busybox:1.37"
		broken    = "ghcr.io/org/broken:2.0"
		matched   = "ghcr.io/org/matched:1.0@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		mismatch  = "ghcr.io/org/mismatch:1.0@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		unchecked = "ghcr.io/org/unchecked:1.0@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	)
	inv := newInventory()
	inv.add(chartTag, origin{source: "chart default"}) // not NIC's: no NIC values name it
	for _, ref := range []string{broken, matched, mismatch, unchecked} {
		inv.add(ref, origin{source: "values/" + ref, nicManifest: true})
	}
	r := cannedResolver{
		chartTag:  {digest: digest("d"), multiArch: true, platforms: []platformDigest{amd64, arm64}},
		broken:    {err: errors.New("registry said no")},
		matched:   {digest: digest("a"), tagNow: digest("a"), platforms: []platformDigest{amd64}},
		mismatch:  {digest: digest("b"), tagNow: digest("e"), tagMismatch: "tag \"1.0\" resolves to x, but the pin records y"},
		unchecked: {digest: digest("c"), tagCheckErr: "could not look up tag \"1.0\""},
	}

	var buf bytes.Buffer
	ok, err := inv.writeInventory(context.Background(), &buf, r, func(ref string) string { return ref }, platforms)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("ok = true, want false: one image could not be resolved")
	}
	var out inventoryFile
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("inventory is not valid JSON: %v\n%s", err, buf.String())
	}
	images := map[string]inventoryImage{}
	for _, img := range out.Images {
		images[img.Ref] = img
	}
	if len(images) != 5 {
		t.Fatalf("got %d images, want 5:\n%s", len(images), buf.String())
	}

	t.Run("tag set by the chart", func(t *testing.T) {
		img := images[chartTag]
		if img.Status != statusTag || img.SetBy != "chart" || !img.Mutable || !img.MultiArch {
			t.Errorf("status = %q, setBy = %q, mutable = %v, multiArch = %v; want tag, chart, true, true",
				img.Status, img.SetBy, img.Mutable, img.MultiArch)
		}
		if want := "docker.io/library/busybox@" + digest("d"); img.ScanRef != want {
			t.Errorf("scanRef = %q, want %q", img.ScanRef, want)
		}
		if img.MissingPlatforms != nil {
			t.Errorf("missingPlatforms = %v, want none", img.MissingPlatforms)
		}
	})

	t.Run("lookup failed: recorded, nothing claimed", func(t *testing.T) {
		img := images[broken]
		if img.Error != "registry said no" {
			t.Errorf("error = %q, want the resolver's error", img.Error)
		}
		if img.ScanRef != "" || img.Digest != "" || img.Platforms != nil {
			t.Errorf("scanRef = %q, digest = %q, platforms = %v: nothing should be recorded for a failed lookup",
				img.ScanRef, img.Digest, img.Platforms)
		}
	})

	t.Run("digest pin, tag agrees", func(t *testing.T) {
		img := images[matched]
		if img.Status != statusDigest || img.SetBy != "nic" || img.Mutable {
			t.Errorf("status = %q, setBy = %q, mutable = %v; want digest, nic, false", img.Status, img.SetBy, img.Mutable)
		}
		if want := "ghcr.io/org/matched@" + digest("a"); img.ScanRef != want {
			t.Errorf("scanRef = %q, want %q", img.ScanRef, want)
		}
		if img.TagMismatch != "" || img.TagCheckError != "" {
			t.Errorf("tagMismatch = %q, tagCheckError = %q, want both empty", img.TagMismatch, img.TagCheckError)
		}
		if !reflect.DeepEqual(img.MissingPlatforms, []string{"linux/arm64"}) {
			t.Errorf("missingPlatforms = %v, want [linux/arm64]", img.MissingPlatforms)
		}
	})

	t.Run("digest pin, tag disagrees", func(t *testing.T) {
		img := images[mismatch]
		if img.TagMismatch != r[mismatch].tagMismatch {
			t.Errorf("tagMismatch = %q, want %q", img.TagMismatch, r[mismatch].tagMismatch)
		}
		if img.Error != "" || img.ScanRef == "" {
			t.Errorf("error = %q, scanRef = %q: a mismatch is recorded, not a lookup failure", img.Error, img.ScanRef)
		}
	})

	t.Run("digest pin, tag not checked", func(t *testing.T) {
		img := images[unchecked]
		if img.TagCheckError != r[unchecked].tagCheckErr || img.TagMismatch != "" {
			t.Errorf("tagCheckError = %q, tagMismatch = %q; want the check error and no mismatch",
				img.TagCheckError, img.TagMismatch)
		}
	})

	t.Run("JSON keys", func(t *testing.T) {
		for _, key := range []string{`"tagMismatch"`, `"tagCheckError"`} {
			if !strings.Contains(buf.String(), key) {
				t.Errorf("inventory has no %s key", key)
			}
		}
		if strings.Contains(buf.String(), `"drift"`) {
			t.Error(`inventory still has the old "drift" key`)
		}
	})
}
