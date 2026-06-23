package ssh

import (
	"strings"
)

// UpsertKnownHostsLine returns content with any existing entry for the same
// host pattern (the first field of newLine) removed, and newLine appended.
// This mirrors the script's "ssh-keygen -R host" followed by a fresh
// ssh-keyscan append, keeping the file free of stale duplicate keys.
func UpsertKnownHostsLine(content []byte, newLine string) []byte {
	newLine = strings.TrimRight(newLine, "\n")
	pattern := firstField(newLine)

	var kept []string
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if firstField(line) == pattern {
			continue // drop stale entry for this host
		}
		kept = append(kept, line)
	}
	kept = append(kept, newLine)
	return []byte(strings.Join(kept, "\n") + "\n")
}

func firstField(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
