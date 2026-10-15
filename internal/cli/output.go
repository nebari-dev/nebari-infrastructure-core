package cli

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/output"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

var (
	// logFormat and verbose back the --log-format and --verbose flags every
	// command inherits.
	logFormat string
	verbose   bool

	// outputFormat is logFormat resolved against the terminal. It is set by
	// configureOutput once flags are parsed.
	outputFormat output.Format
)

// addOutputFlags registers the output flags on the root command.
func addOutputFlags(root *cobra.Command) {
	flags := root.PersistentFlags()
	flags.StringVar(&logFormat, "log-format", string(output.FormatAuto),
		"Output format: auto, pretty, plain, json or github (auto is pretty on a terminal, json otherwise)")
	flags.BoolVarP(&verbose, "verbose", "v", false,
		"Also show informational messages and OpenTofu output under each step")
}

// logFileFlags backs the --log-file and --no-log-file flags of the commands
// that write a log file (deploy and destroy).
type logFileFlags struct {
	path     string
	disabled bool
}

func addLogFileFlags(cmd *cobra.Command, f *logFileFlags) {
	cmd.Flags().StringVar(&f.path, "log-file", "",
		"Write the run's full JSON log to this file (default ~/.nic/logs/<project>/<time>-<command>.jsonl)")
	cmd.Flags().BoolVar(&f.disabled, "no-log-file", false, "Don't write a log file")
}

// configureOutput resolves --log-format and installs the matching slog
// default. Messages the CLI logs directly, including the final error main
// reports, go through it, so they match the format of everything else.
func configureOutput() error {
	f, err := output.ParseFormat(logFormat)
	if err != nil {
		return err
	}
	outputFormat = output.Resolve(f, stderrIsTerminal(), os.Getenv)

	var h slog.Handler
	if outputFormat == output.FormatJSON {
		h = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
	} else {
		h = output.NewTextHandler(os.Stderr, outputFormat)
	}
	slog.SetDefault(slog.New(h))
	return nil
}

func stderrIsTerminal() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}

func terminalWidth() int {
	if w, _, err := term.GetSize(int(os.Stderr.Fd())); err == nil {
		return w
	}
	return 0
}

// newRenderer returns the renderer for the resolved, non-JSON format.
func newRenderer(dryRun bool) *output.Renderer {
	opts := output.Options{Verbose: verbose, DryRun: dryRun, Width: terminalWidth()}
	switch outputFormat {
	case output.FormatPretty:
		opts.Color = true
		opts.Live = stderrIsTerminal()
		return output.NewPretty(os.Stderr, opts)
	case output.FormatGitHub:
		return output.NewGitHub(os.Stderr, opts)
	default:
		return output.NewPlain(os.Stderr, opts)
	}
}

// writeGuidance prints guidance sections to stdout. They follow the resolved
// format, except that pretty panels need stdout itself to be a terminal: when
// it is redirected (nic deploy > notes.txt) they are written as plain text.
func writeGuidance(sections []output.Section) {
	f := outputFormat
	if f == output.FormatPretty && !term.IsTerminal(int(os.Stdout.Fd())) {
		f = output.FormatPlain
	}
	output.WriteSections(os.Stdout, f, sections, output.Options{Color: f == output.FormatPretty, Width: terminalWidth()})
}

// commandOutput routes a deploy or destroy run's status updates to the
// terminal, in the resolved format, and to the run's log file.
type commandOutput struct {
	renderer *output.Renderer
	cleanup  status.CleanupFunc
	logFile  *os.File
	fileLog  *slog.Logger
	once     sync.Once
}

// startCommandOutput attaches a status channel to ctx and starts rendering
// it. dryRun shows the run's informational updates, which are a dry run's
// result. Call finish once the command's work returns.
func startCommandOutput(ctx context.Context, command string, cfg *config.NebariConfig, h output.Header, lf logFileFlags, dryRun bool) (context.Context, *commandOutput) {
	o := &commandOutput{}
	var logPath string
	if !lf.disabled {
		logPath = o.openLogFile(command, cfg.ProjectName, lf.path)
		h.LogPath = displayPath(logPath)
	}

	var handlers []status.Handler
	if outputFormat == output.FormatJSON {
		if logPath != "" {
			slog.Info("Writing log file", "path", logPath)
		}
		handlers = append(handlers, nic.SlogHandler(slog.Default()))
	} else {
		r := newRenderer(dryRun)
		r.Begin(h)
		o.renderer = r
		handlers = append(handlers, r.Handle)
	}
	if o.fileLog != nil {
		handlers = append(handlers, nic.SlogHandler(o.fileLog))
	}

	ctx, o.cleanup = status.StartHandler(ctx, func(u status.Update) {
		for _, handle := range handlers {
			handle(u)
		}
	})
	return ctx, o
}

// openLogFile opens the run's log file and returns its path, or "" when it
// can't be written. A missing log file never stops a command.
func (o *commandOutput) openLogFile(command, project, path string) string {
	if path == "" {
		// The config isn't validated yet, and the default path is built from
		// project_name: skip the log rather than write outside ~/.nic/logs.
		// Validation reports the bad name next.
		if !config.IsSafeProjectName(project) {
			return ""
		}
		home, err := os.UserHomeDir()
		if err != nil {
			slog.Warn("Not writing a log file", "error", err)
			return ""
		}
		path = output.LogFilePath(home, project, command, time.Now())
	}
	f, err := output.OpenLogFile(path)
	if err != nil {
		slog.Warn("Not writing a log file", "error", err)
		return ""
	}
	o.logFile = f
	o.fileLog = slog.New(slog.NewJSONHandler(f, nil))
	return path
}

// pause clears the live progress line and keeps it off while the command
// writes to the terminal itself (the destroy confirmation prompt); resume
// lets it back.
func (o *commandOutput) pause() {
	if o.renderer != nil {
		o.renderer.Pause()
	}
}

func (o *commandOutput) resume() {
	if o.renderer != nil {
		o.renderer.Resume()
	}
}

// finish flushes pending updates, prints the result line, and records err in
// the log file before closing it. It does not print err: the command still
// returns it, and main reports it once (#326). Later calls do nothing.
func (o *commandOutput) finish(err error) {
	o.once.Do(func() {
		o.cleanup()
		if o.renderer != nil {
			o.renderer.Finish(err)
		}
		if o.logFile == nil {
			return
		}
		if err != nil {
			o.fileLog.Error("Command execution failed", "error", err)
		} else {
			o.fileLog.Info("Command completed")
		}
		if cerr := o.logFile.Close(); cerr != nil {
			slog.Warn("Failed to close log file", "error", cerr)
		}
	})
}

// displayPath shortens a path under the home directory to ~/..., which is
// how people read and paste it.
func displayPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || path == "" {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}
	return path
}
