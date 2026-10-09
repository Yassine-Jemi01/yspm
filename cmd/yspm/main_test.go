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

func TestIsLocalArchiveArgRecognizesZip(t *testing.T) {
	if !isLocalArchiveArg("./download/package.ZIP") {
		t.Fatal("ZIP archives should be recognized as local archive arguments")
	}
}

func TestParseWorkerArgsPreservesIndividualPackageArguments(t *testing.T) {
	raw := []string{
		"install", "tx-123", "--snapshot", "--",
		"firefox", "./packages/my package.zip", "--snapshot", "--arch", "aarch64",
	}
	action, id, packages, snapshot, err := parseWorkerArgs(raw)
	if err != nil {
		t.Fatalf("parseWorkerArgs(): %v", err)
	}
	if action != "install" || id != "tx-123" {
		t.Fatalf("worker identity = %q/%q, want install/tx-123", action, id)
	}
	want := []string{"firefox", "./packages/my package.zip", "--snapshot", "--arch", "aarch64"}
	if !reflect.DeepEqual(packages, want) {
		t.Fatalf("worker packages = %#v, want %#v", packages, want)
	}
	if !snapshot {
		t.Fatal("snapshot flag before -- separator was not preserved")
	}
}

func TestParseWorkerArgsRequiresSeparator(t *testing.T) {
	if _, _, _, _, err := parseWorkerArgs([]string{"install", "tx-123", "firefox"}); err == nil {
		t.Fatal("expected missing -- separator to be rejected")
	}
}
