package ec2

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type fakeAPI struct {
	run *ec2.RunInstancesInput
	runOut *ec2.RunInstancesOutput
	describeOut *ec2.DescribeInstancesOutput
	describeInputs []*ec2.DescribeInstancesInput
	started, stopped []string
}
func (f *fakeAPI) RunInstances(_ context.Context, in *ec2.RunInstancesInput, _ ...func(*ec2.Options)) (*ec2.RunInstancesOutput,error) { f.run=in; return f.runOut,nil }
func (f *fakeAPI) DescribeInstances(_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput,error) { f.describeInputs=append(f.describeInputs,in); return f.describeOut,nil }
func (f *fakeAPI) StartInstances(_ context.Context, in *ec2.StartInstancesInput, _ ...func(*ec2.Options)) (*ec2.StartInstancesOutput,error) { f.started=in.InstanceIds; return &ec2.StartInstancesOutput{},nil }
func (f *fakeAPI) StopInstances(_ context.Context, in *ec2.StopInstancesInput, _ ...func(*ec2.Options)) (*ec2.StopInstancesOutput,error) { f.stopped=in.InstanceIds; return &ec2.StopInstancesOutput{},nil }

func TestValidID(t *testing.T) {
	for _, tc := range []struct{v,p string; want bool}{{"ami-123","ami-",true},{"subnet-ab12","subnet-",true},{"sg-ab12","sg-",true},{"ami-","ami-",false},{"ami-abc/def","ami-",false},{"i-123","i-",true}} {
		if got:=validID(tc.v,tc.p); got!=tc.want { t.Errorf("validID(%q,%q)=%v want %v",tc.v,tc.p,got,tc.want) }
	}
}

func TestLaunchUsesSDKAndCreatesExactlyOneTaggedInstance(t *testing.T) {
	f:=&fakeAPI{runOut:&ec2.RunInstancesOutput{Instances:[]types.Instance{{InstanceId:aws.String("i-abc123"),InstanceType:types.InstanceTypeT3Micro,State:&types.InstanceState{Name:types.InstanceStateNamePending}}}}}
	c:=Client{Region:"ap-south-1",API:f}
	got,err:=c.Launch(context.Background(),LaunchInput{Name:"demo-worker",ImageID:"ami-123",InstanceType:"t3.micro",ClientToken:"stable-token"})
	if err!=nil { t.Fatal(err) }
	if got.ID!="i-abc123" || got.State.Name!="pending" { t.Fatalf("unexpected instance: %#v",got) }
	if aws.ToInt32(f.run.MinCount)!=1 || aws.ToInt32(f.run.MaxCount)!=1 || aws.ToString(f.run.ClientToken)!="stable-token" { t.Fatal("launch was not constrained to one idempotent instance") }
	if len(f.run.TagSpecifications)!=1 || len(f.run.TagSpecifications[0].Tags)!=3 { t.Fatal("expected managed instance tags") }
	if f.run.IamInstanceProfile!=nil { t.Fatal("unexpected instance profile") }
	if f.run.SubnetId != nil || len(f.run.SecurityGroupIds) != 0 { t.Fatal("expected default VPC networking") }
}

func TestLaunchRejectsUnsafeInputBeforeCallingAWS(t *testing.T) {
	f:=&fakeAPI{}
	c:=Client{API:f}
	base:=LaunchInput{Name:"test-node",Region:"ap-south-1",ImageID:"ami-123",InstanceType:"t3.micro",SubnetID:"subnet-123",SecurityGroupIDs:[]string{"sg-123"},ClientToken:"form-token"}
	cases:=[]struct{name string; mutate func(*LaunchInput)}{
		{"unapproved instance type",func(v *LaunchInput){v.InstanceType="c8i.xlarge"}},
		{"missing client token",func(v *LaunchInput){v.ClientToken=""}},
		{"invalid security group",func(v *LaunchInput){v.SecurityGroupIDs=[]string{"sg-bad/evil"}}},
		{"invalid AMI",func(v *LaunchInput){v.ImageID="ami-;touch"}},
		{"invalid subnet",func(v *LaunchInput){v.SubnetID="subnet-bad/evil"}},
		{"unsafe name",func(v *LaunchInput){v.Name="node;bad"}},
	}
	for _,tc:=range cases { t.Run(tc.name,func(t *testing.T){ in:=base; tc.mutate(&in); if _,err:=c.Launch(context.Background(),in);err==nil { t.Fatal("expected validation error") }; if f.run!=nil { t.Fatal("AWS API called for invalid input") } }) }
}

func TestDescribeAndActionsUseSDK(t *testing.T) {
	f:=&fakeAPI{describeOut:&ec2.DescribeInstancesOutput{Reservations:[]types.Reservation{{Instances:[]types.Instance{{InstanceId:aws.String("i-123"),PrivateIpAddress:aws.String("10.0.0.2"),Placement:&types.Placement{AvailabilityZone:aws.String("ap-south-1a")}}}}}}}
	c:=Client{Region:"ap-south-1",API:f}
	got,err:=c.Describe(context.Background(),"","i-123")
	if err!=nil || got.PrivateIP!="10.0.0.2" || got.Placement.AvailabilityZone!="ap-south-1a" { t.Fatalf("describe=%#v err=%v",got,err) }
	if err:=c.Action(context.Background(),"","i-123","start"); err!=nil { t.Fatal(err) }
	if err:=c.Action(context.Background(),"","i-123","stop"); err!=nil { t.Fatal(err) }
	if len(f.started)!=1 || f.started[0]!="i-123" || len(f.stopped)!=1 { t.Fatal("SDK lifecycle API was not called") }
	if err:=c.Action(context.Background(),"","i-123","terminate"); err==nil { t.Fatal("terminate must remain unavailable") }
}

func TestLaunchRequiresClientToken(t *testing.T) {
	_,err:=(Client{API:&fakeAPI{}}).Launch(context.Background(),LaunchInput{Name:"x",Region:"ap-south-1",ImageID:"ami-1",InstanceType:"t3.micro",SubnetID:"subnet-1",SecurityGroupIDs:[]string{"sg-1"}})
	if err==nil { t.Fatal("expected missing token error") }
}

func TestListManagedFiltersControllerOwnedInstancesAndMapsDetails(t *testing.T) {
	launched := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	f := &fakeAPI{describeOut: &ec2.DescribeInstancesOutput{Reservations: []types.Reservation{{Instances: []types.Instance{{
		InstanceId: aws.String("i-worker123"), ImageId: aws.String("ami-123"), InstanceType: types.InstanceTypeT3Micro,
		State: &types.InstanceState{Name: types.InstanceStateNameRunning}, PublicIpAddress: aws.String("203.0.113.10"),
		PrivateIpAddress: aws.String("10.0.1.12"), VpcId: aws.String("vpc-123"), SubnetId: aws.String("subnet-123"),
		KeyName: aws.String("ops-key"), LaunchTime: &launched,
		Tags: []types.Tag{{Key: aws.String("Name"), Value: aws.String("aiori-node")}},
		SecurityGroups: []types.GroupIdentifier{{GroupId: aws.String("sg-123")}},
		Placement: &types.Placement{AvailabilityZone: aws.String("ap-south-1a")},
	}}}}}}
	got, err := (Client{Region: "ap-south-1", API: f}).ListManaged(context.Background(), "")
	if err != nil { t.Fatal(err) }
	if len(got) != 1 { t.Fatalf("got %d instances, want 1", len(got)) }
	instance := got[0]
	if instance.ID != "i-worker123" || instance.Name != "aiori-node" || instance.ImageID != "ami-123" || instance.State.Name != "running" { t.Fatalf("unexpected inventory record: %#v", instance) }
	if instance.PublicIP != "203.0.113.10" || instance.PrivateIP != "10.0.1.12" || instance.VPCID != "vpc-123" || instance.SubnetID != "subnet-123" || instance.KeyName != "ops-key" { t.Fatalf("missing network/connection metadata: %#v", instance) }
	if len(instance.SecurityGroups) != 1 || instance.SecurityGroups[0] != "sg-123" || !instance.LaunchTime.Equal(launched) { t.Fatalf("missing security group/launch metadata: %#v", instance) }
	if len(f.describeInputs) != 1 || len(f.describeInputs[0].Filters) != 1 || aws.ToString(f.describeInputs[0].Filters[0].Name) != "tag:ManagedBy" || len(f.describeInputs[0].Filters[0].Values) != 1 || f.describeInputs[0].Filters[0].Values[0] != "aiori-controller" { t.Fatal("inventory query did not filter to controller-owned resources") }
}
