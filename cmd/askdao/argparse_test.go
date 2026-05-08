package main

import (
	"reflect"
	"testing"
)

func TestSplitNameAndFlags(t *testing.T) {
	cases := []struct {
		args     []string
		wantName string
		wantRest []string
		wantOK   bool
	}{
		{[]string{"my-agent"}, "my-agent", []string{}, true},
		// Positional after flags.
		{[]string{"--auto", "my-agent"}, "my-agent", []string{"--auto"}, true},
		// Positional before flags — the breaking case stdlib flag mishandles.
		{[]string{"my-agent", "--auto"}, "my-agent", []string{"--auto"}, true},
		// Mixed.
		{[]string{"--from", ".", "my-agent", "--auto"}, "my-agent",
			[]string{"--from", ".", "--auto"}, true},
		// Empty.
		{nil, "", nil, false},
		// All flags, no positional.
		{[]string{"--auto"}, "", nil, false},
	}
	for _, tc := range cases {
		gotName, gotRest, gotOK := splitNameAndFlags(tc.args)
		if gotOK != tc.wantOK {
			t.Errorf("args=%v ok=%v, want %v", tc.args, gotOK, tc.wantOK)
			continue
		}
		if gotName != tc.wantName {
			t.Errorf("args=%v name=%q, want %q", tc.args, gotName, tc.wantName)
		}
		if !tc.wantOK {
			continue
		}
		if !reflect.DeepEqual(gotRest, tc.wantRest) {
			t.Errorf("args=%v rest=%v, want %v", tc.args, gotRest, tc.wantRest)
		}
	}
}
