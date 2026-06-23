// Package runner runs external commands (mysqldump, rsync, lftp, …) behind a
// small interface so they can be faked in tests, and centralizes dependency
// checks that the original scripts repeated inline.
package runner

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Runner executes external commands. Implemented by Exec in production and by
// fakes in tests.
type Runner interface {
	// Stream runs name with args, writing its stdout to w. Stderr is captured
	// and included in the error on failure.
	Stream(ctx context.Context, w io.Writer, name string, args ...string) error
}

// Exec is the real Runner backed by os/exec.
type Exec struct{}

// Stream runs the command, streaming stdout to w.
func (Exec) Stream(ctx context.Context, w io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = w
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%s failed: %w: %s", name, err, msg)
		}
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

var _ Runner = Exec{}

// RequireTools returns an error naming every tool in names that is not found on
// PATH, so callers fail fast with an actionable message instead of a cryptic
// exec error mid-operation.
func RequireTools(names ...string) error {
	var missing []string
	for _, n := range names {
		if _, err := exec.LookPath(n); err != nil {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("required tool(s) not installed: %s", strings.Join(missing, ", "))
	}
	return nil
}
