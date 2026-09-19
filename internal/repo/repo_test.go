package repo

import "testing"

func TestValidateTarList(t *testing.T) {
	if err := validateTarList("safe/file\n./another\n"); err != nil {
		t.Fatal(err)
	}
	if err := validateTarList("../escape\n"); err == nil {
		t.Fatal("expected path traversal rejection")
	}
	if err := validateTarList("/absolute/path\n"); err == nil {
		t.Fatal("expected absolute path rejection")
	}
}
