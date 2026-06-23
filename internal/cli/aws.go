package cli

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	awscloud "github.com/DigitalTolk/keel/internal/cloud/aws"
)

const defaultIPEchoURL = "https://checkip.amazonaws.com"

func newAWSCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aws",
		Short: "AWS operations",
	}
	cmd.AddCommand(newSGIngressCmd(a))
	return cmd
}

func newSGIngressCmd(a *app) *cobra.Command {
	var (
		sgIDs                     []string
		port                      int
		profile, region, endpoint string
		ipURL                     string
		force                     bool
	)
	cmd := &cobra.Command{
		Use:   "sg-ingress",
		Short: "Authorize this machine's current public IP on security group(s), revoking the previous one",
		RunE: func(cmd *cobra.Command, args []string) error {
			if profile == "" {
				profile = firstNonEmpty(os.Getenv("AWS_PROFILE"), "default")
			}
			if len(sgIDs) == 0 {
				return fmt.Errorf("at least one --security-group is required")
			}
			if port == 0 {
				return fmt.Errorf("--port is required")
			}
			if ipURL == "" {
				ipURL = defaultIPEchoURL
			}

			currentIP, err := a.fetchIP(cmd.Context(), ipURL)
			if err != nil {
				return err
			}
			statePath, err := sgIngressStatePath(profile, sgIDs, port)
			if err != nil {
				return err
			}
			lastIP := readLastIP(statePath)

			if currentIP == lastIP && !force {
				a.log.Info(fmt.Sprintf("public IP unchanged (%s); nothing to do", currentIP))
				return nil
			}

			client, err := a.sgClientFactory(cmd.Context(), awscloud.SGOptions{Region: region, Profile: profile, Endpoint: endpoint})
			if err != nil {
				return err
			}
			if err := awscloud.UpdateSecurityGroups(cmd.Context(), client, sgIDs, port, currentIP, lastIP); err != nil {
				return err
			}
			if err := writeLastIP(statePath, currentIP); err != nil {
				return err
			}
			a.log.Success(fmt.Sprintf("authorized %s on port %d across %d group(s)", currentIP, port, len(sgIDs)))
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&sgIDs, "security-group", "l", nil, "security group id (repeatable, required)")
	cmd.Flags().IntVarP(&port, "port", "p", 0, "tcp port (required)")
	cmd.Flags().StringVarP(&profile, "profile", "u", "", "aws profile (AWS_PROFILE, default \"default\")")
	cmd.Flags().StringVar(&region, "region", "", "aws region")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "custom EC2 endpoint (testing)")
	cmd.Flags().StringVar(&ipURL, "ip-url", "", "public IP echo URL (default "+defaultIPEchoURL+")")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "update even if the IP hasn't changed")
	return cmd
}

// fetchPublicIP GETs url and returns the trimmed, validated IP it returns.
func fetchPublicIP(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch public ip: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch public ip: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(string(body))
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("fetch public ip: %q is not a valid IP", ip)
	}
	return ip, nil
}

// sgIngressStatePath returns a per-(profile, groups, port) state file path,
// matching the original script's lastip_<profile>_<hash> scheme.
func sgIngressStatePath(profile string, sgIDs []string, port int) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	sum := md5.Sum([]byte(strings.Join(sgIDs, ",") + "-" + strconv.Itoa(port)))
	name := fmt.Sprintf("lastip_%s_%x", profile, sum)
	return filepath.Join(home, ".config", "keel", "sg-ingress", name), nil
}

func readLastIP(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func writeLastIP(path, ip string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(ip), 0o600)
}
