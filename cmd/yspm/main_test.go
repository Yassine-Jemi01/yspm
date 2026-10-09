package main

import (
	"reflect"
	"testing"
)

func TestStripRemovesArchFlagAndValue(t *testing.T) {
	got := strip(
		[]string{"foo", "--arch", "aarch64", "bar"},
		"--user", "--snapshot", "-y", "--yes", "--background", "--arch",
	)
	want := []string{"foo", "bar"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("strip() = %#v, want %#v", got, want)
	}
}

func TestStripRemovesArchEqualsFlag(t *testing.T) {
	got := strip(
		[]string{"foo", "--arch=aarch64", "bar"},
		"--user", "--snapshot", "-y", "--yes", "--background", "--arch",
	)
	want := []string{"foo", "bar"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("strip() = %#v, want %#v", got, want)
	}
}

func TestValueAfterSupportsBothArchForms(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "separate value", args: []string{"foo", "--arch", "aarch64"}, want: "aarch64"},
		{name: "equals value", args: []string{"foo", "--arch=x86_64"}, want: "x86_64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := valueAfter(tt.args, "--arch"); got != tt.want {
				t.Fatalf("valueAfter() = %q, want %q", got, tt.want)
			}
		})
	}
}
