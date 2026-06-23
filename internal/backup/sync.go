package backup

import (
	"fmt"
	"strings"
)

// RsyncOptions configures an rsync pull from a remote host over SSH.
type RsyncOptions struct {
	User    string
	Host    string
	Path    string // remote source path
	Dest    string // local destination directory
	Port    int
	KeyFile string // optional ssh identity
	Sudo    bool   // run the remote rsync via sudo
}

// RsyncArgs builds the rsync argument list, mirroring backup-rsync.sh:
// archive + quiet + delete, an explicit ssh transport with port/key, an
// optional sudo rsync-path, then source and destination.
func RsyncArgs(o RsyncOptions) []string {
	args := []string{"-azq", "--delete"}
	if o.Sudo {
		args = append(args, "--rsync-path=sudo rsync")
	}
	ssh := fmt.Sprintf("ssh -p %d", o.Port)
	if o.KeyFile != "" {
		ssh += " -i " + o.KeyFile
	}
	args = append(args, "-e", ssh)
	args = append(args, fmt.Sprintf("%s@%s:%s", o.User, o.Host, o.Path), o.Dest)
	return args
}

// SftpOptions configures an lftp mirror pull over SFTP.
type SftpOptions struct {
	User      string
	Host      string
	Path      string
	Dest      string
	Port      int
	Parallel  int    // defaults to 8
	ExtraArgs string // optional extra mirror flags
}

// LftpMirrorArgs builds the `lftp -c <script>` arguments, mirroring
// backup-sftp.sh: open the connection then mirror only-newer with delete.
func LftpMirrorArgs(o SftpOptions) []string {
	parallel := o.Parallel
	if parallel == 0 {
		parallel = 8
	}
	mirror := "mirror"
	if o.ExtraArgs != "" {
		mirror += " " + o.ExtraArgs
	}
	mirror += fmt.Sprintf(" --only-newer --delete --parallel=%d %s %s", parallel, o.Path, o.Dest)

	script := strings.Join([]string{
		fmt.Sprintf("open -p %d -u %s,placeholder sftp://%s", o.Port, o.User, o.Host),
		mirror,
		"",
	}, "\n")
	return []string{"-c", script}
}
