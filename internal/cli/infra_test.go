package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	awscloud "github.com/DigitalTolk/keel/internal/cloud/aws"
	"github.com/DigitalTolk/keel/internal/runner"
	"github.com/DigitalTolk/keel/internal/vbox"
)

// --- jenkins batch-edit ------------------------------------------------------

func TestJenkinsBatchEditCmd(t *testing.T) {
	a, _ := newTestApp()
	root := t.TempDir()
	p := filepath.Join(root, "config.xml")
	_ = os.WriteFile(p, []byte("host=old.example"), 0o644)

	if err := runCmd(a, "jenkins", "batch-edit", "--root", root, "old.example", "new.example"); err != nil {
		t.Fatalf("batch-edit: %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "host=new.example" {
		t.Fatalf("not replaced: %q", got)
	}
}

func TestJenkinsBatchEditMissingRoot(t *testing.T) {
	a, _ := newTestApp()
	if err := runCmd(a, "jenkins", "batch-edit", "--root", filepath.Join(t.TempDir(), "nope"), "a", "b"); err == nil {
		t.Fatal("missing root should error")
	}
}

// --- mysql to-innodb ---------------------------------------------------------

func TestConvertToInnoDB(t *testing.T) {
	a, _ := newTestApp()
	a.runnerFactory = func() runner.Runner {
		return &fakeRunner{responder: func(name string, args []string) (string, error) {
			if slices.Contains(args, "show tables") {
				return "widgets\norders\n", nil
			}
			return "", nil // ALTER statements
		}}
	}
	n, err := a.convertToInnoDB(context.Background(), "/tmp/my.cnf", "app")
	if err != nil {
		t.Fatalf("convertToInnoDB: %v", err)
	}
	if n != 2 {
		t.Fatalf("converted %d tables, want 2", n)
	}
}

func TestConvertToInnoDBListError(t *testing.T) {
	a, _ := newTestApp()
	a.runnerFactory = func() runner.Runner {
		return &fakeRunner{responder: func(string, []string) (string, error) { return "", errScan }}
	}
	if _, err := a.convertToInnoDB(context.Background(), "/c", "app"); err == nil {
		t.Fatal("show tables error should propagate")
	}
}

func TestConvertToInnoDBAlterError(t *testing.T) {
	a, _ := newTestApp()
	a.runnerFactory = func() runner.Runner {
		return &fakeRunner{responder: func(name string, args []string) (string, error) {
			if slices.Contains(args, "show tables") {
				return "widgets\n", nil
			}
			return "", errScan // ALTER fails
		}}
	}
	if _, err := a.convertToInnoDB(context.Background(), "/c", "app"); err == nil {
		t.Fatal("alter error should propagate")
	}
}

func TestMySQLToInnoDBCnfError(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does-not-exist"))
	a, _ := newTestApp()
	noTools(a)
	a.runnerFactory = func() runner.Runner { return &fakeRunner{} }
	if err := runCmd(a, "mysql", "to-innodb", "--host", "h", "--db", "app"); err == nil {
		t.Fatal("unwritable TMPDIR should fail my.cnf creation")
	}
}

func TestMySQLToInnoDBConvertError(t *testing.T) {
	a, _ := newTestApp()
	noTools(a)
	a.runnerFactory = func() runner.Runner {
		return &fakeRunner{responder: func(string, []string) (string, error) { return "", errScan }}
	}
	if err := runCmd(a, "mysql", "to-innodb", "--host", "h", "--db", "app"); err == nil {
		t.Fatal("convert error should propagate from the command")
	}
}

func TestMySQLToInnoDBRequiresDB(t *testing.T) {
	t.Setenv("MYSQL_DB", "")
	a, _ := newTestApp()
	if err := runCmd(a, "mysql", "to-innodb", "--host", "h"); err == nil {
		t.Fatal("missing --db should error")
	}
}

func TestMySQLToInnoDBCmd(t *testing.T) {
	a, _ := newTestApp()
	noTools(a)
	a.runnerFactory = func() runner.Runner {
		return &fakeRunner{responder: func(name string, args []string) (string, error) {
			if slices.Contains(args, "show tables") {
				return "t1\n", nil
			}
			return "", nil
		}}
	}
	if err := runCmd(a, "mysql", "to-innodb", "--host", "h", "--db", "app"); err != nil {
		t.Fatalf("to-innodb cmd: %v", err)
	}
}

// --- vbox create -------------------------------------------------------------

func vboxSpec() vbox.VMSpec {
	return vbox.VMSpec{Name: "web1", OSType: "Ubuntu_64", BaseDir: "/vms", CPUs: 2, MemoryMB: 1024, RDPAddress: "1.2.3.4", RDPPort: 3390, BridgeAdapter: "eth0"}
}

func TestCreateVBox(t *testing.T) {
	a, _ := newTestApp()
	a.runnerFactory = func() runner.Runner {
		return &fakeRunner{responder: func(name string, args []string) (string, error) {
			if len(args) > 0 && args[0] == "createvm" {
				return "UUID: abc-123\n", nil
			}
			return "", nil
		}}
	}
	if err := a.createVBox(context.Background(), vboxSpec(), "/iso/ubuntu.iso", 20480); err != nil {
		t.Fatalf("createVBox: %v", err)
	}
}

func TestCreateVBoxNoDiskNoISO(t *testing.T) {
	a, _ := newTestApp()
	a.runnerFactory = func() runner.Runner {
		return &fakeRunner{responder: func(name string, args []string) (string, error) {
			if len(args) > 0 && args[0] == "createvm" {
				return "UUID: u\n", nil
			}
			return "", nil
		}}
	}
	if err := a.createVBox(context.Background(), vboxSpec(), "", 0); err != nil {
		t.Fatalf("createVBox without disk/iso: %v", err)
	}
}

func TestCreateVBoxNoUUID(t *testing.T) {
	a, _ := newTestApp()
	a.runnerFactory = func() runner.Runner { return &fakeRunner{} } // empty output
	if err := a.createVBox(context.Background(), vboxSpec(), "", 0); err == nil {
		t.Fatal("missing UUID in createvm output should error")
	}
}

func TestCreateVBoxCreateError(t *testing.T) {
	a, _ := newTestApp()
	a.runnerFactory = func() runner.Runner {
		return &fakeRunner{responder: func(string, []string) (string, error) { return "", errScan }}
	}
	if err := a.createVBox(context.Background(), vboxSpec(), "", 0); err == nil {
		t.Fatal("createvm error should propagate")
	}
}

func TestVboxCreateRequiredFlags(t *testing.T) {
	a, _ := newTestApp()
	if err := runCmd(a, "vbox", "create"); err == nil {
		t.Fatal("missing required flags should error")
	}
}

func TestVboxCreateBadRDP(t *testing.T) {
	a, _ := newTestApp()
	err := runCmd(a, "vbox", "create", "-b", "/vms", "-c", "2", "-m", "1024", "-l", "not-host-port")
	if err == nil {
		t.Fatal("invalid --rdp should error")
	}
}

func TestVboxCreateValidFlags(t *testing.T) {
	a, _ := newTestApp()
	noTools(a) // bypass the VBoxManage presence check
	a.runnerFactory = func() runner.Runner {
		return &fakeRunner{responder: func(name string, args []string) (string, error) {
			if len(args) > 0 && args[0] == "createvm" {
				return "UUID: deadbeef\n", nil
			}
			return "", nil
		}}
	}
	if err := runCmd(a, "vbox", "create", "-b", "/vms", "-c", "2", "-m", "1024", "-l", "1.2.3.4:3390", "-s", "10240", "-i", "ubuntu.iso"); err != nil {
		t.Fatalf("vbox create: %v", err)
	}
}

// --- aws sg-ingress ----------------------------------------------------------

type fakeSGClient struct {
	authorized []string
	revoked    []string
}

func (f *fakeSGClient) HasIngress(context.Context, string, int, string) (bool, error) {
	return false, nil
}
func (f *fakeSGClient) Authorize(_ context.Context, sg string, _ int, cidr string) error {
	f.authorized = append(f.authorized, sg+"|"+cidr)
	return nil
}
func (f *fakeSGClient) Revoke(_ context.Context, sg string, _ int, cidr string) error {
	f.revoked = append(f.revoked, sg+"|"+cidr)
	return nil
}

func TestSGIngressAuthorizes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, _ := newTestApp()
	a.fetchIP = func(context.Context, string) (string, error) { return "5.6.7.8", nil }
	fake := &fakeSGClient{}
	a.sgClientFactory = func(context.Context, awscloud.SGOptions) (awscloud.SGClient, error) { return fake, nil }

	if err := runCmd(a, "aws", "sg-ingress", "-l", "sg-1", "-l", "sg-2", "-p", "443"); err != nil {
		t.Fatalf("sg-ingress: %v", err)
	}
	if len(fake.authorized) != 2 {
		t.Fatalf("authorized = %v, want 2 groups", fake.authorized)
	}
	// state persisted
	sp, _ := sgIngressStatePath("default", []string{"sg-1", "sg-2"}, 443)
	if readLastIP(sp) != "5.6.7.8" {
		t.Errorf("state not persisted, got %q", readLastIP(sp))
	}
}

func TestSGIngressNoChange(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, _ := newTestApp()
	a.fetchIP = func(context.Context, string) (string, error) { return "1.2.3.4", nil }
	a.sgClientFactory = func(context.Context, awscloud.SGOptions) (awscloud.SGClient, error) {
		t.Fatal("client factory must not be called when IP is unchanged")
		return nil, nil
	}
	sp, _ := sgIngressStatePath("default", []string{"sg-1"}, 22)
	_ = writeLastIP(sp, "1.2.3.4")

	if err := runCmd(a, "aws", "sg-ingress", "-l", "sg-1", "-p", "22"); err != nil {
		t.Fatalf("sg-ingress no-change: %v", err)
	}
}

func TestSGIngressForceUpdatesEvenWhenUnchanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, _ := newTestApp()
	a.fetchIP = func(context.Context, string) (string, error) { return "1.2.3.4", nil }
	fake := &fakeSGClient{}
	a.sgClientFactory = func(context.Context, awscloud.SGOptions) (awscloud.SGClient, error) { return fake, nil }
	sp, _ := sgIngressStatePath("default", []string{"sg-1"}, 22)
	_ = writeLastIP(sp, "1.2.3.4")

	if err := runCmd(a, "aws", "sg-ingress", "-l", "sg-1", "-p", "22", "--force"); err != nil {
		t.Fatalf("sg-ingress --force: %v", err)
	}
	if len(fake.authorized) != 1 {
		t.Fatalf("force should re-authorize, got %v", fake.authorized)
	}
}

func TestSGIngressHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	a, _ := newTestApp()
	a.fetchIP = func(context.Context, string) (string, error) { return "1.2.3.4", nil }
	if err := runCmd(a, "aws", "sg-ingress", "-l", "sg-1", "-p", "22"); err == nil {
		t.Fatal("empty HOME should fail state-path resolution")
	}
}

func TestSGIngressRequiredFlags(t *testing.T) {
	a, _ := newTestApp()
	if err := runCmd(a, "aws", "sg-ingress", "-p", "22"); err == nil {
		t.Fatal("missing --security-group should error")
	}
	if err := runCmd(a, "aws", "sg-ingress", "-l", "sg-1"); err == nil {
		t.Fatal("missing --port should error")
	}
}

func TestSGIngressFetchIPError(t *testing.T) {
	a, _ := newTestApp()
	a.fetchIP = func(context.Context, string) (string, error) { return "", errScan }
	if err := runCmd(a, "aws", "sg-ingress", "-l", "sg-1", "-p", "22"); err == nil {
		t.Fatal("fetch IP error should propagate")
	}
}

func TestSGIngressClientFactoryError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, _ := newTestApp()
	a.fetchIP = func(context.Context, string) (string, error) { return "5.6.7.8", nil }
	a.sgClientFactory = func(context.Context, awscloud.SGOptions) (awscloud.SGClient, error) {
		return nil, errScan
	}
	if err := runCmd(a, "aws", "sg-ingress", "-l", "sg-1", "-p", "22"); err == nil {
		t.Fatal("client factory error should propagate")
	}
}

func TestFetchPublicIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("203.0.113.7\n"))
	}))
	defer srv.Close()

	ip, err := fetchPublicIP(context.Background(), srv.URL)
	if err != nil || ip != "203.0.113.7" {
		t.Fatalf("fetchPublicIP = %q, %v", ip, err)
	}
}

func TestFetchPublicIPRejectsGarbage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-an-ip"))
	}))
	defer srv.Close()
	if _, err := fetchPublicIP(context.Background(), srv.URL); err == nil {
		t.Fatal("non-IP body should error")
	}
}

func TestFetchPublicIPNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := fetchPublicIP(context.Background(), srv.URL); err == nil {
		t.Fatal("non-200 should error")
	}
}

func TestFetchPublicIPBadURL(t *testing.T) {
	if _, err := fetchPublicIP(context.Background(), "http://127.0.0.1:1"); err == nil {
		t.Fatal("unreachable URL should error")
	}
}

func TestSGIngressStatePathRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sp, err := sgIngressStatePath("prod", []string{"sg-1"}, 22)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sp, "lastip_prod_") {
		t.Errorf("unexpected state path %q", sp)
	}
	if err := writeLastIP(sp, "8.8.8.8"); err != nil {
		t.Fatal(err)
	}
	if readLastIP(sp) != "8.8.8.8" {
		t.Errorf("round trip failed: %q", readLastIP(sp))
	}
	// missing file -> empty
	if readLastIP(filepath.Join(t.TempDir(), "none")) != "" {
		t.Error("missing state file should read empty")
	}
}
