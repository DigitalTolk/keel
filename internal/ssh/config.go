package ssh

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kevinburke/ssh_config"
)

// HostConfig holds the subset of ~/.ssh/config settings keel applies when
// resolving a host alias.
type HostConfig struct {
	HostName      string
	User          string
	Port          int
	IdentityFiles []string
	ProxyJump     string // converted to keel's "[user@]host[#port]" form
}

// ResolveHost reads ~/.ssh/config (if present) and resolves alias. A missing
// config or unknown alias yields a zero HostConfig, so the alias is used as-is.
func ResolveHost(alias string) HostConfig {
	home, err := os.UserHomeDir()
	if err != nil {
		return HostConfig{}
	}
	f, err := os.Open(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return HostConfig{}
	}
	defer f.Close()
	hc, err := resolveHostFrom(f, alias)
	if err != nil {
		return HostConfig{}
	}
	return hc
}

// resolveHostFrom parses an ssh_config from r and resolves alias. It reads only
// values present in the file (no implicit ssh defaults), so unset fields stay
// zero and keel's own defaults apply instead.
func resolveHostFrom(r io.Reader, alias string) (HostConfig, error) {
	cfg, err := ssh_config.Decode(r)
	if err != nil {
		return HostConfig{}, err
	}
	first := func(key string) string {
		vs, _ := cfg.GetAll(alias, key)
		if len(vs) > 0 {
			return strings.TrimSpace(vs[0])
		}
		return ""
	}

	hc := HostConfig{
		HostName: first("HostName"),
		User:     first("User"),
	}
	if v := first("Port"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			hc.Port = n
		}
	}
	if ids, _ := cfg.GetAll(alias, "IdentityFile"); len(ids) > 0 {
		for _, id := range ids {
			if id = strings.TrimSpace(id); id != "" {
				hc.IdentityFiles = append(hc.IdentityFiles, expandHome(id))
			}
		}
	}
	hc.ProxyJump = proxyJumpToTarget(first("ProxyJump"))
	return hc, nil
}

// expandHome expands a leading ~ to the user's home directory.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// proxyJumpToTarget converts an ssh ProxyJump value ("[user@]host[:port]",
// possibly a comma-separated chain) into keel's "[user@]host[#port]" form,
// using the first hop. Empty / "none" yields "".
func proxyJumpToTarget(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "none") {
		return ""
	}
	user, hostport := "", v
	if i := strings.LastIndex(v, "@"); i >= 0 {
		user, hostport = v[:i], v[i+1:]
	}
	host, port := hostport, ""
	if i := strings.LastIndex(hostport, ":"); i >= 0 {
		host, port = hostport[:i], hostport[i+1:]
	}
	out := host
	if user != "" {
		out = user + "@" + host
	}
	if port != "" {
		out += "#" + port
	}
	return out
}
