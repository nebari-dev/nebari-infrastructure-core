// Command imagepin inventories the container images NIC deploys, as a local
// review aid for pinning them by digest.
//
// It renders NIC's own GitOps output (pkg/argocd.WriteAllToGit), then renders
// every vendor Helm chart those Argo CD Applications reference, at the pinned
// chart version and with NIC's values, and collects every image reference in
// the result.
//
// This is a developer tool, like cmd/docgen. The nic binary never imports it,
// and it never changes a file in the repository.
//
// Adapted from internal/imageinventory in nebari-dev/nebari-infrastructure-core
// PR #747 (closed, unmerged).
//
// Usage:
//
//	go run ./cmd/imagepin list      # what NIC deploys, and who set each reference
//	go run ./cmd/imagepin digests   # plus what each reference resolves to today
//	go run ./cmd/imagepin plan      # the validated changes to pin each by digest
//	go run ./cmd/imagepin inventory -o images.json   # machine-readable list for scanning
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"text/tabwriter"
)

func main() {
	commands := map[string]bool{"list": true, "digests": true, "plan": true, "inventory": true}
	if len(os.Args) < 2 || !commands[os.Args[1]] {
		fmt.Fprintln(os.Stderr, "usage: imagepin list|digests|plan|inventory [-kube-version v1.34.0] [-o file]")
		os.Exit(2)
	}
	cmd := os.Args[1]

	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	kubeVersion := fs.String("kube-version", "v1.34.0", "Kubernetes version charts are rendered for")
	outPath := fs.String("o", "", "inventory: write the JSON here instead of stdout")
	_ = fs.Parse(os.Args[2:])

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	inv := newInventory()
	r, err := run(ctx, inv, *kubeVersion)
	if err != nil {
		fmt.Fprintln(os.Stderr, "imagepin:", err)
		os.Exit(1)
	}

	ok := true
	switch cmd {
	case "list":
		inv.printList(os.Stdout)
		inv.printFooter(os.Stdout)
	case "digests":
		ok = inv.printDigests(ctx, os.Stdout, newResolver(), r.withRecordedTag)
		inv.printFooter(os.Stdout)
	case "plan":
		p := buildPlan(ctx, r, newResolver())
		ok = p.print(os.Stdout)
		inv.printFooter(os.Stdout)
		p.printUnchanged(os.Stdout) // no-op items last
	case "inventory":
		// JSON only on the output, so a pipeline can parse it; messages go to stderr.
		w := os.Stdout
		if *outPath != "" {
			f, err := os.Create(*outPath)
			if err != nil {
				fmt.Fprintln(os.Stderr, "imagepin:", err)
				r.close()
				os.Exit(1)
			}
			w = f
		}
		var err error
		ok, err = inv.writeInventory(ctx, w, newResolver(), r.withRecordedTag)
		if w != os.Stdout {
			// Closed here, not deferred: os.Exit below would skip a defer.
			if cerr := w.Close(); err == nil {
				err = cerr
			}
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "imagepin: write inventory:", err)
			ok = false
		}
		if !ok {
			fmt.Fprintln(os.Stderr, "imagepin: one or more images could not be resolved; see \"error\" in the inventory")
		}
	}
	r.close()
	if !ok {
		os.Exit(1)
	}
}

// pinStatus classifies an image reference as rendered.
func pinStatus(ref string) string {
	if strings.Contains(ref, "@sha256:") {
		return "digest"
	}
	// A tag is a ':' after the last '/'. A ':' before it is a registry port.
	name := ref[strings.LastIndex(ref, "/")+1:]
	switch {
	case strings.HasSuffix(name, ":latest"):
		return "latest"
	case strings.Contains(name, ":"):
		return "tag"
	default:
		return "untagged" // the runtime will pull :latest
	}
}

func setBy(img *image) string {
	if img.setByNIC {
		return "NIC"
	}
	return "chart"
}

func (i *inventory) sortedRefs() []string {
	refs := make([]string, 0, len(i.images))
	for ref := range i.images {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

func (i *inventory) printList(w io.Writer) {
	counts := map[string]int{}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tSET BY\tIMAGE\tFOUND IN")
	for _, ref := range i.sortedRefs() {
		img := i.images[ref]
		status := pinStatus(ref)
		counts[status]++
		sort.Strings(img.sources)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", status, setBy(img), ref, img.sources[0])
		for _, s := range img.sources[1:] {
			fmt.Fprintf(tw, "\t\t\t%s\n", s)
		}
	}
	_ = tw.Flush()
	fmt.Fprintf(w, "\n%d images: %d digest, %d tag only, %d latest, %d untagged\n",
		len(i.images), counts["digest"], counts["tag"], counts["latest"], counts["untagged"])
}

// printDigests resolves every reference and reports, without changing
// anything. It returns false if any lookup failed or a pinned digest is gone.
//
// lookupRef maps a rendered reference to the one to resolve; it restores a
// tag the chart dropped, so digest pins still get their drift check.
func (i *inventory) printDigests(ctx context.Context, w io.Writer, r *resolver, lookupRef func(string) string) bool {
	type row struct {
		ref string
		img *image
		res resolution
	}
	var rows []row
	for _, ref := range i.sortedRefs() {
		rows = append(rows, row{ref, i.images[ref], r.resolve(ctx, lookupRef(ref))})
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tSET BY\tIMAGE\tDIGEST TODAY\tPLATFORMS")
	for _, x := range rows {
		digest, platforms := "-", "-"
		if x.res.err == nil {
			digest = x.res.digest
			platforms = "single"
			if x.res.multiArch {
				platforms = "multi"
			}
		} else {
			digest = "ERROR"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", pinStatus(x.ref), setBy(x.img), x.ref, digest, platforms)
	}
	_ = tw.Flush()

	// The sections below are the review points. Each lists the references
	// whose digest can be scanned today but can't be guaranteed tomorrow.
	section := func(title, why string, match func(row) bool, detail func(row) string) {
		var hits []row
		for _, x := range rows {
			if match(x) {
				hits = append(hits, x)
			}
		}
		if len(hits) == 0 {
			return
		}
		fmt.Fprintf(w, "\n%s (%d)\n  %s\n", title, len(hits), why)
		for _, x := range hits {
			line := "  - " + x.ref
			if d := detail(x); d != "" {
				line += "\n      " + d
			}
			fmt.Fprintln(w, line)
		}
	}
	firstSource := func(x row) string { return "found in " + x.img.sources[0] }
	floating := func(x row) bool { s := pinStatus(x.ref); return s == "latest" || s == "untagged" }

	section("Errors", "Lookup failed. Nothing about these images was verified.",
		func(x row) bool { return x.res.err != nil },
		func(x row) string { return x.res.err.Error() })
	section("Tag moved upstream", "The pinned digest still pulls the reviewed image, but the tag next to it now names something else.",
		func(x row) bool { return x.res.drift != "" },
		func(x row) string { return x.res.drift })
	section("Floating: latest or no tag",
		"Today's digest can be scanned, but the next pull can return a different image. Pinning by digest needs a fixed tag to track first.",
		func(x row) bool { return x.res.err == nil && floating(x) }, firstSource)
	section("Not set by NIC",
		"The chart chose this reference (a default or hard-coded value), so it can change with a chart bump and isn't under NIC's review.",
		func(x row) bool { return !x.img.setByNIC }, firstSource)
	section("Tag pinned by NIC, not by digest",
		"NIC controls these tags, but a tag can be re-pushed. These are the candidates for converting to a digest.",
		func(x row) bool { return x.res.err == nil && x.img.setByNIC && pinStatus(x.ref) == "tag" },
		func(x row) string { return "today: " + x.res.digest })

	fmt.Fprintln(w, "\nA digest above is what the registry returns today. For any reference not pinned by digest,")
	fmt.Fprintln(w, "a later pull can differ; checking that at deployment time is out of scope here.")

	for _, x := range rows {
		if x.res.err != nil {
			return false
		}
	}
	return true
}

func (i *inventory) printFooter(w io.Writer) {
	if len(i.charts) > 0 {
		fmt.Fprintln(w, "\nCharts rendered:")
		for _, c := range i.charts {
			fmt.Fprintln(w, "  "+c)
		}
	}
	if len(i.notes) > 0 {
		fmt.Fprintln(w, "\nNotes (not covered by this listing):")
		for _, n := range i.notes {
			fmt.Fprintln(w, "  - "+n)
		}
	}
}
