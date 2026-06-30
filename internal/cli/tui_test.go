package cli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DigitalTolk/keel/internal/config"
	"github.com/DigitalTolk/keel/internal/ssh"
)

// --- field mapping -----------------------------------------------------------

func TestSplitList(t *testing.T) {
	got := splitList("web1, web2  web3\tweb4\nweb5")
	want := []string{"web1", "web2", "web3", "web4", "web5"}
	if !slices.Equal(got, want) {
		t.Fatalf("splitList = %v, want %v", got, want)
	}
	if len(splitList("   ,  \t")) != 0 {
		t.Errorf("splitList of separators only should be empty")
	}
}

func TestNewBootstrapFieldsSeedsDefaults(t *testing.T) {
	cfg := config.Default() // user bofh, port 22
	f := newBootstrapFields(bootstrapParams{}, cfg)
	if f.user != "bofh" || f.port != "22" || f.adminUser != "bofh" {
		t.Fatalf("defaults wrong: %+v", f)
	}

	f = newBootstrapFields(bootstrapParams{
		hosts: []string{"a", "b"}, user: "root", port: 2222, jump: "j@h#22",
		identities: []string{"/k1", "/k2"}, adminUser: "ops", keys: []string{"key-1", "key-2"},
	}, cfg)
	if f.hosts != "a b" || f.user != "root" || f.port != "2222" || f.jump != "j@h#22" ||
		f.adminUser != "ops" || f.identities != "/k1 /k2" || f.pubkeys != "key-1\nkey-2" {
		t.Fatalf("supplied values not seeded: %+v", f)
	}
}

func TestBootstrapFieldsToParams(t *testing.T) {
	f := bootstrapFields{
		hosts: "web1, web2", user: "root", port: "2222", adminUser: "ops",
		jump: "j@h#22", password: "pw",
		identities: "/k1 /k2", pubkeys: "ssh-ed25519 ONE a@b\n\nssh-ed25519 TWO c@d\n",
	}
	p, err := f.toParams()
	if err != nil {
		t.Fatalf("toParams: %v", err)
	}
	if !slices.Equal(p.hosts, []string{"web1", "web2"}) {
		t.Errorf("hosts = %v", p.hosts)
	}
	if p.user != "root" || p.port != 2222 || p.adminUser != "ops" || p.jump != "j@h#22" {
		t.Errorf("scalar fields wrong: %+v", p)
	}
	if p.password != "pw" {
		t.Errorf("a filled password should select password auth, got %q", p.password)
	}
	if !slices.Equal(p.identities, []string{"/k1", "/k2"}) {
		t.Errorf("identities = %v", p.identities)
	}
	if !slices.Equal(p.keys, []string{"ssh-ed25519 ONE a@b", "ssh-ed25519 TWO c@d"}) {
		t.Errorf("multiline pubkeys not parsed one-per-line, got %v", p.keys)
	}
}

func TestBootstrapFieldsToParamsKeyAuthDefaults(t *testing.T) {
	p, err := bootstrapFields{hosts: "web1", port: ""}.toParams()
	if err != nil {
		t.Fatalf("toParams: %v", err)
	}
	if p.password != "" {
		t.Errorf("blank password means key auth, got %q", p.password)
	}
	if p.port != 22 {
		t.Errorf("empty port should default to 22, got %d", p.port)
	}
	if p.adminUser != "bofh" {
		t.Errorf("empty admin user should default to bofh, got %q", p.adminUser)
	}
}

func TestBootstrapFieldsToParamsErrors(t *testing.T) {
	if _, err := (bootstrapFields{hosts: "  "}).toParams(); err == nil {
		t.Error("no hosts should error")
	}
	if _, err := (bootstrapFields{hosts: "web1", port: "abc"}).toParams(); err == nil {
		t.Error("non-numeric port should error")
	}
	if _, err := (bootstrapFields{hosts: "web1", pubkeyFile: filepath.Join(t.TempDir(), "nope")}).toParams(); err == nil {
		t.Error("missing pubkey file should error")
	}
}

func TestBootstrapFieldsToParamsReadsPubkeyFile(t *testing.T) {
	pf := filepath.Join(t.TempDir(), "keys")
	if err := os.WriteFile(pf, []byte("ssh-ed25519 FROMFILE a@b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := (bootstrapFields{hosts: "web1", pubkeyFile: pf}).toParams()
	if err != nil {
		t.Fatalf("toParams: %v", err)
	}
	if !slices.Contains(p.keys, "ssh-ed25519 FROMFILE a@b") {
		t.Errorf("pubkey file contents should be collected, got %v", p.keys)
	}
}

// --- TUI model ---------------------------------------------------------------

func key(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "space":
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func step(m bootstrapModel, msg tea.Msg) bootstrapModel {
	out, _ := m.Update(msg)
	return out.(bootstrapModel)
}

func TestModelSeedsAndTypes(t *testing.T) {
	m := newBootstrapModel(bootstrapFields{hosts: "1.2.3.4", user: "bofh", port: "22"}, nil)
	if m.hosts.Value() != "1.2.3.4" {
		t.Fatalf("hosts not prefilled: %q", m.hosts.Value())
	}
	// Focus starts on hosts; typing appends to it.
	m = step(m, key("x"))
	if m.hosts.Value() != "1.2.3.4x" {
		t.Errorf("typing should edit focused field, got %q", m.hosts.Value())
	}
}

func TestModelFocusNavigation(t *testing.T) {
	m := newBootstrapModel(bootstrapFields{}, nil)
	if m.order[m.focus] != fcHosts {
		t.Fatal("focus should start on hosts")
	}
	m = step(m, key("tab"))
	if m.order[m.focus] != fcUser {
		t.Errorf("tab should move to user, got %v", m.order[m.focus])
	}
	m = step(m, key("shift+tab"))
	if m.order[m.focus] != fcHosts {
		t.Errorf("shift+tab should move back to hosts, got %v", m.order[m.focus])
	}
}

func focusOn(m bootstrapModel, fc focusable) bootstrapModel {
	m.focus = slices.Index(m.order, fc)
	m.applyFocus()
	return m
}

func TestModelEnterAdvancesFields(t *testing.T) {
	m := newBootstrapModel(bootstrapFields{}, nil) // focus starts on hosts
	m = step(m, key("enter"))
	if m.order[m.focus] != fcUser {
		t.Errorf("enter on a single-line field should advance, got %v", m.order[m.focus])
	}
}

func TestModelKeysEnterInsertsNewline(t *testing.T) {
	m := focusOn(newBootstrapModel(bootstrapFields{}, nil), fcKeys)
	m = step(m, key("k"))
	m = step(m, key("enter")) // enter in the keys box = newline, not submit
	m = step(m, key("j"))
	if !strings.Contains(m.keys.Value(), "\n") {
		t.Errorf("enter in the keys box should insert a newline, got %q", m.keys.Value())
	}
}

func TestModelAdvancedToggleExpandsAndCollapses(t *testing.T) {
	m := focusOn(newBootstrapModel(bootstrapFields{}, nil), fcAdvanced)
	m = step(m, key("space")) // expand
	if !m.advanced || !slices.Contains(m.order, fcPort) || len(m.order) != 11 {
		t.Fatalf("space should reveal advanced fields, order = %v", m.order)
	}
	m = focusOn(m, fcAdvanced)
	m = step(m, key("enter")) // collapse (enter also toggles)
	if m.advanced || slices.Contains(m.order, fcPort) {
		t.Errorf("enter should collapse advanced, order = %v", m.order)
	}
}

func TestModelSubmitValidStartsProvisioning(t *testing.T) {
	started := false
	start := func(p bootstrapParams) <-chan tea.Msg {
		started = true
		if p.hosts[0] != "web1" {
			t.Errorf("submitted params wrong: %+v", p)
		}
		ch := make(chan tea.Msg)
		close(ch)
		return ch
	}
	m := newBootstrapModel(bootstrapFields{hosts: "web1", port: "22", adminUser: "bofh"}, start)
	m = focusOn(m, fcSubmit)
	m = step(m, key("enter")) // enter on the ▶ Bootstrap button
	if !started || !m.provisioning {
		t.Errorf("enter on the submit button should provision (started=%v, provisioning=%v)", started, m.provisioning)
	}
}

func TestModelSpaceOnSubmitAlsoRuns(t *testing.T) {
	started := false
	m := newBootstrapModel(bootstrapFields{hosts: "web1"}, func(bootstrapParams) <-chan tea.Msg {
		started = true
		ch := make(chan tea.Msg)
		close(ch)
		return ch
	})
	m = focusOn(m, fcSubmit)
	step(m, key("space")) // side effect: start() flips `started`
	if !started {
		t.Error("space on the submit button should also run")
	}
}

func TestModelSubmitInvalidShowsError(t *testing.T) {
	m := newBootstrapModel(bootstrapFields{hosts: ""}, func(bootstrapParams) <-chan tea.Msg {
		t.Fatal("start must not be called on invalid input")
		return nil
	})
	m = focusOn(m, fcSubmit)
	m = step(m, key("enter"))
	if m.provisioning {
		t.Error("invalid submit should not provision")
	}
	if m.errMsg == "" {
		t.Error("invalid submit should set an error message")
	}
}

func TestModelProvisioningReducer(t *testing.T) {
	m := newBootstrapModel(bootstrapFields{hosts: "web1"}, nil)
	m.provisioning = true
	m.ch = make(chan tea.Msg)

	// A header starts a step; output lines accumulate under it.
	m = step(m, provisionLogMsg{"web1", "installing base packages", true})
	m = step(m, provisionLogMsg{"web1", "Reading package lists...", false})
	if !m.stepActive || m.curHeader.line != "installing base packages" || len(m.curOut) != 1 {
		t.Fatalf("step state wrong: active=%v header=%q out=%d", m.stepActive, m.curHeader.line, len(m.curOut))
	}

	// A new header freezes the previous step into the box and starts a new one.
	m = step(m, provisionLogMsg{"web1", "writing sudoers drop-in", true})
	if m.curHeader.line != "writing sudoers drop-in" || len(m.curOut) != 0 {
		t.Errorf("new step should reset output, got header=%q out=%d", m.curHeader.line, len(m.curOut))
	}
	if len(m.committed) != 2 { // previous header + its 1 output line
		t.Errorf("previous step should be frozen into the box, committed=%d", len(m.committed))
	}

	m = step(m, spinner.TickMsg{}) // not-finished tick branch
	m = step(m, provisionDoneMsg{})
	if !m.finished || m.runErr != nil || m.stepActive {
		t.Errorf("clean done expected, finished=%v err=%v active=%v", m.finished, m.runErr, m.stepActive)
	}
	if len(m.committed) != 3 { // + the final step's header
		t.Errorf("final step should be frozen on done, committed=%d", len(m.committed))
	}
	m = step(m, spinner.TickMsg{}) // finished tick branch (no-op)
	if _, cmd := m.Update(key("enter")); cmd == nil {
		t.Error("a key after completion should quit")
	}
}

// TestModelStepOutputCapped verifies each step keeps at most stepOutputMax output
// lines (the live, scrolling window), regardless of how much the command prints.
func TestModelStepOutputCapped(t *testing.T) {
	m := newBootstrapModel(bootstrapFields{hosts: "web1"}, nil)
	m.provisioning = true
	m.ch = make(chan tea.Msg)
	m = step(m, provisionLogMsg{"web1", "installing base packages", true})
	for i := 0; i < 50; i++ {
		m = step(m, provisionLogMsg{"web1", "apt line", false})
	}
	if len(m.curOut) != stepOutputMax {
		t.Errorf("step output should be capped at %d, got %d", stepOutputMax, len(m.curOut))
	}
	// The current step contributes the header + at most stepOutputMax output lines.
	if got := len(m.provisionLines()); got != stepOutputMax+1 {
		t.Errorf("provisionLines = %d, want %d (header + %d output)", got, stepOutputMax+1, stepOutputMax)
	}
}

func TestModelProvisioningError(t *testing.T) {
	m := newBootstrapModel(bootstrapFields{hosts: "web1"}, nil)
	m.provisioning = true
	m = step(m, provisionDoneMsg{errors.New("boom")})
	if !m.finished || m.runErr == nil {
		t.Error("error done should set finished + runErr")
	}
}

func TestModelCancel(t *testing.T) {
	m := newBootstrapModel(bootstrapFields{}, nil)
	out, cmd := m.Update(key("esc"))
	if !out.(bootstrapModel).canceled || cmd == nil {
		t.Error("esc should cancel and quit")
	}
}

func TestModelWindowSizeAndViews(t *testing.T) {
	m := newBootstrapModel(bootstrapFields{hosts: "web1"}, nil)
	m = step(m, tea.WindowSizeMsg{Width: 100, Height: 40})
	if m.width != 100 {
		t.Errorf("window size not stored, width=%d", m.width)
	}
	m.advanced = true
	m.rebuildOrder()
	m.errMsg = "boom"
	if m.viewForm() == "" {
		t.Error("form view should render")
	}
	// Render again with the submit button focused (highlighted branch).
	if focusOn(m, fcSubmit).viewForm() == "" {
		t.Error("form view with focused button should render")
	}
	m.provisioning = true
	m.stepActive = true
	m.curHeader = provisionLogMsg{"web1", "installing base packages", true}
	m.curOut = []provisionLogMsg{
		{"web1", strings.Repeat("very-long-output-line ", 30), false}, // exercises truncation
	}
	if m.viewProvisioning() == "" {
		t.Error("provisioning view should render (running)")
	}
	m.finished = true
	_ = m.viewProvisioning() // success branch
	m.runErr = errors.New("x")
	_ = m.viewProvisioning() // error branch
	if m.Init() == nil {
		t.Error("Init should return a command")
	}
}

func TestModelViewDispatchAndHelpers(t *testing.T) {
	m := newBootstrapModel(bootstrapFields{hosts: "web1"}, nil)
	if m.View() == "" {
		t.Error("View() should render the form")
	}
	m.provisioning = true
	if m.View() == "" {
		t.Error("View() should render the progress view")
	}
	// field() returns nil for the non-textinput focusables.
	if m.field(fcKeys) != nil || m.field(fcAdvanced) != nil {
		t.Error("field() should be nil for keys/advanced")
	}
}

func TestModelRebuildOrderClampsFocus(t *testing.T) {
	m := newBootstrapModel(bootstrapFields{}, nil)
	m.advanced = true
	m.rebuildOrder()
	m.focus = len(m.order) - 1 // focused on a deep advanced field
	m.advanced = false
	m.rebuildOrder() // collapsing must clamp the focus index
	if m.focus >= len(m.order) {
		t.Errorf("focus not clamped after collapse: %d >= %d", m.focus, len(m.order))
	}
}

func TestModelKeyWhileRunningIsIgnored(t *testing.T) {
	m := newBootstrapModel(bootstrapFields{hosts: "web1"}, nil)
	m.provisioning = true
	if _, cmd := m.Update(key("enter")); cmd != nil {
		t.Error("a key press while still provisioning should not quit")
	}
}

func TestModelResizeClampsNarrowWidth(t *testing.T) {
	m := newBootstrapModel(bootstrapFields{}, nil)
	m = step(m, tea.WindowSizeMsg{Width: 10, Height: 5}) // narrower than the min field width
	if m.hosts.Width < 20 {
		t.Errorf("field width should clamp to a minimum, got %d", m.hosts.Width)
	}
}

// TestProvisioningBoxKeepsAllSteps verifies the single box keeps every step's
// header (and its output) — earlier steps are never dropped — while a chatty
// command's output stays capped.
func TestProvisioningBoxKeepsAllSteps(t *testing.T) {
	m := newBootstrapModel(bootstrapFields{hosts: "web1"}, nil)
	m = step(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.provisioning = true
	m.ch = make(chan tea.Msg)

	m = step(m, provisionLogMsg{"web1", "installing base packages", true})
	for i := 0; i < 50; i++ { // a chatty command
		m = step(m, provisionLogMsg{"web1", "apt output", false})
	}
	m = step(m, provisionLogMsg{"web1", "writing sudoers drop-in", true})
	m = step(m, provisionLogMsg{"web1", "parsed OK", false})

	out := m.viewProvisioning()
	// Both step headers are still present — nothing is cut off.
	for _, want := range []string{"installing base packages", "writing sudoers drop-in", "parsed OK"} {
		if !strings.Contains(out, want) {
			t.Errorf("box should keep %q, got:\n%s", want, out)
		}
	}
	// The chatty command's output is capped (only ≤8 of its lines kept).
	if got := strings.Count(out, "apt output"); got > stepOutputMax {
		t.Errorf("command output should be capped at %d, got %d", stepOutputMax, got)
	}
}

func TestTruncateStr(t *testing.T) {
	if truncateStr("hello", 10) != "hello" {
		t.Error("short string should be unchanged")
	}
	if got := truncateStr("hello world", 5); got != "hell…" {
		t.Errorf("truncateStr = %q, want hell…", got)
	}
	if truncateStr("x", 0) != "" {
		t.Error("zero width should be empty")
	}
	if truncateStr("xy", 1) != "…" {
		t.Error("width 1 should be the ellipsis")
	}
}

func TestRenderLog(t *testing.T) {
	h := renderLog(provisionLogMsg{"web1", "installing base packages", true}, 50)
	if !strings.Contains(h, "web1") || !strings.Contains(h, "installing") {
		t.Errorf("header log render = %q", h)
	}
	o := renderLog(provisionLogMsg{"web1", "Reading package lists...", false}, 50)
	if !strings.Contains(o, "Reading") {
		t.Errorf("output log render = %q", o)
	}
	if long := renderLog(provisionLogMsg{"web1", strings.Repeat("a", 200), false}, 30); !strings.Contains(long, "…") {
		t.Errorf("a long line should be truncated with an ellipsis: %q", long)
	}
}

func TestWaitForActivity(t *testing.T) {
	ch := make(chan tea.Msg, 1)
	ch <- provisionLogMsg{"h", "x", false}
	if _, ok := waitForActivity(ch)().(provisionLogMsg); !ok {
		t.Error("should deliver the queued message")
	}
	close(ch)
	if _, ok := waitForActivity(ch)().(provisionDoneMsg); !ok {
		t.Error("a closed channel should yield a done message")
	}
}

// --- provisionStream (real goroutine, fake dialer) ---------------------------

func drain(ch <-chan tea.Msg) []tea.Msg {
	var msgs []tea.Msg
	for m := range ch {
		msgs = append(msgs, m)
	}
	return msgs
}

func TestProvisionStreamSuccess(t *testing.T) {
	a, _ := newTestApp()
	a.dialer = func(ssh.Target, ssh.DialOptions) (SSHSession, error) { return &fakeSession{}, nil }
	msgs := drain(provisionStream(a, bootstrapParams{
		hosts: []string{"web1"}, user: "root", port: 22, adminUser: "bofh", keys: []string{"ssh-ed25519 K a@b"},
	}))
	last := msgs[len(msgs)-1]
	if d, ok := last.(provisionDoneMsg); !ok || d.err != nil {
		t.Fatalf("expected clean done, got %#v", last)
	}
	hasReady := false
	for _, m := range msgs {
		if s, ok := m.(provisionLogMsg); ok && s.header && strings.Contains(s.line, "ready") {
			hasReady = true
		}
	}
	if !hasReady {
		t.Error("expected a host-ready log message")
	}
}

func TestProvisionStreamDialError(t *testing.T) {
	a, _ := newTestApp()
	a.dialer = func(ssh.Target, ssh.DialOptions) (SSHSession, error) { return nil, errScan }
	msgs := drain(provisionStream(a, bootstrapParams{hosts: []string{"web1"}, user: "root", port: 22}))
	if d, ok := msgs[len(msgs)-1].(provisionDoneMsg); !ok || d.err == nil {
		t.Fatalf("dial failure should end with an error done, got %#v", msgs[len(msgs)-1])
	}
}

func TestProvisionStreamProvisionError(t *testing.T) {
	a, _ := newTestApp()
	a.dialer = func(ssh.Target, ssh.DialOptions) (SSHSession, error) {
		return &fakeSession{execErr: errScan}, nil
	}
	msgs := drain(provisionStream(a, bootstrapParams{hosts: []string{"web1"}, user: "root", port: 22, adminUser: "bofh"}))
	if d, ok := msgs[len(msgs)-1].(provisionDoneMsg); !ok || d.err == nil {
		t.Fatalf("provision failure should end with an error done, got %#v", msgs[len(msgs)-1])
	}
}
