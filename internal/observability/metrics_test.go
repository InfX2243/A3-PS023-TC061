package observability

import (
	"testing"
	"time"
)

func TestStoreValidatesAndReturnsSamplesChronologically(t *testing.T) {
	store := NewStore()
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	later := Sample{InstanceID:"i-0123456789abcdef0", Timestamp:now.Add(-time.Minute), CPUPercent:25, MemoryPercent:40, DiskPercent:60, NetworkRxBytes:100, NetworkTxBytes:200}
	earlier := later; earlier.Timestamp = now.Add(-2*time.Minute); earlier.CPUPercent = 10
	if err:=store.Add(later); err!=nil { t.Fatal(err) }
	if err:=store.Add(earlier); err!=nil { t.Fatal(err) }
	got:=store.List(later.InstanceID,time.Time{})
	if len(got)!=2 || got[0].CPUPercent!=10 || got[1].CPUPercent!=25 { t.Fatalf("unexpected ordered samples: %#v",got) }
	if len(store.List(later.InstanceID,now.Add(-90*time.Second)))!=1 { t.Fatal("since filter was not applied") }
}

func TestStoreRejectsInvalidSamples(t *testing.T) {
	store:=NewStore()
	cases:=[]Sample{
		{InstanceID:"not-an-instance"},
		{InstanceID:"i-0123456789abcdef0",CPUPercent:101},
		{InstanceID:"i-0123456789abcdef0",MemoryPercent:-1},
		{InstanceID:"i-0123456789abcdef0",DiskPercent:101},
	}
	for _,sample:=range cases { if err:=store.Add(sample); err==nil { t.Fatalf("expected invalid sample rejection: %#v",sample) } }
}

func TestStoreDropsSamplesOutsideRetention(t *testing.T) {
	store:=NewStore()
	now:=time.Date(2026,10,30,12,0,0,0,time.UTC)
	store.now=func()time.Time{return now}
	sample:=Sample{InstanceID:"i-0123456789abcdef0",Timestamp:now.Add(-25*time.Hour)}
	if err:=store.Add(sample);err==nil { t.Fatal("expected expired sample rejection") }
}
