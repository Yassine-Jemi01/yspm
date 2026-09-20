package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Yassine-Jemi01/yspm/internal/model"
	"github.com/Yassine-Jemi01/yspm/internal/repo"
	"github.com/Yassine-Jemi01/yspm/internal/store"
)

func (m *Manager) hydrateHardcorePackage(p model.Package) (model.Package, error) {
	archive := filepath.Join(m.Paths.Cache, packageFilename(p))
	if err := os.MkdirAll(m.Paths.Cache, 0o755); err != nil {
		return p, err
	}
	valid := false
	if st, err := os.Stat(archive); err == nil && st.Mode().IsRegular() {
		valid = repo.VerifySHA256(archive, p.SHA256) == nil
	}
	if !valid {
		if err := repo.Download(p.URL, archive); err != nil {
			return p, fmt.Errorf("download %s: %w", p.Name, err)
		}
		if err := repo.VerifySHA256(archive, p.SHA256); err != nil {
			_ = os.Remove(archive)
			return p, fmt.Errorf("verify %s: %w", p.Name, err)
		}
	}
	h, err := repo.InspectHardcoreArchive(archive)
	if err != nil {
		return p, err
	}
	p.Version = h.Package.Version
	p.Dependencies = h.Package.Dependencies
	p.Kind = "system"
	p.OS = "linux"
	p.Format = repo.HardcorePackageFormat
	p.Architecture = m.arch
	return p, nil
}

func (m *Manager) syncLegacyPackages(db *model.Database) (int, error) {
	root := filepath.Join(m.Paths.Root, "etc", "installer")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if db.Packages == nil {
		db.Packages = map[string]model.InstalledPackage{}
	}
	changed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := repo.ReadHardcoreInstaller(filepath.Join(root, entry.Name()))
		if err != nil {
			continue
		}
		name := data.Package.Name
		if existing, ok := db.Packages[name]; ok && existing.Format != "" && existing.Format != repo.HardcorePackageFormat {
			continue
		}
		manifest, err := repo.HardcoreInstalledManifest(m.Paths.Root, name)
		if err != nil {
			continue
		}
		hashes := map[string]string{}
		configs := map[string]string{}
		var files []string
		for _, e := range manifest {
			if e.Type == "dir" {
				continue
			}
			files = append(files, e.Path)
			if e.Type == "file" {
				hashes[e.Path] = e.SHA256
				if strings.HasPrefix(e.Path, "etc/") && !strings.HasPrefix(e.Path, "etc/installer/") {
					configs[e.Path] = e.SHA256
				}
			}
		}
		ip := model.InstalledPackage{
			Name: name, Version: data.Package.Version, Kind: "system",
			Format: repo.HardcorePackageFormat, Architecture: m.arch,
			Dependencies: data.Package.Dependencies, Files: files, Manifest: manifest,
			FileHashes: hashes, ConfigHashes: configs, Explicit: true,
			Hooks: map[string]string{
				"install": data.InstallScript, "uninstall": data.UninstallScript,
			},
			InstalledAt: time.Now(),
		}
		if old, ok := db.Packages[name]; ok && old.Version == ip.Version && old.Format == ip.Format {
			continue
		}
		db.Packages[name] = ip
		changed++
	}
	return changed, nil
}

func (m *Manager) SyncLegacy() error {
	if err := m.requirePrivileges("sync"); err != nil {
		return err
	}
	return withLock(m.Paths.State, func() error {
		db, err := store.LoadDBFor(m.User)
		if err != nil {
			return err
		}
		n, err := m.syncLegacyPackages(&db)
		if err != nil {
			return err
		}
		if n > 0 {
			if err := store.SaveDBFor(m.User, db); err != nil {
				return err
			}
		}
		fmt.Printf("Imported %d HardcoreLinux legacy package(s).\n", n)
		return nil
	})
}

func (m *Manager) Owner(path string) error {
	db, err := store.LoadDBFor(m.User)
	if err != nil {
		return err
	}
	if _, err := m.syncLegacyPackages(&db); err != nil {
		return err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("owner requires a path")
	}
	if filepath.IsAbs(path) {
		path = strings.TrimPrefix(filepath.Clean(path), string(filepath.Separator))
	}
	path = filepath.ToSlash(path)
	var owners []string
	for name, p := range db.Packages {
		for _, e := range p.Manifest {
			if filepath.ToSlash(e.Path) == path {
				owners = append(owners, name)
				break
			}
		}
		for _, f := range p.Files {
			if filepath.ToSlash(f) == path && !containsString(owners, name) {
				owners = append(owners, name)
				break
			}
		}
	}
	sort.Strings(owners)
	if len(owners) == 0 {
		fmt.Printf("%s is not owned by an installed package.\n", path)
		return nil
	}
	for _, name := range owners {
		fmt.Println(name)
	}
	return nil
}

func legacyDependenciesSatisfied(pkgs []model.Package, db model.Database) error {
	available := map[string]bool{}
	for name := range db.Packages {
		available[name] = true
	}
	for _, p := range pkgs {
		available[p.Name] = true
	}
	for _, p := range pkgs {
		for _, dep := range p.Dependencies {
			satisfied := false
			for _, alt := range strings.Split(string(dep), "|") {
				r := parseDependency(strings.TrimSpace(alt))
				if available[r.Name] {
					satisfied = true
					break
				}
			}
			if !satisfied {
				return fmt.Errorf("package %s requires missing dependency %s", p.Name, dep)
			}
		}
	}
	return nil
}
