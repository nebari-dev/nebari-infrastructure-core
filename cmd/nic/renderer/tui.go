package renderer

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Bubble Tea messages sent from the Renderer interface methods ---

type tuiPhaseStartMsg struct{ name string }
type tuiPhaseEndMsg struct {
	status  PhaseStatus
	elapsed time.Duration
}
type tuiStepEndMsg struct {
	status  StepStatus
	elapsed time.Duration
	detail  string
}
type tuiDetailMsg struct{ line string }
type tuiInfoMsg struct{ message string }
type tuiWarnMsg struct{ message string }
type tuiErrorMsg struct {
	err  string
	hint string
}
type tuiSummaryMsg struct{ items []SummaryItem }
type tuiQuitMsg struct{}
type tickMsg time.Time

// --- Phase tracking ---

type tuiPhaseStatus int

const (
	phasePending tuiPhaseStatus = iota
	phaseRunning
	phaseDone
	phaseFailed
)

type tuiPhase struct {
	Name    string
	Status  tuiPhaseStatus
	Elapsed time.Duration
}

// --- Event ring buffer ---

const maxEvents = 1000

// Event level constants.
const (
	levelInfo   = "info"
	levelOK     = "ok"
	levelWarn   = "warn"
	levelError  = "error"
	levelDetail = "detail"
	levelPhase  = "phase"
)

type tuiEvent struct {
	Time    time.Time
	Level   string // levelInfo, levelOK, levelWarn, levelError, levelDetail, levelPhase
	Message string
}

type eventRing struct {
	buf   []tuiEvent
	start int // index of oldest element
	len   int // number of elements
}

func newEventRing() eventRing {
	return eventRing{buf: make([]tuiEvent, maxEvents)}
}

func (r *eventRing) add(e tuiEvent) {
	idx := (r.start + r.len) % maxEvents
	r.buf[idx] = e
	if r.len < maxEvents {
		r.len++
	} else {
		r.start = (r.start + 1) % maxEvents
	}
}

func (r *eventRing) get(i int) tuiEvent {
	return r.buf[(r.start+i)%maxEvents]
}

func (r *eventRing) size() int {
	return r.len
}

// --- Styles ---

type tuiStyles struct {
	title    lipgloss.Style
	dim      lipgloss.Style
	val      lipgloss.Style
	ok       lipgloss.Style
	warn     lipgloss.Style
	err      lipgloss.Style
	detail   lipgloss.Style
	cyan     lipgloss.Style
	phaseHdr lipgloss.Style
	help     lipgloss.Style
}

func newTuiStyles() tuiStyles {
	return tuiStyles{
		title:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4")),
		dim:      lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		val:      lipgloss.NewStyle().Foreground(lipgloss.Color("7")),
		ok:       lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		warn:     lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		err:      lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
		detail:   lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		cyan:     lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
		phaseHdr: lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Bold(true),
		help:     lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
	}
}

// --- Bubble Tea model ---

type tuiModel struct {
	width  int
	height int
	styles tuiStyles

	phases       []tuiPhase
	events       eventRing
	summaryItems []SummaryItem

	project  string
	provider string

	startTime time.Time
	spinner   spinner.Model
	warnCount int
	errCount  int
	stepCount int
	lastStep  string // most recent step detail (shown in status)

	scrollOffset int
	autoScroll   bool
	done         bool
}

func newTuiModel() tuiModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	return tuiModel{
		autoScroll: true,
		startTime:  time.Now(),
		spinner:    s,
		styles:     newTuiStyles(),
		events:     newEventRing(),
	}
}

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, tickEvery())
}

func tickEvery() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		if !m.done {
			cmds = append(cmds, tickEvery())
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if !m.done {
			cmds = append(cmds, cmd)
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			if m.done {
				return m, tea.Quit
			}
		case "ctrl+c":
			// Always allow ctrl+c — Bubble Tea will restore terminal,
			// then the OS delivers SIGINT to the process.
			return m, tea.Quit
		case "up", "k":
			if m.scrollOffset > 0 {
				m.scrollOffset--
				m.autoScroll = false
			}
		case "down", "j":
			m.scrollOffset++
			maxScroll := m.maxScroll()
			if m.scrollOffset >= maxScroll {
				m.scrollOffset = maxScroll
				m.autoScroll = true
			}
		case "G":
			m.scrollOffset = m.maxScroll()
			m.autoScroll = true
		case "g":
			m.scrollOffset = 0
			m.autoScroll = false
		}

	case tuiPhaseStartMsg:
		for i := range m.phases {
			if m.phases[i].Status == phaseRunning {
				m.phases[i].Status = phaseDone
			}
		}
		m.phases = append(m.phases, tuiPhase{Name: msg.name, Status: phaseRunning})
		m.addEvent(levelPhase, msg.name)

	case tuiPhaseEndMsg:
		for i := range m.phases {
			if m.phases[i].Status == phaseRunning {
				if msg.status == PhaseFailed {
					m.phases[i].Status = phaseFailed
				} else {
					m.phases[i].Status = phaseDone
				}
				m.phases[i].Elapsed = msg.elapsed
			}
		}

	case tuiStepEndMsg:
		m.stepCount++
		m.lastStep = msg.detail

		level := levelOK
		switch msg.status {
		case StepFailed:
			level = levelError
		case StepWarning:
			level = levelWarn
		case StepSkipped:
			level = levelInfo
		}
		durStr := ""
		if msg.elapsed > 0 {
			durStr = " " + formatDur(msg.elapsed)
		}
		m.addEvent(level, msg.detail+durStr)

	case tuiDetailMsg:
		m.addEvent(levelDetail, msg.line)

	case tuiInfoMsg:
		// Extract project/provider from structured "Deploying X (Y)" message
		if m.project == "" {
			if rest, ok := strings.CutPrefix(msg.message, "Deploying "); ok {
				if idx := strings.Index(rest, " ("); idx > 0 {
					m.project = rest[:idx]
					prov := strings.TrimRight(rest[idx+2:], ")")
					prov = strings.TrimSuffix(prov, " (dry-run")
					m.provider = prov
				}
			}
		}
		m.addEvent(levelInfo, msg.message)

	case tuiWarnMsg:
		m.warnCount++
		m.addEvent(levelWarn, msg.message)

	case tuiErrorMsg:
		m.errCount++
		errMsg := msg.err
		if msg.hint != "" {
			errMsg += " — " + msg.hint
		}
		m.addEvent(levelError, errMsg)

	case tuiSummaryMsg:
		m.summaryItems = msg.items
		m.done = true
		for i := range m.phases {
			if m.phases[i].Status == phaseRunning {
				m.phases[i].Status = phaseDone
			}
		}
		m.addEvent(levelOK, "Deployment complete")

	case tuiQuitMsg:
		m.done = true
		return m, tea.Quit
	}

	return m, tea.Batch(cmds...)
}

func (m *tuiModel) addEvent(level, message string) {
	m.events.add(tuiEvent{
		Time:    time.Now(),
		Level:   level,
		Message: message,
	})
	if m.autoScroll {
		m.scrollOffset = m.maxScroll()
	}
}

func (m tuiModel) maxScroll() int {
	visible := m.activityVisibleLines()
	max := m.events.size() - visible
	if max < 0 {
		return 0
	}
	return max
}

func (m tuiModel) activityVisibleLines() int {
	h := m.height - 6 // title, blank, help bar, borders
	if h < 1 {
		h = 1
	}
	return h
}

func (m tuiModel) leftWidth() int {
	// Compact left panel — just phases + status, no nested steps
	w := 30
	if m.width > 140 {
		w = 36
	} else if m.width > 100 {
		w = 32
	}
	maxW := m.width / 3 // never exceed 1/3
	if w > maxW {
		w = maxW
	}
	if w < 24 {
		w = 24
	}
	return w
}

// --- View ---

func (m tuiModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	leftW := m.leftWidth()
	rightW := m.width - leftW - 3
	if rightW < 20 {
		rightW = 20
	}
	contentH := m.height - 2

	bc := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	top := bc.Render("╭" + strings.Repeat("─", leftW) + "┬" + strings.Repeat("─", rightW) + "╮")
	bot := bc.Render("╰" + strings.Repeat("─", leftW) + "┴" + strings.Repeat("─", rightW) + "╯")

	leftLines := m.leftContentLines(leftW)
	rightLines := m.rightContentLines(rightW)

	for len(leftLines) < contentH {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < contentH {
		rightLines = append(rightLines, "")
	}

	vbar := bc.Render("│")
	var rows []string
	for i := 0; i < contentH; i++ {
		left := padToVisibleWidth(leftLines[i], leftW)
		right := padToVisibleWidth(rightLines[i], rightW)
		rows = append(rows, vbar+left+vbar+right+vbar)
	}

	return top + "\n" + strings.Join(rows, "\n") + "\n" + bot
}

func padToVisibleWidth(s string, w int) string {
	vis := lipgloss.Width(s)
	if vis >= w {
		return s
	}
	return s + strings.Repeat(" ", w-vis)
}

// --- Left panel: compact phase list + status ---

func (m tuiModel) leftContentLines(w int) []string {
	s := m.styles
	var lines []string

	// Title
	if m.project != "" {
		lines = append(lines, " "+s.title.Bold(true).Render(m.project))
	} else {
		lines = append(lines, " "+s.title.Bold(true).Render("NIC Deploy"))
	}
	if m.provider != "" {
		lines = append(lines, " "+s.dim.Render(m.provider))
	}
	lines = append(lines, "")

	// Phase list — compact, one line per phase
	for _, phase := range m.phases {
		icon, color := m.phaseDisplay(phase)
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(color))

		name := phase.Name
		maxName := w - 4
		if maxName > 0 && len(name) > maxName {
			name = name[:maxName-1] + "…"
		}

		dur := ""
		if phase.Status == phaseDone && phase.Elapsed > 0 {
			dur = " " + s.dim.Render(formatDur(phase.Elapsed))
		}

		lines = append(lines, " "+style.Render(icon+" "+name)+dur)
	}

	// Divider
	lines = append(lines, "")
	lines = append(lines, " "+s.dim.Render(strings.Repeat("─", w-2)))
	lines = append(lines, "")

	// Status
	elapsed := time.Since(m.startTime)
	lines = append(lines, " "+s.dim.Render("Elapsed ")+s.val.Render(formatElapsed(elapsed)))
	lines = append(lines, " "+s.dim.Render("Steps   ")+s.val.Render(fmt.Sprintf("%d", m.stepCount)))

	if m.warnCount > 0 {
		lines = append(lines, " "+s.warn.Render(fmt.Sprintf("Warns   %d", m.warnCount)))
	}
	if m.errCount > 0 {
		lines = append(lines, " "+s.err.Render(fmt.Sprintf("Errors  %d", m.errCount)))
	}

	// Current step (what's happening right now)
	if m.lastStep != "" && !m.done {
		lines = append(lines, "")
		label := s.dim.Render("Current:")
		step := m.lastStep
		maxStep := w - 4
		if maxStep > 0 && len(step) > maxStep {
			step = step[:maxStep-1] + "…"
		}
		lines = append(lines, " "+label)
		lines = append(lines, " "+s.val.Render(step))
	}

	// Done state
	if m.done {
		lines = append(lines, "")
		lines = append(lines, " "+s.ok.Bold(true).Render("✓ Complete"))
	}

	// Summary outputs
	if len(m.summaryItems) > 0 {
		lines = append(lines, "")
		lines = append(lines, " "+s.dim.Render(strings.Repeat("─", w-2)))
		lines = append(lines, "")
		lines = append(lines, " "+s.title.Render("Outputs"))

		maxLabel := 0
		for _, item := range m.summaryItems {
			if len(item.Label) > maxLabel {
				maxLabel = len(item.Label)
			}
		}
		for _, item := range m.summaryItems {
			pad := maxLabel - len(item.Label) + 1
			val := item.Value
			maxVal := w - maxLabel - 4
			if maxVal > 0 && len(val) > maxVal {
				val = val[:maxVal-1] + "…"
			}
			lines = append(lines, " "+s.dim.Render(item.Label)+strings.Repeat(" ", pad)+s.cyan.Render(val))
		}
	}

	return lines
}

func (m tuiModel) phaseDisplay(phase tuiPhase) (string, string) {
	switch phase.Status {
	case phaseRunning:
		return m.spinner.View(), "6"
	case phaseDone:
		return "✓", "2"
	case phaseFailed:
		return "✗", "1"
	default:
		return "○", "8"
	}
}

// --- Right panel: activity stream with all detail ---

func (m tuiModel) rightContentLines(w int) []string {
	s := m.styles
	visibleLines := m.activityVisibleLines()
	total := m.events.size()

	// Build visible event lines
	start := m.scrollOffset
	if start > total {
		start = total
	}
	end := start + visibleLines
	if end > total {
		end = total
	}

	var visible []string
	for i := start; i < end; i++ {
		e := m.events.get(i)
		visible = append(visible, m.renderEvent(e, w))
	}
	for len(visible) < visibleLines {
		visible = append(visible, "")
	}

	// Header
	title := s.title.Bold(true).Render("Activity")
	var header string
	if total > visibleLines {
		if m.autoScroll {
			header = " " + title + "  " + s.dim.Render("↓ auto-scroll")
		} else {
			header = " " + title + "  " + s.warn.Render(
				fmt.Sprintf("line %d/%d", start+1, total))
		}
	} else {
		header = " " + title
	}

	// Help bar
	var helpText string
	if m.done {
		helpText = s.help.Render(" q quit")
	} else {
		helpText = s.help.Render(" j/k scroll · G end · ctrl+c cancel")
	}

	var lines []string
	lines = append(lines, header)
	lines = append(lines, "")
	lines = append(lines, visible...)
	lines = append(lines, helpText)
	return lines
}

func (m tuiModel) renderEvent(e tuiEvent, w int) string {
	s := m.styles
	ts := s.dim.Render(e.Time.Format("15:04:05"))
	maxMsg := w - 14

	switch e.Level {
	case levelPhase:
		return " " + s.phaseHdr.Render("── "+e.Message+" ──")
	case levelOK:
		return " " + ts + " " + s.ok.Render("✓ "+truncate(e.Message, maxMsg-2))
	case levelWarn:
		return " " + ts + " " + s.warn.Render("⚠ "+truncate(e.Message, maxMsg-2))
	case levelError:
		return " " + ts + " " + s.err.Render("✗ "+truncate(e.Message, maxMsg-2))
	case levelDetail:
		return " " + ts + " " + s.detail.Render("  "+truncate(e.Message, maxMsg-2))
	default:
		return " " + ts + " " + s.val.Render(truncate(e.Message, maxMsg))
	}
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// --- Helper functions ---

func formatDur(d time.Duration) string {
	if d == 0 {
		return ""
	}
	if d >= time.Minute {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", mins, secs)
	}
	if d < time.Second {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func formatElapsed(d time.Duration) string {
	d = d.Round(time.Second)
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d", m, s)
}

// ============================================================
// TUI Renderer — implements the Renderer interface
// ============================================================

// TUI is a full-screen split-panel renderer powered by Bubble Tea.
// Left panel: compact progress phases + status counters.
// Right panel: timestamped activity stream with scrolling.
type TUI struct {
	program *tea.Program
	mu      sync.Mutex
	done    bool
	ready   chan struct{} // closed when program.Run() has started
}

// NewTUI creates and starts the TUI renderer.
func NewTUI() *TUI {
	model := newTuiModel()
	p := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	t := &TUI{
		program: p,
		ready:   make(chan struct{}),
	}

	go func() {
		close(t.ready) // signal that Run is about to start
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		}
		t.mu.Lock()
		t.done = true
		t.mu.Unlock()
	}()

	// Wait for the goroutine to be scheduled and Run() to begin
	<-t.ready

	return t
}

func (t *TUI) send(msg tea.Msg) {
	t.mu.Lock()
	done := t.done
	t.mu.Unlock()
	if !done {
		t.program.Send(msg)
	}
}

func (t *TUI) StartPhase(name string) {
	t.send(tuiPhaseStartMsg{name: name})
}

func (t *TUI) EndPhase(status PhaseStatus, elapsed time.Duration) {
	t.send(tuiPhaseEndMsg{status: status, elapsed: elapsed})
}

func (t *TUI) StartStep(_ string) {
	// Rendered on EndStep
}

func (t *TUI) EndStep(status StepStatus, elapsed time.Duration, detail string) {
	t.send(tuiStepEndMsg{status: status, elapsed: elapsed, detail: detail})
}

func (t *TUI) Detail(line string) {
	t.send(tuiDetailMsg{line: line})
}

func (t *TUI) Summary(items []SummaryItem) {
	t.send(tuiSummaryMsg{items: items})
	// Block until user presses q (TUI shows summary in left panel)
	t.program.Wait()

	// Print summary to stdout so it persists in scrollback after alt-screen exits
	if len(items) > 0 {
		fmt.Println()
		fmt.Println("  ✓ Deployment complete")
		fmt.Println()
		fmt.Println("  Outputs:")
		maxLabel := 0
		for _, item := range items {
			if len(item.Label) > maxLabel {
				maxLabel = len(item.Label)
			}
		}
		for _, item := range items {
			pad := maxLabel - len(item.Label)
			fmt.Printf("     %s:%s  %s\n", item.Label, strings.Repeat(" ", pad), item.Value)
		}
		fmt.Println()
	}
}

func (t *TUI) Error(err error, hint string) {
	t.send(tuiErrorMsg{err: err.Error(), hint: hint})
}

func (t *TUI) Warn(message string) {
	t.send(tuiWarnMsg{message: message})
}

func (t *TUI) Info(message string) {
	t.send(tuiInfoMsg{message: message})
}

func (t *TUI) Version(version, commit, tofuVersion string, clusterProviders, dnsProviders []string) {
	t.Quit()
	fmt.Printf("Nebari Infrastructure Core (NIC)\n")
	fmt.Printf("  Version:    %s\n", version)
	fmt.Printf("  Commit:     %s\n", commit)
	fmt.Printf("  OpenTofu:   %s\n", tofuVersion)
	fmt.Printf("  Providers:  %s\n", strings.Join(clusterProviders, ", "))
	fmt.Printf("  DNS:        %s\n", strings.Join(dnsProviders, ", "))
}

func (t *TUI) Confirm(message string, details map[string]string, expected string) (bool, error) {
	// Temporarily exit the TUI for stdio confirmation.
	// Bubble Tea restores the terminal on quit, so we can use stdin/stdout.
	t.program.Send(tuiQuitMsg{})
	t.program.Wait()

	fmt.Println()
	fmt.Printf(" ⚠  %s\n\n", message)
	for k, v := range details {
		fmt.Printf("     %s: %s\n", k, v)
	}
	fmt.Println()
	fmt.Println("  This will permanently delete all resources and data.")
	fmt.Println("  This action cannot be undone.")
	fmt.Printf("\n  Type '%s' to confirm: ", expected)

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read user input: %w", err)
	}
	fmt.Println()
	confirmed := strings.TrimSpace(response) == expected

	// Restart the TUI for the rest of the operation
	t.restart()

	return confirmed, nil
}

func (t *TUI) DetailWriter() io.Writer {
	return &lineWriter{r: t}
}

// Quit stops the TUI program and restores the terminal.
// Safe to call multiple times.
func (t *TUI) Quit() {
	t.mu.Lock()
	done := t.done
	t.mu.Unlock()
	if done {
		return
	}
	t.program.Send(tuiQuitMsg{})
	t.program.Wait()
}

// restart creates a new Bubble Tea program, preserving existing model state.
func (t *TUI) restart() {
	t.mu.Lock()
	t.done = false
	t.mu.Unlock()

	model := newTuiModel()
	p := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	t.program = p
	t.ready = make(chan struct{})

	go func() {
		close(t.ready)
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		}
		t.mu.Lock()
		t.done = true
		t.mu.Unlock()
	}()

	<-t.ready
}
