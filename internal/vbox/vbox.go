// Package vbox builds VBoxManage argument lists for creating VirtualBox guests,
// porting vbox-create.sh. Args are built here (pure, testable); the caller runs
// them via internal/runner. VBoxManage is shelled out to (it cannot reasonably
// be reimplemented).
package vbox

import (
	"fmt"
	"strconv"
	"strings"
)

// SATAController is the storage controller name used for all disks/ISOs.
const SATAController = "SATA Controller"

// VMSpec describes a guest to create.
type VMSpec struct {
	Name          string
	OSType        string
	BaseDir       string
	CPUs          int
	MemoryMB      int
	RDPAddress    string
	RDPPort       int
	BridgeAdapter string // defaults to eth0
	MACAddress    string // optional
}

// OSType maps an ISO filename to a VBox OS type, matching the script's
// ubuntu-vs-debian heuristic.
func OSType(isoName string) string {
	if strings.HasPrefix(isoName, "ubuntu-") {
		return "Ubuntu_64"
	}
	return "Debian_64"
}

// CreateVMArgs builds `VBoxManage createvm …`.
func CreateVMArgs(name, osType, baseDir string) []string {
	return []string{"createvm", "--name", name, "--ostype", osType, "--basefolder", baseDir, "--register"}
}

// ParseCreateVMUUID extracts the UUID from createvm output (a line "UUID: …").
func ParseCreateVMUUID(out string) (string, error) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "UUID:" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("no UUID found in createvm output")
}

// ModifyVMArgs builds `VBoxManage modifyvm …` with the script's fixed
// virtualization settings plus the spec's variable ones.
func ModifyVMArgs(uuid string, s VMSpec) []string {
	bridge := s.BridgeAdapter
	if bridge == "" {
		bridge = "eth0"
	}
	args := []string{
		"modifyvm", uuid,
		"--cpus", strconv.Itoa(s.CPUs),
		"--memory", strconv.Itoa(s.MemoryMB),
		"--vrdeport", strconv.Itoa(s.RDPPort),
		"--vrdeaddress", s.RDPAddress,
		"--acpi", "on", "--ioapic", "on", "--apic", "on", "--x2apic", "on",
		"--paravirtprovider", "kvm", "--hwvirtex", "on", "--nestedpaging", "on",
		"--largepages", "on", "--vtxvpid", "on", "--vtxux", "on", "--pae", "on",
		"--longmode", "on", "--rtcuseutc", "on",
		"--accelerate3d", "off", "--accelerate2dvideo", "off",
		"--firmware", "bios", "--bioslogofadein", "off", "--bioslogofadeout", "off",
		"--boot1", "dvd", "--boot2", "disk",
		"--nic1", "bridged", "--bridgeadapter1", bridge,
	}
	if s.MACAddress != "" {
		args = append(args, "--macaddress1", s.MACAddress)
	}
	args = append(args,
		"--audio", "none", "--clipboard", "disabled", "--draganddrop", "disabled",
		"--vrde", "on", "--vrdeauthtype", "external", "--defaultfrontend", "headless",
	)
	return args
}

// StorageCtlArgs builds the SATA controller creation args.
func StorageCtlArgs(uuid string) []string {
	return []string{
		"storagectl", uuid,
		"--name", SATAController,
		"--add", "sata", "--portcount", "2", "--hostiocache", "off", "--bootable", "on",
	}
}

// CreateMediumArgs builds `VBoxManage createmedium disk …`.
func CreateMediumArgs(filename string, sizeMB int) []string {
	return []string{"createmedium", "disk", "--filename", filename, "--size", strconv.Itoa(sizeMB)}
}

// AttachDiskArgs attaches a hard disk on SATA port 1.
func AttachDiskArgs(uuid, medium string) []string {
	return []string{
		"storageattach", uuid, "--storagectl", SATAController,
		"--port", "1", "--device", "0", "--type", "hdd", "--medium", medium,
	}
}

// AttachISOArgs attaches a DVD ISO on SATA port 2.
func AttachISOArgs(uuid, iso string) []string {
	return []string{
		"storageattach", uuid, "--storagectl", SATAController,
		"--port", "2", "--device", "0", "--type", "dvddrive", "--medium", iso,
	}
}

// StartVMArgs builds `VBoxManage startvm …`.
func StartVMArgs(uuid string) []string {
	return []string{"startvm", uuid}
}
