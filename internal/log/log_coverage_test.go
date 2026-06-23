package log

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewDisablesTimestampsUnderJenkins(t *testing.T) {
	t.Setenv("JENKINS_HOME", "/var/lib/jenkins")
	var out, errw bytes.Buffer
	l := New(WithWriters(&out, &errw), WithColor(false))
	l.Info("hi")
	if strings.HasPrefix(out.String(), "[") {
		t.Errorf("under Jenkins, timestamps should be suppressed; got %q", out.String())
	}
}

func TestNewDefaultsOutsideJenkins(t *testing.T) {
	t.Setenv("JENKINS_HOME", "")
	var out, errw bytes.Buffer
	l := New(WithWriters(&out, &errw), WithColor(false))
	l.Info("hi")
	if !strings.HasPrefix(out.String(), "[") {
		t.Errorf("outside Jenkins, expected a timestamp prefix; got %q", out.String())
	}
}
