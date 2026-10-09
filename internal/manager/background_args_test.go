package manager

import (
	"reflect"
	"testing"
)

func TestBackgroundWorkerArgsPreservePackagesAndSnapshot(t *testing.T) {
	got := backgroundWorkerArgs("install", "tx-1", []string{"firefox", "ripgrep"}, true)
	want := []string{"__worker", "install", "tx-1", "--", "firefox", "ripgrep", "--yes", "--snapshot"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("backgroundWorkerArgs() = %#v, want %#v", got, want)
	}
}

func TestBackgroundWorkerArgsDoNotEnableSnapshotByDefault(t *testing.T) {
	got := backgroundWorkerArgs("remove", "tx-2", []string{"firefox"}, false)
	want := []string{"__worker", "remove", "tx-2", "--", "firefox", "--yes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("backgroundWorkerArgs() = %#v, want %#v", got, want)
	}
}
