package vbox

import (
	"slices"
	"strings"
	"testing"
)

func TestOSTypeFromISO(t *testing.T) {
	if got := OSType("ubuntu-24.04.iso"); got != "Ubuntu_64" {
		t.Errorf("ubuntu iso -> %q, want Ubuntu_64", got)
	}
	if got := OSType("debian-12.iso"); got != "Debian_64" {
		t.Errorf("non-ubuntu iso -> %q, want Debian_64", got)
	}
	if got := OSType(""); got != "Debian_64" {
		t.Errorf("empty iso -> %q, want Debian_64", got)
	}
}

func TestCreateVMArgs(t *testing.T) {
	got := CreateVMArgs("web1", "Ubuntu_64", "/vms")
	want := []string{"createvm", "--name", "web1", "--ostype", "Ubuntu_64", "--basefolder", "/vms", "--register"}
	if !slices.Equal(got, want) {
		t.Fatalf("CreateVMArgs = %v, want %v", got, want)
	}
}

func TestParseCreateVMUUID(t *testing.T) {
	out := "Virtual machine 'web1' is created and registered.\nUUID: 79725d5f-5f5a-40be-a935-cd6b518384e6\nSettings file: '/vms/web1/web1.vbox'\n"
	uuid, err := ParseCreateVMUUID(out)
	if err != nil {
		t.Fatal(err)
	}
	if uuid != "79725d5f-5f5a-40be-a935-cd6b518384e6" {
		t.Fatalf("uuid = %q", uuid)
	}

	if _, err := ParseCreateVMUUID("no uuid here"); err == nil {
		t.Fatal("expected error when no UUID present")
	}
}

func TestModifyVMArgs(t *testing.T) {
	got := ModifyVMArgs("UUID1", VMSpec{
		CPUs: 4, MemoryMB: 4096, RDPAddress: "10.0.0.5", RDPPort: 3390,
		BridgeAdapter: "eth1", MACAddress: "080027ABCDEF",
	})
	joined := strings.Join(got, " ")
	for _, want := range []string{
		"modifyvm UUID1", "--cpus 4", "--memory 4096",
		"--vrdeport 3390", "--vrdeaddress 10.0.0.5",
		"--bridgeadapter1 eth1", "--macaddress1 080027ABCDEF",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("ModifyVMArgs missing %q in: %s", want, joined)
		}
	}
}

func TestModifyVMArgsOmitsMACWhenEmpty(t *testing.T) {
	got := ModifyVMArgs("U", VMSpec{CPUs: 1, MemoryMB: 512, RDPAddress: "1.2.3.4", RDPPort: 1000, BridgeAdapter: "eth0"})
	if slices.Contains(got, "--macaddress1") {
		t.Errorf("MAC flag should be omitted when empty: %v", got)
	}
}

func TestStorageAndMediumArgs(t *testing.T) {
	if got := StorageCtlArgs("U"); !strings.Contains(strings.Join(got, " "), "--name SATA Controller --add sata") {
		t.Errorf("StorageCtlArgs = %v", got)
	}
	if got := CreateMediumArgs("/vms/web1/web1.vdi", 20480); !slices.Contains(got, "--size") || !slices.Contains(got, "20480") {
		t.Errorf("CreateMediumArgs = %v", got)
	}
	if got := AttachDiskArgs("U", "/vms/web1/web1.vdi"); !strings.Contains(strings.Join(got, " "), "--type hdd") {
		t.Errorf("AttachDiskArgs = %v", got)
	}
	if got := AttachISOArgs("U", "/iso/ubuntu.iso"); !strings.Contains(strings.Join(got, " "), "--type dvddrive") {
		t.Errorf("AttachISOArgs = %v", got)
	}
	if got := StartVMArgs("U"); !slices.Equal(got, []string{"startvm", "U"}) {
		t.Errorf("StartVMArgs = %v", got)
	}
}
