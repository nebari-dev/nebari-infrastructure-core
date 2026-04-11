package main

import (
	"fmt"

	"github.com/nebari-dev/nebari-infrastructure-core/cmd/nic/renderer"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// statusRendererHandler returns a status.Handler that routes updates to the given Renderer.
func statusRendererHandler(r renderer.Renderer) status.Handler {
	return func(update status.Update) {
		detail := update.Message
		if update.Resource != "" {
			detail = fmt.Sprintf("%s: %s", update.Resource, update.Message)
		}

		switch update.Level {
		case status.LevelProgress:
			r.StartStep(detail)
		case status.LevelSuccess:
			r.EndStep(renderer.StepOK, 0, detail)
		case status.LevelInfo:
			r.Info(detail)
		case status.LevelWarning:
			r.Warn(detail)
		case status.LevelError:
			r.Error(fmt.Errorf("%s", detail), "")
		default:
			r.Info(detail)
		}
	}
}
