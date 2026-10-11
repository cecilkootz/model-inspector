package main

import "testing"

func TestParseLayout(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
		ok   bool
	}{
		{[]string{"--path", "d"}, "", true},
		{[]string{"--path", "d", "--layout", "panels"}, "panels", true},
		{[]string{"--layout=tabs", "--path", "d"}, "tabs", true},
		{[]string{"--path", "d", "--layout", "grid"}, "", false},
		{[]string{"--path", "d", "--layout="}, "", true},
		{[]string{"--path", "d", "--layout"}, "", false},
	} {
		o, err := parseArgs(tc.args)
		if (err == nil) != tc.ok {
			t.Errorf("%v: err %v", tc.args, err)
			continue
		}
		if tc.ok && o.layout != tc.want {
			t.Errorf("%v: layout %q, want %q", tc.args, o.layout, tc.want)
		}
	}
}
