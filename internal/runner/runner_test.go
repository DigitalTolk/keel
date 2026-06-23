package runner

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRequireToolsReportsMissing(t *testing.T) {
	err := RequireTools("sh", "definitely-not-a-real-binary-xyz")
	if err == nil {
		t.Fatal("RequireTools: want error for missing tool, got nil")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-real-binary-xyz") {
		t.Errorf("error should name the missing tool, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "sh ") || strings.Contains(err.Error(), "'sh'") {
		t.Errorf("error should not name the present tool 'sh', got %q", err.Error())
	}
}

func TestRequireToolsPassesWhenPresent(t *testing.T) {
	if err := RequireTools("sh"); err != nil {
		t.Fatalf("RequireTools(sh): unexpected error %v", err)
	}
}

func TestExecStreamsStdout(t *testing.T) {
	var buf bytes.Buffer
	r := Exec{}
	if err := r.Stream(context.Background(), &buf, "printf", "hello"); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got := buf.String(); got != "hello" {
		t.Fatalf("Stream stdout = %q, want %q", got, "hello")
	}
}

func TestExecStreamNonZeroExitIsError(t *testing.T) {
	var buf bytes.Buffer
	r := Exec{}
	if err := r.Stream(context.Background(), &buf, "sh", "-c", "exit 7"); err == nil {
		t.Fatal("Stream: want error on non-zero exit, got nil")
	}
}
