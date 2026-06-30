package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DigitalTolk/keel/internal/bootstrap"
)

// focusable identifies a navigable item in the guided form.
type focusable int

const (
	fcHosts focusable = iota
	fcUser
	fcPassword
	fcKeys
	fcAdvanced // the expand/collapse toggle
	fcPort
	fcAdminUser
	fcJump
	fcIdentity
	fcPubkeyFile
	fcSubmit // the "run" button
)

// --- progress messages (sent by the provisioning goroutine) ------------------

// provisionLogMsg is one line of provisioning output. header lines are keel's
// own step labels; non-header lines are raw output streamed from the host.
type provisionLogMsg struct {
	host   string
	line   string
	header bool
}

type provisionDoneMsg struct{ err error }

// waitForActivity reads the next provisioning message off ch.
func waitForActivity(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return provisionDoneMsg{}
		}
		return msg
	}
}

// --- styles ------------------------------------------------------------------
//
// Colors are ANSI palette indices so they follow the terminal's own theme.
// Adaptive {Light,Dark} pairs keep text legible on both backgrounds; the dark
// help color is bright on purpose so the footer stays readable on dark themes.
var (
	tuiAccent  = lipgloss.AdaptiveColor{Light: "4", Dark: "12"}
	tuiText    = lipgloss.AdaptiveColor{Light: "0", Dark: "7"}
	tuiHelpCol = lipgloss.AdaptiveColor{Light: "8", Dark: "7"}
	tuiHint    = lipgloss.AdaptiveColor{Light: "8", Dark: "8"}

	styTitle  = lipgloss.NewStyle().Bold(true).Foreground(tuiAccent)
	styLabel  = lipgloss.NewStyle().Width(12).Foreground(tuiText)
	styHelp   = lipgloss.NewStyle().Foreground(tuiHelpCol)
	styHint   = lipgloss.NewStyle().Foreground(tuiHint)
	styErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styPoint  = lipgloss.NewStyle().Foreground(tuiAccent)
	styAccent = lipgloss.NewStyle().Foreground(tuiAccent)
)

const logWindowHeight = 14

// bootstrapModel is the guided TUI: a single-screen form that, on submit,
// transitions in place to a live, scrolling provisioning log.
type bootstrapModel struct {
	hosts      textinput.Model
	user       textinput.Model
	password   textinput.Model
	keys       textarea.Model
	port       textinput.Model
	adminUser  textinput.Model
	jump       textinput.Model
	identity   textinput.Model
	pubkeyFile textinput.Model

	advanced bool
	order    []focusable
	focus    int
	width    int
	errMsg   string

	canceled bool
	params   bootstrapParams

	// provisioning phase
	provisioning bool
	finished     bool
	runErr       error
	spinner      spinner.Model
	logs         []provisionLogMsg
	ch           <-chan tea.Msg

	// start kicks off provisioning and returns the message channel. Injected so
	// tests can drive the model without a network.
	start func(bootstrapParams) <-chan tea.Msg
}

func newTextField(value, placeholder string, masked bool) textinput.Model {
	ti := textinput.New()
	ti.SetValue(value)
	ti.Placeholder = placeholder
	ti.Prompt = "› "
	ti.PlaceholderStyle = styHint
	if masked {
		ti.EchoMode = textinput.EchoPassword
	}
	return ti
}

// newBootstrapModel builds the model seeded from f. start provisions on submit.
func newBootstrapModel(f bootstrapFields, start func(bootstrapParams) <-chan tea.Msg) bootstrapModel {
	keys := textarea.New()
	keys.SetValue(f.pubkeys)
	keys.Placeholder = "ssh-ed25519 AAAA… user@host (paste one per line)"
	keys.ShowLineNumbers = false
	keys.SetHeight(4)

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	m := bootstrapModel{
		hosts:      newTextField(f.hosts, "1.2.3.4 web1  (IPs, names, or ~/.ssh/config aliases)", false),
		user:       newTextField(f.user, "bofh", false),
		password:   newTextField(f.password, "blank = use SSH key", true),
		keys:       keys,
		port:       newTextField(f.port, "22", false),
		adminUser:  newTextField(f.adminUser, "bofh", false),
		jump:       newTextField(f.jump, "user@host#port  (optional)", false),
		identity:   newTextField(f.identities, "~/.ssh/id_ed25519  (optional)", false),
		pubkeyFile: newTextField(f.pubkeyFile, "/path/to/authorized_keys  (optional)", false),
		spinner:    sp,
		start:      start,
	}
	m.rebuildOrder()
	m.applyFocus()
	return m
}

func (m *bootstrapModel) rebuildOrder() {
	m.order = []focusable{fcHosts, fcUser, fcPassword, fcKeys, fcAdvanced}
	if m.advanced {
		m.order = append(m.order, fcPort, fcAdminUser, fcJump, fcIdentity, fcPubkeyFile)
	}
	m.order = append(m.order, fcSubmit)
	if m.focus >= len(m.order) {
		m.focus = len(m.order) - 1
	}
}

func (m *bootstrapModel) field(fc focusable) *textinput.Model {
	switch fc {
	case fcHosts:
		return &m.hosts
	case fcUser:
		return &m.user
	case fcPassword:
		return &m.password
	case fcPort:
		return &m.port
	case fcAdminUser:
		return &m.adminUser
	case fcJump:
		return &m.jump
	case fcIdentity:
		return &m.identity
	case fcPubkeyFile:
		return &m.pubkeyFile
	}
	return nil
}

// applyFocus blurs every field, then focuses the current one.
func (m *bootstrapModel) applyFocus() {
	for _, fc := range []focusable{fcHosts, fcUser, fcPassword, fcPort, fcAdminUser, fcJump, fcIdentity, fcPubkeyFile} {
		m.field(fc).Blur()
	}
	m.keys.Blur()
	switch m.order[m.focus] {
	case fcKeys:
		m.keys.Focus()
	case fcAdvanced, fcSubmit:
		// nothing to focus
	default:
		m.field(m.order[m.focus]).Focus()
	}
}

func (m *bootstrapModel) focusNext() {
	m.focus = (m.focus + 1) % len(m.order)
	m.applyFocus()
}

func (m *bootstrapModel) focusPrev() {
	m.focus = (m.focus - 1 + len(m.order)) % len(m.order)
	m.applyFocus()
}

func (m *bootstrapModel) toggleAdvanced() {
	m.advanced = !m.advanced
	m.rebuildOrder()
	m.applyFocus()
}

func (m bootstrapModel) toFields() bootstrapFields {
	return bootstrapFields{
		hosts:      m.hosts.Value(),
		user:       m.user.Value(),
		password:   m.password.Value(),
		pubkeys:    m.keys.Value(),
		port:       m.port.Value(),
		adminUser:  m.adminUser.Value(),
		jump:       m.jump.Value(),
		identities: m.identity.Value(),
		pubkeyFile: m.pubkeyFile.Value(),
	}
}

// submit validates and maps the fields; on success it enters the provisioning
// phase and returns the command stream.
func (m *bootstrapModel) submit() tea.Cmd {
	p, err := m.toFields().toParams()
	if err != nil {
		m.errMsg = err.Error()
		return nil
	}
	m.errMsg = ""
	m.params = p
	m.provisioning = true
	m.ch = m.start(p)
	return tea.Batch(waitForActivity(m.ch), m.spinner.Tick)
}

func (m bootstrapModel) Init() tea.Cmd { return textinput.Blink }

func (m bootstrapModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.provisioning {
		return m.updateProvisioning(msg)
	}
	return m.updateForm(msg)
}

func (m bootstrapModel) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.resize()
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			m.canceled = true
			return m, tea.Quit
		case "tab", "down":
			m.focusNext()
			return m, nil
		case "shift+tab", "up":
			m.focusPrev()
			return m, nil
		case "enter":
			switch m.order[m.focus] {
			case fcAdvanced:
				m.toggleAdvanced()
				return m, nil
			case fcSubmit:
				return m, m.submit()
			case fcKeys:
				// fall through to the textarea (insert a newline)
			default:
				m.focusNext()
				return m, nil
			}
		case " ":
			switch m.order[m.focus] {
			case fcAdvanced:
				m.toggleAdvanced()
				return m, nil
			case fcSubmit:
				return m, m.submit()
			}
		}
	}
	return m.routeToField(msg)
}

// routeToField forwards the message to the currently focused editable field.
func (m bootstrapModel) routeToField(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.order[m.focus] {
	case fcKeys:
		m.keys, cmd = m.keys.Update(msg)
	case fcAdvanced, fcSubmit:
		// not editable
	default:
		f := m.field(m.order[m.focus])
		*f, cmd = f.Update(msg)
	}
	return m, cmd
}

func (m bootstrapModel) updateProvisioning(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case provisionLogMsg:
		m.logs = append(m.logs, msg)
		return m, waitForActivity(m.ch)
	case provisionDoneMsg:
		m.finished = true
		m.runErr = msg.err
		return m, nil
	case spinner.TickMsg:
		if m.finished {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		if m.finished {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *bootstrapModel) resize() {
	w := max(m.width-16, 20)
	for _, fc := range []focusable{fcHosts, fcUser, fcPassword, fcPort, fcAdminUser, fcJump, fcIdentity, fcPubkeyFile} {
		m.field(fc).Width = w
	}
	m.keys.SetWidth(max(m.width-4, 24))
}

// renderLog renders one log entry, truncated to width so a long line (apt
// output, a base64 blob) never wraps and grows the fixed-height window.
func renderLog(msg provisionLogMsg, width int) string {
	if msg.header {
		prefix := "▸ " + msg.host + "  "
		line := truncateStr(msg.line, width-lipgloss.Width(prefix))
		return styPoint.Render("▸ ") + styAccent.Render(msg.host) + "  " + line
	}
	return styHint.Render("    " + truncateStr(msg.line, width-4))
}

// truncateStr clamps s to at most maxw runes, adding an ellipsis when cut.
func truncateStr(s string, maxw int) string {
	if maxw < 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxw {
		return s
	}
	if maxw == 1 {
		return "…"
	}
	return string(r[:maxw-1]) + "…"
}

func (m bootstrapModel) View() string {
	if m.provisioning {
		return m.viewProvisioning()
	}
	return m.viewForm()
}

func (m bootstrapModel) pointer(fc focusable) string {
	if m.order[m.focus] == fc {
		return styPoint.Render("▌ ")
	}
	return "  "
}

// contentWidth is the width used for dividers and the log window.
func (m bootstrapModel) contentWidth() int {
	w := m.width - 6
	if w < 40 {
		w = 40
	}
	if w > 110 {
		w = 110
	}
	return w
}

func (m bootstrapModel) divider() string {
	return styHint.Render(strings.Repeat("─", m.contentWidth()))
}

func (m bootstrapModel) viewForm() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n %s  %s\n", styTitle.Render("keel bootstrap"), styHint.Render("· prepare hosts for Ansible"))
	fmt.Fprintf(&b, "%s\n", m.divider())

	row := func(fc focusable, label, view string) {
		fmt.Fprintf(&b, "%s%s%s\n", m.pointer(fc), styLabel.Render(label), view)
	}
	row(fcHosts, "Hosts", m.hosts.View())
	row(fcUser, "SSH user", m.user.View())
	row(fcPassword, "Password", m.password.View())
	fmt.Fprintf(&b, "%s%s\n%s\n", m.pointer(fcKeys), styLabel.Render("Public keys"), m.keys.View())

	// Advanced toggle and the action button share one section (no divider
	// between them) and line up at the same column.
	fmt.Fprintf(&b, "%s\n", m.divider())
	caret := "▸"
	if m.advanced {
		caret = "▾"
	}
	fmt.Fprintf(&b, "%s%s %s\n", m.pointer(fcAdvanced), styPoint.Render(caret+" Advanced options"), styHint.Render("(space)"))
	if m.advanced {
		row(fcPort, "Port", m.port.View())
		row(fcAdminUser, "Admin user", m.adminUser.View())
		row(fcJump, "Jump host", m.jump.View())
		row(fcIdentity, "Identity", m.identity.View())
		row(fcPubkeyFile, "Key file", m.pubkeyFile.View())
	}
	fmt.Fprintf(&b, "\n%s%s\n", m.pointer(fcSubmit), m.button())
	if m.errMsg != "" {
		fmt.Fprintf(&b, "  %s\n", styErr.Render("✗ "+m.errMsg))
	}

	fmt.Fprintf(&b, "%s\n", m.divider())
	fmt.Fprintf(&b, " %s\n", styHelp.Render("tab/↑↓ move · enter next · space toggles advanced · esc cancel"))
	return b.String()
}

// button renders the [ Bootstrap ] action (inverse-highlighted when focused).
// Brackets keep it visually distinct from the ▸/▾ advanced caret.
func (m bootstrapModel) button() string {
	base := lipgloss.NewStyle().Bold(true)
	if m.order[m.focus] == fcSubmit {
		return base.Foreground(lipgloss.Color("0")).Background(tuiAccent).Render("[ Bootstrap ]")
	}
	return base.Foreground(tuiAccent).Render("[ Bootstrap ]")
}

func (m bootstrapModel) viewProvisioning() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n %s\n\n", styTitle.Render("keel bootstrap — provisioning"))

	// Docker-style: a fixed-height window tailing the most recent log lines.
	// Each line is clamped to the window width so long output never wraps.
	w := m.contentWidth()
	cell := lipgloss.NewStyle().Width(w)
	tail := m.logs
	if len(tail) > logWindowHeight {
		tail = tail[len(tail)-logWindowHeight:]
	}
	rendered := make([]string, 0, logWindowHeight)
	for _, msg := range tail {
		rendered = append(rendered, cell.Render(renderLog(msg, w)))
	}
	for len(rendered) < logWindowHeight {
		rendered = append(rendered, cell.Render(""))
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tuiHint).
		Padding(0, 1).
		Render(strings.Join(rendered, "\n"))
	fmt.Fprintf(&b, "%s\n\n", box)

	switch {
	case !m.finished:
		fmt.Fprintf(&b, "  %s%s\n", m.spinner.View(), styHelp.Render("working…"))
	case m.runErr != nil:
		fmt.Fprintf(&b, "  %s\n\n%s\n", styErr.Render("✗ "+m.runErr.Error()), styHelp.Render("press any key to close"))
	default:
		fmt.Fprintf(&b, "  %s\n\n%s\n", styOK.Render("✓ all hosts bootstrapped"), styHelp.Render("press any key to close"))
	}
	return b.String()
}

// --- production runner -------------------------------------------------------

// runBootstrapTUI is the default tui seam: it seeds the model from the CLI
// args/flags + config, runs it full screen, and provisions in place.
func runBootstrapTUI(a *app, seed bootstrapParams) error {
	f := newBootstrapFields(seed, a.cfg)
	model := newBootstrapModel(f, func(p bootstrapParams) <-chan tea.Msg {
		return provisionStream(a, p)
	})
	out, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}
	final := out.(bootstrapModel)
	if final.canceled {
		return nil
	}
	return final.runErr
}

// provisionStream provisions each host in a goroutine, streaming step headers
// and live host output as log messages.
func provisionStream(a *app, p bootstrapParams) <-chan tea.Msg {
	ch := make(chan tea.Msg)
	go func() {
		defer close(ch)
		for _, host := range p.hosts {
			target, opts := a.resolveTarget(host, p)
			ch <- provisionLogMsg{host, "connecting to " + target.Addr() + " as " + target.User, true}
			client, err := a.dialer(target, opts)
			if err != nil {
				ch <- provisionDoneMsg{fmt.Errorf("connect %s: %w", host, err)}
				return
			}
			prov := bootstrap.Provisioner{
				Exec:        client,
				Sudo:        bootstrap.SudoWrapperFor(target.User, p.password),
				AdminUser:   p.adminUser,
				ConnectUser: target.User,
				OnStep:      func(s string) { ch <- provisionLogMsg{host, s, true} },
				OnOutput:    func(l string) { ch <- provisionLogMsg{host, l, false} },
			}
			err = prov.Run(p.keys)
			client.Close()
			if err != nil {
				ch <- provisionDoneMsg{fmt.Errorf("provision %s: %w", host, err)}
				return
			}
			ch <- provisionLogMsg{host, "ready ✓", true}
		}
		ch <- provisionDoneMsg{}
	}()
	return ch
}
