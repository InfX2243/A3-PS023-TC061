// Package ec2 provides a small adapter around the AWS CLI. Credentials are
// resolved by the CLI provider chain and are never accepted from HTTP forms.
package ec2

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Client struct { Binary string; Region string; Timeout time.Duration }
type LaunchInput struct {
	Name string
	Region string
	ImageID string
	InstanceType string
	SubnetID string
	SecurityGroupIDs []string
	KeyName string
	InstanceProfile string
	ClientToken string
}
type Instance struct {
	ID string `json:"InstanceId"`
	Type string `json:"InstanceType"`
	State struct { Name string `json:"Name"` } `json:"State"`
	PublicIP string `json:"PublicIpAddress"`
	PrivateIP string `json:"PrivateIpAddress"`
	Placement struct { AvailabilityZone string `json:"AvailabilityZone"` } `json:"Placement"`
}

func (c Client) region(override string) string {
	if strings.TrimSpace(override) != "" { return strings.TrimSpace(override) }
	if strings.TrimSpace(c.Region) != "" { return strings.TrimSpace(c.Region) }
	return ""
}
func (c Client) run(ctx context.Context, region string, args ...string) ([]byte,error) {
	binary:=c.Binary; if binary=="" { binary="aws" }
	timeout:=c.Timeout; if timeout<=0 { timeout=45*time.Second }
	ctx,cancel:=context.WithTimeout(ctx,timeout); defer cancel()
	base:=[]string{"--no-cli-pager","--output","json"}
	if region!="" { base=append(base,"--region",region) }
	cmd:=exec.CommandContext(ctx,binary,append(args,base...)...)
	var stdout,stderr bytes.Buffer; cmd.Stdout=&stdout; cmd.Stderr=&stderr
	if err:=cmd.Run(); err!=nil {
		if ctx.Err()!=nil { return nil,fmt.Errorf("AWS CLI request failed: %w",ctx.Err()) }
		msg:=strings.TrimSpace(stderr.String()); if len(msg)>1200 { msg=msg[:1200] }
		return nil,fmt.Errorf("AWS CLI request failed: %s",msg)
	}
	if stdout.Len()>2<<20 { return nil,errors.New("AWS response exceeded 2 MiB") }
	return stdout.Bytes(),nil
}
func (c Client) Launch(ctx context.Context,in LaunchInput) (Instance,error) {
	in.Name=strings.TrimSpace(in.Name); in.Region=c.region(in.Region)
	if in.Name=="" || len(in.Name)>128 { return Instance{},errors.New("instance name must be 1–128 characters") }
	for _,r:=range in.Name { if !(r>='a'&&r<='z'||r>='A'&&r<='Z'||r>='0'&&r<='9'||r=='-'||r=='_'||r=='.'||r==' ') { return Instance{},errors.New("instance name may contain only letters, numbers, spaces, dot, dash, and underscore") } }
	if in.Region=="" || len(in.Region)>32 || strings.ContainsAny(in.Region,"/\\r\\n ") { return Instance{},errors.New("a valid AWS region is required") }
	if !validID(in.ImageID,"ami-") || !validID(in.SubnetID,"subnet-") { return Instance{},errors.New("a valid AMI ID and subnet ID are required") }
	if in.InstanceType!="t3.micro" && in.InstanceType!="t3.small" { return Instance{},errors.New("only t3.micro and t3.small are enabled for the initial test workflow") }
	if len(in.SecurityGroupIDs)==0 || len(in.SecurityGroupIDs)>5 { return Instance{},errors.New("provide between one and five preconfigured security groups") }
	for _,id:=range in.SecurityGroupIDs { if !validID(id,"sg-") { return Instance{},errors.New("security group IDs must be valid sg- identifiers") } }
	if in.KeyName!="" && (len(in.KeyName)>255 || strings.ContainsAny(in.KeyName,"\\r\\n")) { return Instance{},errors.New("invalid EC2 key pair name") }
	if in.ClientToken=="" || len(in.ClientToken)>64 { return Instance{},errors.New("a stable client token is required for idempotent launch") }
	if in.InstanceProfile!="" && (len(in.InstanceProfile)>128 || strings.ContainsAny(in.InstanceProfile,"\\r\\n")) { return Instance{},errors.New("invalid IAM instance profile name") }
	tags,_:=json.Marshal([]map[string]any{{"ResourceType":"instance","Tags":[]map[string]string{{"Key":"Name","Value":in.Name},{"Key":"ManagedBy","Value":"aiori-controller"},{"Key":"Role","Value":"worker-connectivity-test"}}}})
	args:=[]string{"ec2","run-instances","--image-id",in.ImageID,"--instance-type",in.InstanceType,"--subnet-id",in.SubnetID,"--min-count","1","--max-count","1","--tag-specifications",string(tags)}
	args=append(args,"--client-token",in.ClientToken)
	args=append(args,"--security-group-ids"); args=append(args,in.SecurityGroupIDs...)
	if in.KeyName!="" { args=append(args,"--key-name",in.KeyName) }
	if in.InstanceProfile!="" { args=append(args,"--iam-instance-profile","Name="+in.InstanceProfile) }
	raw,err:=c.run(ctx,in.Region,args...); if err!=nil{return Instance{},err}
	var response struct { Instances []Instance `json:"Instances"` }
	if err:=json.Unmarshal(raw,&response); err!=nil{return Instance{},fmt.Errorf("decode RunInstances response: %w",err)}
	if len(response.Instances)!=1 || response.Instances[0].ID=="" { return Instance{},errors.New("AWS did not return exactly one instance ID") }
	return response.Instances[0],nil
}
func (c Client) Describe(ctx context.Context,region,id string) (Instance,error) {
	region=c.region(region); if region=="" || !validID(id,"i-") { return Instance{},errors.New("valid region and EC2 instance ID are required") }
	raw,err:=c.run(ctx,region,"ec2","describe-instances","--instance-ids",id); if err!=nil{return Instance{},err}
	var response struct { Reservations []struct { Instances []Instance `json:"Instances"` } `json:"Reservations"` }
	if err:=json.Unmarshal(raw,&response);err!=nil{return Instance{},fmt.Errorf("decode DescribeInstances response: %w",err)}
	for _,r:=range response.Reservations { if len(r.Instances)>0{return r.Instances[0],nil} }
	return Instance{},errors.New("EC2 instance was not returned")
}
func (c Client) Action(ctx context.Context,region,id,action string) error {
	region=c.region(region); if region=="" || !validID(id,"i-") { return errors.New("valid region and EC2 instance ID are required") }
	switch action { case "start","stop": default:return errors.New("unsupported EC2 lifecycle action") }
	_,err:=c.run(ctx,region,"ec2",action+"-instances","--instance-ids",id); return err
}
func validID(v,prefix string) bool {
	if !strings.HasPrefix(v,prefix)||len(v)<=len(prefix)||len(v)>64{return false}
	for _,r:=range v[len(prefix):] { if !(r>='a'&&r<='z'||r>='A'&&r<='Z'||r>='0'&&r<='9'||r=='-'){return false} }
	return true
}
