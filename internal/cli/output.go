package cli

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"k8s.io/client-go/rest"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/output"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
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

	// logOutput is where the CLI's own log records go (see stderrRouter).
	logOutput = &stderrRouter{w: os.Stderr}
)

// stderrRouter is the writer behind the CLI's slog handler. While deploy or
// destroy draws a live panel it hands records to the renderer, which prints
// them above the panel; written straight to stderr, the panel's next redraw
// would erase them.
type stderrRouter struct {
	mu sync.Mutex
	w  io.Writer
	r  *output.Renderer
}

func (s *stderrRouter) Write(b []byte) (int, error) {
	s.mu.Lock()
	r := s.r
	s.mu.Unlock()
	if r != nil {
		return r.Write(b)
	}
	return s.w.Write(b)
}

// through sends writes to r until it is called again with nil.
func (s *stderrRouter) through(r *output.Renderer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.r = r
}

// apiWarnings logs the warnings the Kubernetes API returns (deprecated
// fields, for instance) through slog, once each, instead of client-go's
// default of printing them through klog straight to stderr.
type apiWarnings struct{ seen sync.Map }

func (w *apiWarnings) HandleWarningHeader(code int, _ string, text string) {
	if code != 299 || text == "" {
		return
	}
	if _, dup := w.seen.LoadOrStore(text, true); !dup {
		slog.Warn("Kubernetes API warning", "warning", text)
	}
}

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
		h = slog.NewJSONHandler(logOutput, &slog.HandlerOptions{Level: slog.LevelInfo})
	} else {
		h = output.NewTextHandler(logOutput, outputFormat, output.TrueColor(os.Getenv))
	}
	slog.SetDefault(slog.New(h))
	rest.SetDefaultWarningHandler(&apiWarnings{})
	return nil
}

func stderrIsTerminal() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// terminalSize is stderr's width and height, or zeros when it isn't a
// terminal (the renderer then picks defaults).
func terminalSize() (width, height int) {
	if w, h, err := term.GetSize(int(os.Stderr.Fd())); err == nil {
		return w, h
	}
	return 0, 0
}

func terminalWidth() int {
	w, _ := terminalSize()
	return w
}

// spinnerInterval is how often the live panel's spinner advances.
const spinnerInterval = 100 * time.Millisecond

// newRenderer returns the renderer for the resolved, non-JSON format.
func newRenderer(dryRun bool) *output.Renderer {
	opts := output.Options{Verbose: verbose, DryRun: dryRun, Width: terminalWidth()}
	switch outputFormat {
	case output.FormatPretty:
		opts.Color = true
		opts.TrueColor = output.TrueColor(os.Getenv)
		opts.Live = stderrIsTerminal()
		_, opts.Height = terminalSize()
		opts.Size = terminalSize
		opts.Tick = spinnerInterval
		return output.NewPretty(os.Stderr, opts)
	case output.FormatGitHub:
		return output.NewGitHub(os.Stderr, opts)
	default:
		return output.NewPlain(os.Stderr, opts)
	}
}

// writeGuidance prints the steps to take after a command to stdout. Pretty
// needs stdout itself to be a terminal: when it is redirected (nic deploy >
// notes.txt) the steps are written as plain text, as in every other format.
func writeGuidance(steps []output.Step) {
	f := outputFormat
	if f == output.FormatPretty && !term.IsTerminal(int(os.Stdout.Fd())) {
		f = output.FormatPlain
	}
	output.WriteNextSteps(os.Stdout, f, steps, output.Options{
		Color:     f == output.FormatPretty,
		TrueColor: output.TrueColor(os.Getenv),
		Width:     terminalWidth(),
	})
}

// commandHeader fills the header fields deploy and destroy share: where the
// command runs, the domain, the config file and NIC's version.
func commandHeader(ctx context.Context, client *nic.Client, cfg *config.NebariConfig, configFile string) output.Header {
	return output.Header{
		Target:     clusterTarget(ctx, client, cfg),
		Domain:     cfg.Domain,
		ConfigFile: configFile,
		Version:    displayVersion(version),
	}
}

// clusterTarget names where a command runs: the cluster provider, then the
// region, location or kubeconfig context from its summary, whichever it has.
func clusterTarget(ctx context.Context, client *nic.Client, cfg *config.NebariConfig) string {
	target := cfg.Cluster.ProviderName()
	summary, err := client.ClusterSummary(ctx, cfg)
	if err != nil {
		return target
	}
	for _, key := range []string{cluster.SummaryKeyRegion, cluster.SummaryKeyLocation} {
		if v := summary[key]; v != "" {
			return target + " \u00b7 " + v
		}
	}
	if v := summary[cluster.SummaryKeyContext]; v != "" {
		return target + " \u00b7 context " + v
	}
	return target
}

// displayVersion is the build version as the header shows it: v0.15.0 for a
// release (goreleaser drops the v), dev for a local build.
func displayVersion(v string) string {
	if v != "" && v[0] >= '0' && v[0] <= '9' {
		return "v" + v
	}
	return v
}

// retryCommand is the command line to run cmd again with the same config:
// the -f flag only when one was given, since auto-discovery finds it again.
func retryCommand(cmd, configFlag string) string {
	if configFlag != "" {
		return cmd + " -f " + configFlag
	}
	return cmd
}

// commandOutput routes a deploy or destroy run's status updates to the
// terminal, in the resolved format, and to the run's log file.
type commandOutput struct {
	renderer *output.Renderer
	cleanup  status.CleanupFunc
	logFile  *os.File
	fileLog  *slog.Logger
}

// startCommandOutput attaches a status channel to ctx and starts rendering
// it. dryRun shows the run's informational updates, which are a dry run's
// result. Call finish exactly once, when the command's work returns.
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
		logOutput.through(r)
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
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
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

// pause clears the live panel and keeps it off while the command
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

// finish flushes pending updates, prints the result line (with link on
// success), and records err in the log file before closing it. It does not
// print err: the command still returns it, and main reports it once (#326).
func (o *commandOutput) finish(err error, link string) {
	o.cleanup()
	if o.renderer != nil {
		o.renderer.Finish(err, link)
		logOutput.through(nil)
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
