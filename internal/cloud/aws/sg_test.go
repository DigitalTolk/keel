package aws

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// fakeSG records calls and serves canned HasIngress answers.
type fakeSG struct {
	has        map[string]bool // keyed by "sg|cidr"
	authorized []string
	revoked    []string
	failHas    bool
}

func (f *fakeSG) HasIngress(_ context.Context, sg string, _ int, cidr string) (bool, error) {
	if f.failHas {
		return false, errors.New("describe failed")
	}
	return f.has[sg+"|"+cidr], nil
}
func (f *fakeSG) Authorize(_ context.Context, sg string, _ int, cidr string) error {
	f.authorized = append(f.authorized, sg+"|"+cidr)
	return nil
}
func (f *fakeSG) Revoke(_ context.Context, sg string, _ int, cidr string) error {
	f.revoked = append(f.revoked, sg+"|"+cidr)
	return nil
}

func TestUpdateSecurityGroupsNewIP(t *testing.T) {
	f := &fakeSG{has: map[string]bool{}}
	if err := UpdateSecurityGroups(context.Background(), f, []string{"sg-1"}, 22, "1.2.3.4", ""); err != nil {
		t.Fatal(err)
	}
	if len(f.revoked) != 0 {
		t.Errorf("no revoke expected with no last IP, got %v", f.revoked)
	}
	if len(f.authorized) != 1 || f.authorized[0] != "sg-1|1.2.3.4/32" {
		t.Errorf("authorized = %v, want [sg-1|1.2.3.4/32]", f.authorized)
	}
}

func TestUpdateSecurityGroupsChangedIPRevokesOld(t *testing.T) {
	f := &fakeSG{has: map[string]bool{"sg-1|9.9.9.9/32": true}} // old IP present, new absent
	if err := UpdateSecurityGroups(context.Background(), f, []string{"sg-1"}, 443, "1.2.3.4", "9.9.9.9"); err != nil {
		t.Fatal(err)
	}
	if len(f.revoked) != 1 || f.revoked[0] != "sg-1|9.9.9.9/32" {
		t.Errorf("revoked = %v, want old IP", f.revoked)
	}
	if len(f.authorized) != 1 || f.authorized[0] != "sg-1|1.2.3.4/32" {
		t.Errorf("authorized = %v, want new IP", f.authorized)
	}
}

func TestUpdateSecurityGroupsCurrentAlreadyPresent(t *testing.T) {
	f := &fakeSG{has: map[string]bool{"sg-1|1.2.3.4/32": true}}
	if err := UpdateSecurityGroups(context.Background(), f, []string{"sg-1"}, 22, "1.2.3.4", "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	if len(f.authorized) != 0 {
		t.Errorf("no authorize expected when current already present, got %v", f.authorized)
	}
}

func TestUpdateSecurityGroupsErrorPropagates(t *testing.T) {
	f := &fakeSG{failHas: true}
	if err := UpdateSecurityGroups(context.Background(), f, []string{"sg-1"}, 22, "1.2.3.4", ""); err == nil {
		t.Fatal("describe error should propagate")
	}
}

func TestMatchIngressCIDR(t *testing.T) {
	groups := []ec2types.SecurityGroup{{
		IpPermissions: []ec2types.IpPermission{
			{
				IpProtocol: aws.String("tcp"),
				FromPort:   aws.Int32(20),
				ToPort:     aws.Int32(25),
				IpRanges:   []ec2types.IpRange{{CidrIp: aws.String("1.2.3.4/32")}},
			},
			{
				IpProtocol: aws.String("udp"), // ignored
				FromPort:   aws.Int32(53),
				ToPort:     aws.Int32(53),
				IpRanges:   []ec2types.IpRange{{CidrIp: aws.String("9.9.9.9/32")}},
			},
		},
	}}

	if !matchIngressCIDR(groups, 22, "1.2.3.4/32") {
		t.Error("expected match for tcp 20-25 covering port 22 with cidr")
	}
	if matchIngressCIDR(groups, 22, "5.6.7.8/32") {
		t.Error("different cidr should not match")
	}
	if matchIngressCIDR(groups, 80, "1.2.3.4/32") {
		t.Error("port outside range should not match")
	}
	if matchIngressCIDR(groups, 53, "9.9.9.9/32") {
		t.Error("non-tcp protocol should not match")
	}
}

func TestNewSecurityGroupClientConstructs(t *testing.T) {
	c, err := NewSecurityGroupClient(context.Background(), SGOptions{
		Region: "us-east-1", Endpoint: "http://127.0.0.1:1", AccessKeyID: "k", SecretAccessKey: "s",
	})
	if err != nil || c == nil {
		t.Fatalf("construct: %v", err)
	}
}

func TestEC2SGClientErrorPaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := &ec2SGClient{client: ec2.NewFromConfig(aws.Config{Region: "us-east-1", RetryMaxAttempts: 1}, func(o *ec2.Options) {
		o.BaseEndpoint = aws.String("http://127.0.0.1:1")
		o.Credentials = staticCreds{}
	})}

	if _, err := c.HasIngress(ctx, "sg-1", 22, "1.2.3.4/32"); err == nil {
		t.Error("HasIngress should fail against a dead endpoint")
	}
	if err := c.Authorize(ctx, "sg-1", 22, "1.2.3.4/32"); err == nil {
		t.Error("Authorize should fail against a dead endpoint")
	}
	if err := c.Revoke(ctx, "sg-1", 22, "1.2.3.4/32"); err == nil {
		t.Error("Revoke should fail against a dead endpoint")
	}
}
