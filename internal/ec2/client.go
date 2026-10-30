// Package ec2 provides an AWS SDK for Go v2 adapter for EC2 provisioning.
package ec2

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type API interface {
	RunInstances(context.Context, *ec2.RunInstancesInput, ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error)
	DescribeInstances(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	StartInstances(context.Context, *ec2.StartInstancesInput, ...func(*ec2.Options)) (*ec2.StartInstancesOutput, error)
	StopInstances(context.Context, *ec2.StopInstancesInput, ...func(*ec2.Options)) (*ec2.StopInstancesOutput, error)
}

type Client struct {
	Region string
	API    API
}

type LaunchInput struct {
	Name, Region, ImageID, InstanceType, SubnetID string
	SecurityGroupIDs                              []string
	KeyName, InstanceProfile, ClientToken         string
	UserData                                      string
}

type Instance struct {
	ID        string
	Name      string
	ImageID   string
	Type      string
	State     struct{ Name string }
	StateReason string
	PublicIP  string
	PrivateIP string
	VPCID     string
	SubnetID  string
	KeyName   string
	LaunchTime time.Time
	SecurityGroups []string
	Placement struct{ AvailabilityZone string }
}

var regionPattern = regexp.MustCompile(`^[a-z]{2}(-[a-z0-9]+){1,2}-[0-9]+$`)

func (c Client) region(override string) string {
	if v := strings.TrimSpace(override); v != "" {
		return v
	}
	return strings.TrimSpace(c.Region)
}

func (c Client) sdk(ctx context.Context, region string) (API, error) {
	if c.API != nil {
		return c.API, nil
	}
	if region == "" || !regionPattern.MatchString(region) {
		return nil, errors.New("a valid AWS region is required")
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS SDK configuration: %w", err)
	}
	return ec2.NewFromConfig(cfg), nil
}

func (c Client) Launch(ctx context.Context, in LaunchInput) (Instance, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Region = c.region(in.Region)
	if in.Name == "" || len(in.Name) > 128 {
		return Instance{}, errors.New("instance name must be 1–128 characters")
	}
	for _, r := range in.Name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == ' ') {
			return Instance{}, errors.New("instance name may contain only letters, numbers, spaces, dot, dash, and underscore")
		}
	}
	if !regionPattern.MatchString(in.Region) {
		return Instance{}, errors.New("a valid AWS region is required")
	}
	if !validID(in.ImageID, "ami-") {
		return Instance{}, errors.New("a valid AMI ID is required")
	}
	if in.SubnetID != "" && !validID(in.SubnetID, "subnet-") {
		return Instance{}, errors.New("subnet ID must be a valid subnet- identifier when provided")
	}
	if in.InstanceType != "t3.micro" && in.InstanceType != "t3.small" {
		return Instance{}, errors.New("only t3.micro and t3.small are enabled for the initial test workflow")
	}
	if len(in.SecurityGroupIDs) > 5 {
		return Instance{}, errors.New("provide no more than five security groups")
	}
	for _, id := range in.SecurityGroupIDs {
		if !validID(id, "sg-") {
			return Instance{}, errors.New("security group IDs must be valid sg- identifiers")
		}
	}
	if in.KeyName != "" && (len(in.KeyName) > 255 || strings.ContainsAny(in.KeyName, "\r\n")) {
		return Instance{}, errors.New("invalid EC2 key pair name")
	}
	if in.ClientToken == "" || len(in.ClientToken) > 64 {
		return Instance{}, errors.New("a stable client token is required for idempotent launch")
	}
	if in.InstanceProfile != "" && (len(in.InstanceProfile) > 128 || strings.ContainsAny(in.InstanceProfile, "\r\n")) {
		return Instance{}, errors.New("invalid IAM instance profile name")
	}

	api, err := c.sdk(ctx, in.Region)
	if err != nil {
		return Instance{}, err
	}
	input := &ec2.RunInstancesInput{
		ImageId:          aws.String(in.ImageID),
		InstanceType:     types.InstanceType(in.InstanceType),
		MinCount:         aws.Int32(1),
		MaxCount:         aws.Int32(1),
		ClientToken:      aws.String(in.ClientToken),
		TagSpecifications: []types.TagSpecification{{
			ResourceType: types.ResourceTypeInstance,
			Tags: []types.Tag{
				{Key: aws.String("Name"), Value: aws.String(in.Name)},
				{Key: aws.String("ManagedBy"), Value: aws.String("aiori-controller")},
				{Key: aws.String("Role"), Value: aws.String("worker-connectivity-test")},
			},
		}},
	}
	if in.UserData != "" { input.UserData = aws.String(base64.StdEncoding.EncodeToString([]byte(in.UserData))) }
	// If network IDs are omitted, EC2 uses the default subnet and security group in the default VPC.
	if in.SubnetID != "" { input.SubnetId = aws.String(in.SubnetID) }
	if len(in.SecurityGroupIDs) > 0 { input.SecurityGroupIds = in.SecurityGroupIDs }
	if in.KeyName != "" {
		input.KeyName = aws.String(in.KeyName)
	}
	if in.InstanceProfile != "" {
		input.IamInstanceProfile = &types.IamInstanceProfileSpecification{Name: aws.String(in.InstanceProfile)}
	}
	out, err := api.RunInstances(ctx, input)
	if err != nil {
		return Instance{}, fmt.Errorf("AWS EC2 RunInstances: %w", err)
	}
	if out == nil || len(out.Instances) != 1 || aws.ToString(out.Instances[0].InstanceId) == "" {
		return Instance{}, errors.New("AWS did not return exactly one instance ID")
	}
	return fromSDK(out.Instances[0]), nil
}

func (c Client) Describe(ctx context.Context, region, id string) (Instance, error) {
	region = c.region(region)
	if !regionPattern.MatchString(region) || !validID(id, "i-") {
		return Instance{}, errors.New("valid region and EC2 instance ID are required")
	}
	api, err := c.sdk(ctx, region)
	if err != nil {
		return Instance{}, err
	}
	out, err := api.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{id}})
	if err != nil {
		return Instance{}, fmt.Errorf("AWS EC2 DescribeInstances: %w", err)
	}
	if out != nil {
		for _, reservation := range out.Reservations {
			for _, instance := range reservation.Instances {
				return fromSDK(instance), nil
			}
		}
	}
	return Instance{}, errors.New("EC2 instance was not returned")
}

// ListManaged returns every instance tagged as managed by this controller in the
// selected region. It deliberately does not list unrelated account resources.
func (c Client) ListManaged(ctx context.Context, region string) ([]Instance, error) {
	region = c.region(region)
	if !regionPattern.MatchString(region) {
		return nil, errors.New("a valid AWS region is required")
	}
	api, err := c.sdk(ctx, region)
	if err != nil { return nil, err }
	var result []Instance
	var token *string
	for {
		out, err := api.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			Filters: []types.Filter{{Name: aws.String("tag:ManagedBy"), Values: []string{"aiori-controller"}}},
			NextToken: token,
		})
		if err != nil { return nil, fmt.Errorf("AWS EC2 ListManagedInstances: %w", err) }
		if out == nil { break }
		for _, reservation := range out.Reservations {
			for _, raw := range reservation.Instances {
				result = append(result, fromSDK(raw))
			}
		}
		if aws.ToString(out.NextToken) == "" { break }
		token = out.NextToken
	}
	return result, nil
}

func (c Client) Action(ctx context.Context, region, id, action string) error {
	region = c.region(region)
	if !regionPattern.MatchString(region) || !validID(id, "i-") {
		return errors.New("valid region and EC2 instance ID are required")
	}
	api, err := c.sdk(ctx, region)
	if err != nil {
		return err
	}
	switch action {
	case "start":
		_, err = api.StartInstances(ctx, &ec2.StartInstancesInput{InstanceIds: []string{id}})
	case "stop":
		_, err = api.StopInstances(ctx, &ec2.StopInstancesInput{InstanceIds: []string{id}})
	default:
		return errors.New("unsupported EC2 lifecycle action")
	}
	if err != nil {
		return fmt.Errorf("AWS EC2 %s-instances: %w", action, err)
	}
	return nil
}

func fromSDK(v types.Instance) Instance {
	var out Instance
	out.ID, out.Type = aws.ToString(v.InstanceId), string(v.InstanceType)
	out.ImageID, out.KeyName = aws.ToString(v.ImageId), aws.ToString(v.KeyName)
	out.VPCID, out.SubnetID = aws.ToString(v.VpcId), aws.ToString(v.SubnetId)
	for _, tag := range v.Tags {
		if aws.ToString(tag.Key) == "Name" { out.Name = aws.ToString(tag.Value) }
	}
	for _, group := range v.SecurityGroups { if aws.ToString(group.GroupId) != "" { out.SecurityGroups = append(out.SecurityGroups, aws.ToString(group.GroupId)) } }
	if v.LaunchTime != nil { out.LaunchTime = v.LaunchTime.UTC() }
	if v.StateReason != nil { out.StateReason = aws.ToString(v.StateReason.Message) }
	if v.State != nil {
		out.State.Name = string(v.State.Name)
	}
	out.PublicIP, out.PrivateIP = aws.ToString(v.PublicIpAddress), aws.ToString(v.PrivateIpAddress)
	if v.Placement != nil {
		out.Placement.AvailabilityZone = aws.ToString(v.Placement.AvailabilityZone)
	}
	return out
}

func validID(v, prefix string) bool {
	if !strings.HasPrefix(v, prefix) || len(v) <= len(prefix) || len(v) > 64 {
		return false
	}
	for _, r := range v[len(prefix):] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}
