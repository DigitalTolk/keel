package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// SGClient manages a security group's TCP ingress rules for a single port.
type SGClient interface {
	HasIngress(ctx context.Context, sgID string, port int, cidr string) (bool, error)
	Authorize(ctx context.Context, sgID string, port int, cidr string) error
	Revoke(ctx context.Context, sgID string, port int, cidr string) error
}

// SGOptions configures the EC2 security-group client.
type SGOptions struct {
	Region          string
	Profile         string
	Endpoint        string // for tests / LocalStack
	AccessKeyID     string
	SecretAccessKey string
}

type ec2SGClient struct{ client *ec2.Client }

// NewSecurityGroupClient builds an EC2-backed SGClient.
func NewSecurityGroupClient(ctx context.Context, opts SGOptions) (SGClient, error) {
	cfg, err := awsConfig(ctx, opts.Region, opts.Profile, opts.AccessKeyID, opts.SecretAccessKey)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	client := ec2.NewFromConfig(cfg, func(o *ec2.Options) {
		if opts.Endpoint != "" {
			o.BaseEndpoint = aws.String(opts.Endpoint)
		}
	})
	return &ec2SGClient{client: client}, nil
}

func tcpPermission(port int, cidr string) ec2types.IpPermission {
	return ec2types.IpPermission{
		IpProtocol: aws.String("tcp"),
		FromPort:   aws.Int32(int32(port)),
		ToPort:     aws.Int32(int32(port)),
		IpRanges:   []ec2types.IpRange{{CidrIp: aws.String(cidr)}},
	}
}

func (c *ec2SGClient) HasIngress(ctx context.Context, sgID string, port int, cidr string) (bool, error) {
	out, err := c.client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{GroupIds: []string{sgID}})
	if err != nil {
		return false, fmt.Errorf("describe security group %s: %w", sgID, err)
	}
	return matchIngressCIDR(out.SecurityGroups, port, cidr), nil
}

// matchIngressCIDR reports whether any group has a TCP rule covering port that
// already allows cidr. Pure, so it is unit-tested without a live EC2 endpoint.
func matchIngressCIDR(groups []ec2types.SecurityGroup, port int, cidr string) bool {
	for _, g := range groups {
		for _, p := range g.IpPermissions {
			if aws.ToString(p.IpProtocol) != "tcp" {
				continue
			}
			if aws.ToInt32(p.FromPort) <= int32(port) && int32(port) <= aws.ToInt32(p.ToPort) {
				for _, r := range p.IpRanges {
					if aws.ToString(r.CidrIp) == cidr {
						return true
					}
				}
			}
		}
	}
	return false
}

func (c *ec2SGClient) Authorize(ctx context.Context, sgID string, port int, cidr string) error {
	_, err := c.client.AuthorizeSecurityGroupIngress(ctx, &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId:       aws.String(sgID),
		IpPermissions: []ec2types.IpPermission{tcpPermission(port, cidr)},
	})
	if err != nil {
		return fmt.Errorf("authorize %s on %s:%d: %w", cidr, sgID, port, err)
	}
	return nil
}

func (c *ec2SGClient) Revoke(ctx context.Context, sgID string, port int, cidr string) error {
	_, err := c.client.RevokeSecurityGroupIngress(ctx, &ec2.RevokeSecurityGroupIngressInput{
		GroupId:       aws.String(sgID),
		IpPermissions: []ec2types.IpPermission{tcpPermission(port, cidr)},
	})
	if err != nil {
		return fmt.Errorf("revoke %s on %s:%d: %w", cidr, sgID, port, err)
	}
	return nil
}

var _ SGClient = (*ec2SGClient)(nil)

// UpdateSecurityGroups brings each security group's ingress for port in line
// with currentIP: it revokes lastIP/32 where present (when the IP changed) and
// authorizes currentIP/32 where missing. Mirrors dyn_ingress.sh.
func UpdateSecurityGroups(ctx context.Context, c SGClient, sgIDs []string, port int, currentIP, lastIP string) error {
	current := currentIP + "/32"
	for _, sg := range sgIDs {
		if lastIP != "" && lastIP != currentIP {
			last := lastIP + "/32"
			has, err := c.HasIngress(ctx, sg, port, last)
			if err != nil {
				return err
			}
			if has {
				if err := c.Revoke(ctx, sg, port, last); err != nil {
					return err
				}
			}
		}
		has, err := c.HasIngress(ctx, sg, port, current)
		if err != nil {
			return err
		}
		if !has {
			if err := c.Authorize(ctx, sg, port, current); err != nil {
				return err
			}
		}
	}
	return nil
}
