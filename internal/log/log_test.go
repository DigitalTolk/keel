package log

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func fixedClock() time.Time {
	return time.Date(2026, 6, 19, 8, 30, 15, 0, time.UTC)
}

func newTestLogger(out, errw *bytes.Buffer, opts ...Option) *Logger {
	base := []Option{
		WithWriters(out, errw),
		WithClock(fixedClock),
		WithColor(false),
		WithProgram("keel"),
	}
	return New(append(base, opts...)...)
}

func TestInfoWritesTimestampedLineToStdout(t *testing.T) {
	var out, errw bytes.Buffer
	l := newTestLogger(&out, &errw)

	l.Info("adding web1 to inventory")

	want := "[2026-06-19 08:30:15] keel: adding web1 to inventory\n"
	if got := out.String(); got != want {
		t.Fatalf("Info() stdout = %q, want %q", got, want)
	}
	if errw.Len() != 0 {
		t.Fatalf("Info() wrote to stderr: %q", errw.String())
	}
}

func TestErrorWritesToStderr(t *testing.T) {
	var out, errw bytes.Buffer
	l := newTestLogger(&out, &errw)

	l.Error("connection refused")

	want := "[2026-06-19 08:30:15] keel: connection refused\n"
	if got := errw.String(); got != want {
		t.Fatalf("Error() stderr = %q, want %q", got, want)
	}
	if out.Len() != 0 {
		t.Fatalf("Error() wrote to stdout: %q", out.String())
	}
}

func TestJenkinsModeOmitsTimestamp(t *testing.T) {
	var out, errw bytes.Buffer
	l := newTestLogger(&out, &errw, WithTimestamps(false))

	l.Success("done")

	want := "keel: done\n"
	if got := out.String(); got != want {
		t.Fatalf("Success() with timestamps off = %q, want %q", got, want)
	}
}

func TestColorWrapsWhenEnabled(t *testing.T) {
	var out, errw bytes.Buffer
	l := newTestLogger(&out, &errw, WithColor(true))

	l.Success("ok")

	got := out.String()
	if !strings.HasPrefix(got, "\x1b[") {
		t.Fatalf("Success() with color = %q, want ANSI escape prefix", got)
	}
	if !strings.HasSuffix(got, "\x1b[0m\n") {
		t.Fatalf("Success() with color = %q, want reset suffix", got)
	}
	if !strings.Contains(got, "keel: ok") {
		t.Fatalf("Success() with color = %q, want message body", got)
	}
}
