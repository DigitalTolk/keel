package runner

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestStreamIncludesStderrInError(t *testing.T) {
	var buf bytes.Buffer
	err := Exec{}.Stream(context.Background(), &buf, "sh", "-c", "echo problem >&2; exit 1")
	if err == nil {
		t.Fatal("expected error for failing command")
	}
	if !strings.Contains(err.Error(), "problem") {
		t.Errorf("error should include stderr output, got %q", err.Error())
	}
}
