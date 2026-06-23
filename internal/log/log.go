// Package log provides a small leveled logger that ports the behavior of
// consolelog from libs/functions.lib.sh: a UTC timestamp prefix (suppressed
// when running under Jenkins), colored levels written only to a TTY, and
// errors routed to stderr.
package log

import (
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"
)

const (
	colorReset   = "\x1b[0m"
	colorInfo    = "\x1b[0;37m"
	colorSuccess = "\x1b[0;32m"
	colorError   = "\x1b[1;31m"
)

// Logger is a leveled console logger. Construct it with New.
type Logger struct {
	out        io.Writer
	err        io.Writer
	color      bool
	timestamps bool
	now        func() time.Time
	prog       string
}

// Option configures a Logger.
type Option func(*Logger)

// WithWriters sets the stdout and stderr destinations.
func WithWriters(out, err io.Writer) Option {
	return func(l *Logger) { l.out, l.err = out, err }
}

// WithClock injects a clock, primarily for deterministic tests.
func WithClock(now func() time.Time) Option {
	return func(l *Logger) { l.now = now }
}

// WithColor forces color on or off, bypassing TTY detection.
func WithColor(c bool) Option {
	return func(l *Logger) { l.color = c }
}

// WithTimestamps toggles the timestamp prefix. It is disabled automatically
// under Jenkins (see New).
func WithTimestamps(ts bool) Option {
	return func(l *Logger) { l.timestamps = ts }
}

// WithProgram sets the program name shown in each line.
func WithProgram(name string) Option {
	return func(l *Logger) { l.prog = name }
}

// New builds a Logger. By default it writes to os.Stdout/os.Stderr, uses the
// real UTC clock, enables timestamps unless JENKINS_HOME is set, and enables
// color only when stdout is a terminal.
func New(opts ...Option) *Logger {
	l := &Logger{
		out:        os.Stdout,
		err:        os.Stderr,
		now:        func() time.Time { return time.Now().UTC() },
		prog:       "keel",
		timestamps: os.Getenv("JENKINS_HOME") == "",
		color:      term.IsTerminal(int(os.Stdout.Fd())),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Info logs an informational message to stdout.
func (l *Logger) Info(msg string) { l.write(l.out, colorInfo, msg) }

// Success logs a success message to stdout.
func (l *Logger) Success(msg string) { l.write(l.out, colorSuccess, msg) }

// Error logs an error message to stderr.
func (l *Logger) Error(msg string) { l.write(l.err, colorError, msg) }

func (l *Logger) write(w io.Writer, color, msg string) {
	var ts string
	if l.timestamps {
		ts = l.now().Format("[2006-01-02 15:04:05] ")
	}
	body := fmt.Sprintf("%s%s: %s", ts, l.prog, msg)
	if l.color {
		body = color + body + colorReset
	}
	fmt.Fprintln(w, body)
}
