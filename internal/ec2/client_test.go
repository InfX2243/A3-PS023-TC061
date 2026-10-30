package ec2

import "testing"

func TestValidID(t *testing.T) {
	for _,tc:=range []struct{v,p string; want bool}{{"ami-123","ami-",true},{"subnet-ab12","subnet-",true},{"ami-","ami-",false},{"ami-abc/def","ami-",false},{"i-123","i-",true}} {
		if got:=validID(tc.v,tc.p);got!=tc.want { t.Errorf("validID(%q,%q)=%v want %v",tc.v,tc.p,got,tc.want) }
	}
}
