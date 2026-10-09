package manager

import (
	"strings"
	"testing"

	"github.com/Yassine-Jemi01/yspm/internal/model"
)

func packageForResolver(name, version string) model.Package {
	return model.Package{
		Name:         name,
		Version:      version,
		Description:  name,
		OS:           "linux",
		Architecture: "x86_64",
		License:      "MIT",
		Kind:         "binary",
		ABI:          "test-abi-1",
		URL:          "https://packages.example.test/" + name + ".tar.gz",
		SHA256:       strings.Repeat("a", 64),
	}
}

func TestResolveIncludesDependenciesInDeterministicOrder(t *testing.T) {
	m := New(true, "x86_64")
	app := packageForResolver("app", "1.0.0")
	app.Dependencies = []model.Dependency{"lib>=1.0"}
	lib := packageForResolver("lib", "1.2.0")
	idx := model.Index{
		Release: "1",
		Channel: "stable",
		ABI:     "test-abi-1",
		Packages: []model.Package{lib, app},
	}
	db := model.Database{
		Release:  "1",
		ABI:      "test-abi-1",
		Packages: map[string]model.InstalledPackage{},
	}

	plan, err := m.resolve(idx, db, []string{"app"}, false)
	if err != nil {
		t.Fatalf("resolve app with dependency: %v", err)
	}
	if len(plan.Packages) != 2 {
		t.Fatalf("resolved %d packages, want 2: %#v", len(plan.Packages), plan.Packages)
	}
	if plan.Packages[0].Name != "app" || plan.Packages[1].Name != "lib" {
		t.Fatalf("plan order = [%s, %s], want [app, lib]", plan.Packages[0].Name, plan.Packages[1].Name)
	}
	if len(plan.Requested) != 1 || plan.Requested[0] != "app" {
		t.Fatalf("requested roots = %#v, want [app]", plan.Requested)
	}
}

func TestResolveRejectsUnsatisfiedDependencyVersion(t *testing.T) {
	m := New(true, "x86_64")
	app := packageForResolver("app", "1.0.0")
	app.Dependencies = []model.Dependency{"lib>=1.0"}
	lib := packageForResolver("lib", "0.9.0")
	idx := model.Index{
		Release: "1",
		Channel: "stable",
		ABI:     "test-abi-1",
		Packages: []model.Package{app, lib},
	}
	db := model.Database{
		Release:  "1",
		ABI:      "test-abi-1",
		Packages: map[string]model.InstalledPackage{},
	}

	if _, err := m.resolve(idx, db, []string{"app"}, false); err == nil {
		t.Fatal("expected dependency version conflict to fail resolution")
	}
}
