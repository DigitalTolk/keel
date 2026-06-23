package cli

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/DigitalTolk/keel/internal/vbox"
)

func newVboxCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vbox",
		Short: "Create VirtualBox guests",
	}
	cmd.AddCommand(newVboxCreateCmd(a))
	return cmd
}

func newVboxCreateCmd(a *app) *cobra.Command {
	var (
		name, baseDir, bridge string
		iso, mac, rdp         string
		cpus, memoryMB, disk  int
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create (and, when a disk is given, start) a VirtualBox guest",
		RunE: func(cmd *cobra.Command, args []string) error {
			if baseDir == "" || cpus == 0 || memoryMB == 0 || rdp == "" {
				return fmt.Errorf("--base, --cpus, --memory and --rdp are required")
			}
			rdpHost, rdpPortStr, err := net.SplitHostPort(rdp)
			if err != nil {
				return fmt.Errorf("--rdp must be IP:PORT: %w", err)
			}
			rdpPort, err := strconv.Atoi(rdpPortStr)
			if err != nil {
				return fmt.Errorf("--rdp port: %w", err)
			}
			if name == "" {
				name = "base"
			}

			if err := a.requireTools("VBoxManage"); err != nil {
				return err
			}
			spec := vbox.VMSpec{
				Name: name, OSType: vbox.OSType(iso), BaseDir: baseDir,
				CPUs: cpus, MemoryMB: memoryMB, RDPAddress: rdpHost, RDPPort: rdpPort,
				BridgeAdapter: bridge, MACAddress: mac,
			}
			return a.createVBox(cmd.Context(), spec, iso, disk)
		},
	}
	cmd.Flags().StringVarP(&baseDir, "base", "b", "", "base folder (required)")
	cmd.Flags().StringVarP(&bridge, "bridge", "B", "eth0", "bridge adapter")
	cmd.Flags().IntVarP(&cpus, "cpus", "c", 0, "number of vCPUs (required)")
	cmd.Flags().StringVarP(&iso, "iso", "i", "", "path to boot ISO")
	cmd.Flags().StringVarP(&rdp, "rdp", "l", "", "RDP IP:PORT (required)")
	cmd.Flags().IntVarP(&memoryMB, "memory", "m", 0, "memory in MB (required)")
	cmd.Flags().StringVarP(&mac, "mac", "M", "", "MAC address")
	cmd.Flags().StringVarP(&name, "name", "n", "", "vbox name (default base)")
	cmd.Flags().IntVarP(&disk, "disk", "s", 0, "disk size in MB (also starts the VM)")
	return cmd
}

// createVBox runs the VBoxManage sequence: create the VM, parse its UUID, then
// modify/storage/attach/start. Separated from the command so it is testable
// with a fake runner regardless of whether VBoxManage is installed.
func (a *app) createVBox(ctx context.Context, spec vbox.VMSpec, iso string, disk int) error {
	r := a.runnerFactory()

	var createOut bytes.Buffer
	if err := r.Stream(ctx, &createOut, "VBoxManage", vbox.CreateVMArgs(spec.Name, spec.OSType, spec.BaseDir)...); err != nil {
		return err
	}
	uuid, err := vbox.ParseCreateVMUUID(createOut.String())
	if err != nil {
		return err
	}
	a.log.Info(fmt.Sprintf("created vbox %s (%s)", spec.Name, uuid))

	steps := [][]string{
		vbox.ModifyVMArgs(uuid, spec),
		vbox.StorageCtlArgs(uuid),
	}
	if disk > 0 {
		medium := fmt.Sprintf("%s/%s/%s.vdi", spec.BaseDir, spec.Name, spec.Name)
		steps = append(steps, vbox.CreateMediumArgs(medium, disk), vbox.AttachDiskArgs(uuid, medium))
	}
	if iso != "" {
		steps = append(steps, vbox.AttachISOArgs(uuid, iso))
	}
	if disk > 0 {
		steps = append(steps, vbox.StartVMArgs(uuid))
	}
	for _, step := range steps {
		if err := r.Stream(ctx, &bytes.Buffer{}, "VBoxManage", step...); err != nil {
			return err
		}
	}

	a.log.Success(fmt.Sprintf("vbox %s ready", spec.Name))
	return nil
}
