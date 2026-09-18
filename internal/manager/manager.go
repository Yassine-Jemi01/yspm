package manager

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Yassine-Jemi01/yspm/internal/config"
	"github.com/Yassine-Jemi01/yspm/internal/model"
	"github.com/Yassine-Jemi01/yspm/internal/repo"
	"github.com/Yassine-Jemi01/yspm/internal/store"
)

type Manager struct{ repository string }

func New() *Manager { return &Manager{repository: config.RepositoryURL()} }

func (m *Manager) index() (model.Index, error) {
	idx, err := repo.LoadCachedIndex()
	if err == nil {
		return idx, nil
	}
	if err := m.Update(); err != nil {
		return model.Index{}, err
	}
	return repo.LoadCachedIndex()
}

func (m *Manager) Update() error {
	fmt.Printf("Updating stable repository...\n  %s\n", m.repository)
	idx, err := repo.FetchIndex(m.repository)
	if err != nil {
		return err
	}
	db, err := store.LoadDB()
	if err != nil {
		return err
	}
	if db.Release != "" && db.Release != idx.Release {
		return fmt.Errorf("repository release changed from %s to %s; yspm does not perform rolling-release upgrades", db.Release, idx.Release)
	}
	if db.Release == "" {
		db.Release = idx.Release
		if err := store.SaveDB(db); err != nil {
			return err
		}
	}
	if err := repo.CacheIndex(idx); err != nil {
		return err
	}
	fmt.Printf("Release %s (%s) — %d packages.\n", idx.Release, idx.Channel, len(idx.Packages))
	return nil
}

func (m *Manager) find(name string, idx model.Index) (model.Package, error) {
	var found *model.Package
	for i := range idx.Packages {
		p := idx.Packages[i]
		if p.Name != name || !repo.SupportsCurrentSystem(p) {
			continue
		}
		if found == nil || repo.CompareVersion(p.Version, found.Version) > 0 {
			cp := p
			found = &cp
		}
	}
	if found == nil {
		return model.Package{}, fmt.Errorf("package %q not found", name)
	}
	return *found, nil
}

func (m *Manager) Search(query string) ([]model.Package, error) {
	idx, err := m.index()
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(query)
	seen := map[string]model.Package{}
	for _, p := range idx.Packages {
		if !repo.SupportsCurrentSystem(p) {
			continue
		}
		if strings.Contains(strings.ToLower(p.Name), query) || strings.Contains(strings.ToLower(p.Description), query) {
			if old, ok := seen[p.Name]; !ok || repo.CompareVersion(p.Version, old.Version) > 0 {
				seen[p.Name] = p
			}
		}
	}
	out := make([]model.Package, 0, len(seen))
	for _, p := range seen {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Manager) Info(name string) (model.Package, error) {
	idx, err := m.index()
	if err != nil {
		return model.Package{}, err
	}
	return m.find(name, idx)
}

func (m *Manager) List() ([]model.InstalledPackage, error) {
	db, err := store.LoadDB()
	if err != nil {
		return nil, err
	}
	out := make([]model.InstalledPackage, 0, len(db.Packages))
	for _, p := range db.Packages {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Manager) Install(name string) error {
	idx, err := m.index()
	if err != nil {
		return err
	}
	db, err := store.LoadDB()
	if err != nil {
		return err
	}
	if db.Release == "" {
		db.Release = idx.Release
	} else if db.Release != idx.Release {
		return fmt.Errorf("installed packages belong to release %s, but repository is release %s", db.Release, idx.Release)
	}
	return m.installRecursive(name, idx, &db, map[string]bool{})
}

func (m *Manager) installRecursive(name string, idx model.Index, db *model.Database, visiting map[string]bool) error {
	if visiting[name] {
		return fmt.Errorf("dependency cycle detected involving %q", name)
	}
	pkg, err := m.find(name, idx)
	if err != nil {
		return err
	}
	if installed, ok := db.Packages[name]; ok && repo.CompareVersion(installed.Version, pkg.Version) >= 0 {
		fmt.Printf("%s %s is already installed.\n", name, installed.Version)
		return nil
	}
	visiting[name] = true
	defer delete(visiting, name)

	for _, dep := range pkg.Dependencies {
		if err := m.installRecursive(dep, idx, db, visiting); err != nil {
			return fmt.Errorf("install dependency %q for %q: %w", dep, name, err)
		}
	}

	var installed model.InstalledPackage
	if pkg.Kind != "meta" {
		var err error
		installed, err = m.installPackage(pkg)
		if err != nil {
			return err
		}
	} else {
		fmt.Printf("Installing %s %s (meta-package)\n", pkg.Name, pkg.Version)
		installed = model.InstalledPackage{
			Name:         pkg.Name,
			Version:      pkg.Version,
			Kind:         pkg.Kind,
			Dependencies: append([]string(nil), pkg.Dependencies...),
		}
	}
	db.Packages[pkg.Name] = installed
	return store.SaveDB(*db)
}

func (m *Manager) installPackage(pkg model.Package) (model.InstalledPackage, error) {
	if err := repo.ValidateInstallPackage(pkg); err != nil {
		return model.InstalledPackage{}, err
	}
	dataDir, err := config.DataDir()
	if err != nil {
		return model.InstalledPackage{}, err
	}
	binDir, err := config.BinDir()
	if err != nil {
		return model.InstalledPackage{}, err
	}
	cacheDir, err := config.CacheDir()
	if err != nil {
		return model.InstalledPackage{}, err
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return model.InstalledPackage{}, err
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return model.InstalledPackage{}, err
	}

	archivePath := filepath.Join(cacheDir, "packages", packageFilename(pkg))
	if pkg.Kind == "appimage" {
		archivePath = filepath.Join(cacheDir, "packages", packageFilename(pkg))
	}
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		return model.InstalledPackage{}, err
	}
	validCache := false
	if _, err := os.Stat(archivePath); err == nil {
		if pkg.SHA256 != "" {
			if err := repo.VerifySHA256(archivePath, pkg.SHA256); err == nil {
				validCache = true
			}
		} else {
			validCache = true
		}
	}
	if !validCache {
		fmt.Printf("Downloading %s %s...\n", pkg.Name, pkg.Version)
		if err := repo.Download(pkg.URL, archivePath); err != nil {
			return model.InstalledPackage{}, fmt.Errorf("download %s: %w", pkg.Name, err)
		}
		if pkg.SHA256 == "" {
			fmt.Printf("  warning: repository does not provide a SHA-256 for %s\n", pkg.Name)
		}
		if err := repo.VerifySHA256(archivePath, pkg.SHA256); err != nil {
			os.Remove(archivePath)
			return model.InstalledPackage{}, fmt.Errorf("verify %s: %w", pkg.Name, err)
		}
	}

	stagingRoot := filepath.Join(dataDir, ".staging")
	if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
		return model.InstalledPackage{}, err
	}
	stage, err := os.MkdirTemp(stagingRoot, pkg.Name+"-*")
	if err != nil {
		return model.InstalledPackage{}, err
	}
	defer os.RemoveAll(stage)

	installDir := filepath.Join(dataDir, "packages", pkg.Name, pkg.Version)
	commandName := pkg.Command
	var executable string
	if pkg.Kind == "appimage" {
		fileName := filepath.Base(pkg.Entry)
		if fileName == "." || fileName == string(filepath.Separator) || fileName == "" {
			fileName = pkg.Name + ".AppImage"
		}
		target := filepath.Join(stage, fileName)
		if err := copyFile(archivePath, target, 0o755); err != nil {
			return model.InstalledPackage{}, err
		}
		executable = target
		if commandName == "" {
			commandName = pkg.Name
		}
	} else {
		if _, err := repo.ExtractArchive(archivePath, pkg.Format, stage); err != nil {
			return model.InstalledPackage{}, fmt.Errorf("extract %s: %w", pkg.Name, err)
		}
		executable, err = repo.FindEntry(stage, pkg.Entry)
		if err != nil {
			return model.InstalledPackage{}, fmt.Errorf("locate executable for %s: %w", pkg.Name, err)
		}
		if commandName == "" {
			commandName = pkg.Name
		}
		if info, err := os.Stat(executable); err == nil && info.Mode()&0o111 == 0 {
			if err := os.Chmod(executable, info.Mode()|0o755); err != nil {
				return model.InstalledPackage{}, err
			}
		}
	}

	execRel, err := filepath.Rel(stage, executable)
	if err != nil {
		return model.InstalledPackage{}, err
	}
	if err := os.MkdirAll(filepath.Dir(installDir), 0o755); err != nil {
		return model.InstalledPackage{}, err
	}
	backupDir := installDir + ".old"
	_ = os.RemoveAll(backupDir)
	if _, err := os.Stat(installDir); err == nil {
		if err := os.Rename(installDir, backupDir); err != nil {
			return model.InstalledPackage{}, fmt.Errorf("prepare upgrade: %w", err)
		}
	}
	if err := os.Rename(stage, installDir); err != nil {
		if _, statErr := os.Stat(backupDir); statErr == nil {
			_ = os.Rename(backupDir, installDir)
		}
		return model.InstalledPackage{}, fmt.Errorf("commit install: %w", err)
	}
	finalExecutable := filepath.Join(installDir, execRel)
	if err := linkCommand(finalExecutable, filepath.Join(binDir, commandName)); err != nil {
		_ = os.RemoveAll(installDir)
		if _, statErr := os.Stat(backupDir); statErr == nil {
			_ = os.Rename(backupDir, installDir)
		}
		return model.InstalledPackage{}, err
	}
	_ = os.RemoveAll(backupDir)

	fmt.Printf("Installed %s %s\n", pkg.Name, pkg.Version)
	return model.InstalledPackage{
		Name:         pkg.Name,
		Version:      pkg.Version,
		Kind:         pkg.Kind,
		Dependencies: append([]string(nil), pkg.Dependencies...),
		InstallDir:   installDir,
		Executable:   finalExecutable,
		Command:      commandName,
	}, nil
}

func (m *Manager) Remove(name string) error {
	db, err := store.LoadDB()
	if err != nil {
		return err
	}
	installed, ok := db.Packages[name]
	if !ok {
		return fmt.Errorf("package %q is not installed", name)
	}
	for otherName, other := range db.Packages {
		if otherName == name {
			continue
		}
		for _, dep := range other.Dependencies {
			if dep == name {
				return fmt.Errorf("cannot remove %q: installed package %q depends on it", name, otherName)
			}
		}
	}
	binDir, err := config.BinDir()
	if err != nil {
		return err
	}
	dataDir, err := config.DataDir()
	if err != nil {
		return err
	}
	if installed.Command != "" {
		link := filepath.Join(binDir, installed.Command)
		if target, err := os.Readlink(link); err == nil {
			if filepath.IsAbs(target) && strings.HasPrefix(filepath.Clean(target), filepath.Clean(dataDir)+string(os.PathSeparator)) {
				_ = os.Remove(link)
			}
		}
	}
	if installed.InstallDir != "" {
		_ = os.RemoveAll(installed.InstallDir)
	}
	delete(db.Packages, name)
	if err := store.SaveDB(db); err != nil {
		return err
	}
	fmt.Printf("Removed %s %s\n", name, installed.Version)
	return nil
}

func (m *Manager) Upgrade() error {
	if err := m.Update(); err != nil {
		return err
	}
	idx, err := repo.LoadCachedIndex()
	if err != nil {
		return err
	}
	db, err := store.LoadDB()
	if err != nil {
		return err
	}
	var updates []model.Package
	for name, installed := range db.Packages {
		pkg, err := m.find(name, idx)
		if err != nil {
			continue
		}
		if repo.CompareVersion(pkg.Version, installed.Version) > 0 {
			updates = append(updates, pkg)
		}
	}
	if len(updates) == 0 {
		fmt.Println("All packages are up to date.")
		return nil
	}
	sort.Slice(updates, func(i, j int) bool { return updates[i].Name < updates[j].Name })
	for _, pkg := range updates {
		fmt.Printf("Upgrading %s: %s -> %s\n", pkg.Name, db.Packages[pkg.Name].Version, pkg.Version)
		if err := m.installRecursive(pkg.Name, idx, &db, map[string]bool{}); err != nil {
			return err
		}
	}
	return nil
}

func packageFilename(pkg model.Package) string {
	format := pkg.Format
	if format == "appimage" || pkg.Kind == "appimage" {
		return pkg.Name + "-" + pkg.Version + ".AppImage"
	}
	return pkg.Name + "-" + pkg.Version + "." + format
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func linkCommand(executable, link string) error {
	if info, err := os.Lstat(link); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("refusing to overwrite existing non-symlink %s", link)
		}
		_ = os.Remove(link)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	target, err := filepath.Abs(executable)
	if err != nil {
		return err
	}
	return os.Symlink(target, link)
}
