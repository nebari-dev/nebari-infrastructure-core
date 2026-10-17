package status

import "context"

// MetadataKeyPhase is the Metadata key carrying the phase name on a phase
// marker Update (see Phase).
const MetadataKeyPhase = "phase"

// ResourcePhase is the Resource of a phase marker Update.
const ResourcePhase = "phase"

// Phase marks the start of a named phase of a long-running operation, such
// as "Infrastructure" or "Argo CD" during a deploy. Every Update sent after it
// belongs to that phase until the next Phase call: there is no explicit end,
// so a phase closes when the next one opens or when the operation returns.
// Handlers that group output by phase detect the marker with PhaseName.
func Phase(ctx context.Context, name string) {
	Send(ctx, NewUpdate(LevelProgress, name).
		WithResource(ResourcePhase).
		WithAction("start").
		WithMetadata(MetadataKeyPhase, name))
}

// PhaseName reports whether u is a phase marker sent by Phase, and if so the
// name of the phase it opens.
func PhaseName(u Update) (string, bool) {
	if u.Resource != ResourcePhase {
		return "", false
	}
	name, ok := u.Metadata[MetadataKeyPhase].(string)
	return name, ok && name != ""
}
