package wal

import "testing"

func TestResolveMode(t *testing.T) {
	direct := &DetectedServer{Mode: ModeDirect}
	remote := &DetectedServer{Mode: ModeSpool, Reason: "container"}
	cases := []struct {
		req   string
		spool bool
		srv   *DetectedServer
		want  string
		err   bool
	}{
		{"auto", false, direct, ModeDirect, false},
		{"auto", false, remote, ModeStream, false},
		{"", true, remote, ModeSpool, false},
		{"stream", false, direct, ModeStream, false},
		{"direct", false, remote, "", true},
		{"bogus", false, direct, "", true},
	}
	for _, c := range cases {
		got, err := ResolveMode(c.req, c.spool, c.srv)
		if (err != nil) != c.err || got != c.want {
			t.Errorf("%+v: got %q err=%v", c, got, err)
		}
	}
}

func TestSlotName(t *testing.T) {
	if got := SlotName("backup-dataForge-20261017_201023"); got != "acrynite_backup_dataforge_20261017_201023" {
		t.Fatalf("unexpected slot name %s", got)
	}
}
