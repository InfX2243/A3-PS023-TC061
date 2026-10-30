package ec2

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestValidID(t *testing.T) {
	for _,tc:=range []struct{v,p string; want bool}{{"ami-123","ami-",true},{"subnet-ab12","subnet-",true},{"sg-ab12","sg-",true},{"ami-","ami-",false},{"ami-abc/def","ami-",false},{"i-123","i-",true}} {
		if got:=validID(tc.v,tc.p);got!=tc.want { t.Errorf("validID(%q,%q)=%v want %v",tc.v,tc.p,got,tc.want) }
	}
}

func TestLaunchRejectsUnsafeOrUnapprovedInputBeforeCallingAWS(t *testing.T) {
	client:=Client{Binary:"/definitely/not/an/aws/cli"}
	base:=LaunchInput{Name:"test-node",Region:"ap-south-1",ImageID:"ami-123",InstanceType:"t3.micro",SubnetID:"subnet-123",SecurityGroupIDs:[]string{"sg-123"},ClientToken:"form-token"}
	cases:=[]struct{name string; mutate func(*LaunchInput)}{
		{"unapproved instance type",func(v *LaunchInput){v.InstanceType="c8i.xlarge"}},
		{"missing client token",func(v *LaunchInput){v.ClientToken=""}},
		{"invalid security group",func(v *LaunchInput){v.SecurityGroupIDs=[]string{"sg-bad/evil"}}},
		{"invalid AMI",func(v *LaunchInput){v.ImageID="ami-;touch"}},
		{"unsafe name",func(v *LaunchInput){v.Name="node;bad"}},
	}
	for _,tc:=range cases { t.Run(tc.name,func(t *testing.T){ in:=base; tc.mutate(&in); if _,err:=client.Launch(context.Background(),in);err==nil { t.Fatal("expected validation error") } }) }
}

func TestLifecycleRejectsTerminate(t *testing.T) {
	if err:=(Client{Binary:"/missing"}).Action(context.Background(),"ap-south-1","i-123","terminate");err==nil { t.Fatal("terminate must not be available through this adapter") }
}

func TestLaunchDecodesAWSResponse(t *testing.T) {
	binary:=filepath.Join(t.TempDir(),"aws")
	script:="#!/bin/sh\nprintf '%s' '"+`{"Instances":[{"InstanceId":"i-abc123","InstanceType":"t3.micro","State":{"Name":"pending"},"Placement":{"AvailabilityZone":"ap-south-1a"}}]}`+"'\n"
	if err:=os.WriteFile(binary,[]byte(script),0700);err!=nil { t.Fatal(err) }
	client:=Client{Binary:binary}
	instance,err:=client.Launch(context.Background(),LaunchInput{Name:"worker-test",Region:"ap-south-1",ImageID:"ami-123",InstanceType:"t3.micro",SubnetID:"subnet-123",SecurityGroupIDs:[]string{"sg-123"},ClientToken:"stable-form-token"})
	if err!=nil { t.Fatal(err) }
	if instance.ID!="i-abc123" || instance.State.Name!="pending" || instance.Placement.AvailabilityZone!="ap-south-1a" { t.Fatalf("unexpected EC2 response: %#v",instance) }
}
