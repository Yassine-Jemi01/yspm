package manager

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Yassine-Jemi01/yspm/internal/config"
	"github.com/Yassine-Jemi01/yspm/internal/model"
	"github.com/Yassine-Jemi01/yspm/internal/repo"
	"github.com/Yassine-Jemi01/yspm/internal/store"
)

type Manager struct{ repository string }

type dependencyRequest struct{ Name, Op, Version string }
type resolvedPlan struct {
	Packages  []model.Package
	Requested []string
}

type installResult struct {
	pkg        model.Package
	archive    string
	stage      string
	executable string
	files      []string
	command    string
}

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
		return fmt.Errorf("repository release changed from %s to %s; use an explicit release migration instead of rolling forward", db.Release, idx.Release)
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

func (m *Manager) findSatisfying(name, constraint string, idx model.Index) (model.Package, error) {
	var found *model.Package
	for i := range idx.Packages {
		p := idx.Packages[i]
		if !repo.SupportsCurrentSystem(p) || !matchesName(&p, name) {
			continue
		}
		if !satisfies(p.Version, constraint) {
			continue
		}
		if found == nil || compareVersion(p.Version, found.Version) > 0 || (compareVersion(p.Version, found.Version) == 0 && p.Revision > found.Revision) {
			cp := p
			found = &cp
		}
	}
	if found == nil {
		return model.Package{}, fmt.Errorf("no version of %q satisfies %q", name, constraint)
	}
	return *found, nil
}

func matchesName(p *model.Package, wanted string) bool {
	if p.Name == wanted {
		return true
	}
	for _, provided := range p.Provides {
		if provided == wanted {
			return true
		}
	}
	return false
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
			if old, ok := seen[p.Name]; !ok || compareVersion(p.Version, old.Version) > 0 {
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
	return m.findSatisfying(name, "", idx)
}

func (m *Manager) List(upgradable, explicit bool) ([]model.InstalledPackage, error) {
	db, err := store.LoadDB()
	if err != nil {
		return nil, err
	}
	var idx model.Index
	if upgradable {
		idx, err = m.index()
		if err != nil {
			return nil, err
		}
	}
	out := make([]model.InstalledPackage, 0, len(db.Packages))
	for _, p := range db.Packages {
		if explicit && !p.Explicit {
			continue
		}
		if upgradable {
			candidate, e := m.findSatisfying(p.Name, "", idx)
			if e != nil || compareVersion(candidate.Version, p.Version) <= 0 {
				continue
			}
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Manager) Depends(name string) error {
	idx, err := m.index()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	return m.printDependencyTree(name, idx, "", seen)
}

func (m *Manager) printDependencyTree(name string, idx model.Index, prefix string, seen map[string]bool) error {
	p, err := m.findSatisfying(name, "", idx)
	if err != nil {
		return err
	}
	fmt.Printf("%s%s %s\n", prefix, p.Name, p.Version)
	if seen[p.Name] {
		return nil
	}
	seen[p.Name] = true
	for _, dep := range p.Dependencies {
		r := parseDependency(string(dep))
		child, err := m.findSatisfying(r.Name, r.Op+r.Version, idx)
		if err != nil {
			fmt.Printf("%s  ? %s (%s)\n", prefix, dep, err)
			continue
		}
		_ = m.printDependencyTree(child.Name, idx, prefix+"  ", seen)
	}
	return nil
}

func (m *Manager) Why(target string) error {
	db, err := store.LoadDB()
	if err != nil {
		return err
	}
	if p, ok := db.Packages[target]; ok && p.Explicit {
		fmt.Printf("%s is explicitly installed.\n", target)
		return nil
	}
	found := false
	names := make([]string, 0, len(db.Packages))
	for name, p := range db.Packages {
		if p.Explicit && name != target {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if chain, ok := dependencyChain(name, target, db.Packages, map[string]bool{}); ok {
			fmt.Printf("%s is required by %s via %s\n", target, name, strings.Join(chain, " -> "))
			found = true
		}
	}
	if !found {
		fmt.Printf("No installed package requires %s.\n", target)
	}
	return nil
}

func dependencyChain(start, target string, pkgs map[string]model.InstalledPackage, seen map[string]bool) ([]string, bool) {
	if seen[start] {
		return nil, false
	}
	seen[start] = true
	for _, d := range pkgs[start].Dependencies {
		r := parseDependency(string(d))
		if r.Name == target {
			return []string{start, target}, true
		}
		if _, ok := pkgs[r.Name]; ok {
			if c, ok := dependencyChain(r.Name, target, pkgs, seen); ok {
				return append([]string{start}, c...), true
			}
		}
	}
	return nil, false
}

func (m *Manager) Explain(name string) error {
	idx, err := m.index()
	if err != nil {
		return err
	}
	p, err := m.findSatisfying(name, "", idx)
	if err != nil {
		return err
	}
	fmt.Printf("Package: %s %s\n", p.Name, p.Version)
	fmt.Printf("Dependencies:\n")
	if len(p.Dependencies) == 0 {
		fmt.Println("  none")
	} else {
		for _, d := range p.Dependencies {
			r := parseDependency(string(d))
			_, e := m.findSatisfying(r.Name, r.Op+r.Version, idx)
			if e != nil {
				fmt.Printf("  %s  [UNRESOLVED: %v]\n", d, e)
			} else {
				fmt.Printf("  %s\n", d)
			}
		}
	}
	if len(p.Conflicts) > 0 {
		fmt.Printf("Conflicts: %s\n", strings.Join(p.Conflicts, ", "))
	}
	if len(p.Provides) > 0 {
		fmt.Printf("Provides: %s\n", strings.Join(p.Provides, ", "))
	}
	return nil
}

func (m *Manager) InstallMany(names []string, yes bool) error {
	return m.runTransaction("install", names, yes, false)
}
func (m *Manager) Upgrade(yes bool) error { return m.runTransaction("upgrade", nil, yes, true) }

func (m *Manager) RemoveMany(names []string, yes bool) error {
	return withLock(func() error {
		db, err := store.LoadDB()
		if err != nil {
			return err
		}
		idx, err := m.index()
		if err != nil {
			return err
		}
		if db.Release == "" {
			db.Release = idx.Release
		} else if db.Release != idx.Release {
			return fmt.Errorf("installed packages belong to release %s, repository is release %s", db.Release, idx.Release)
		}
		for _, name := range names {
			if _, ok := db.Packages[name]; !ok {
				return fmt.Errorf("package %q is not installed", name)
			}
		}
		var remove []model.InstalledPackage
		for _, name := range names {
			remove = append(remove, db.Packages[name])
		}
		for _, p := range remove {
			for otherName, other := range db.Packages {
				if otherName == p.Name || containsName(names, otherName) {
					continue
				}
				if dependsOn(other.Dependencies, p.Name) {
					return fmt.Errorf("cannot remove %q: installed package %q depends on it", p.Name, otherName)
				}
			}
		}
		if !yes {
			fmt.Printf("Remove %s? [y/N] ", strings.Join(names, ", "))
			var ans string
			_, _ = fmt.Scanln(&ans)
			if strings.ToLower(strings.TrimSpace(ans)) != "y" {
				fmt.Println("Aborted.")
				return nil
			}
		}
		tx := m.startTransaction("remove", names)
		db, err = store.LoadDB()
		if err != nil {
			return m.finishFailed(tx, err)
		}
		state, err := stageRemoval(db, remove, tx.ID)
		if err != nil {
			return m.finishFailed(tx, err)
		}
		if err := store.SaveDB(db); err != nil {
			state.Rollback()
			return m.finishFailed(tx, err)
		}
		state.Finalize()
		return m.finishSuccess(tx)
	})
}

func (m *Manager) Autoremove(yes bool) error {
	for {
		db, err := store.LoadDB()
		if err != nil {
			return err
		}
		var candidates []string
		for name, p := range db.Packages {
			if p.Explicit {
				continue
			}
			needed := false
			for otherName, other := range db.Packages {
				if otherName == name {
					continue
				}
				if dependsOn(other.Dependencies, name) {
					needed = true
					break
				}
			}
			if !needed {
				candidates = append(candidates, name)
			}
		}
		if len(candidates) == 0 {
			fmt.Println("No automatically installed packages can be removed.")
			return nil
		}
		sort.Strings(candidates)
		if err := m.RemoveMany(candidates, yes); err != nil {
			return err
		}
		if !yes {
			return nil
		}
	}
}

func (m *Manager) Clean() error {
	cache, err := config.CacheDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(cache, "packages"))
	if os.IsNotExist(err) {
		fmt.Println("Cache is already clean.")
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(cache, "packages", e.Name())); err != nil {
			return err
		}
	}
	fmt.Println("Package cache cleaned.")
	return nil
}

func (m *Manager) Check() error {
	db, err := store.LoadDB()
	if err != nil {
		return err
	}
	var problems int
	for name, p := range db.Packages {
		if p.InstallDir != "" {
			if _, err := os.Stat(p.InstallDir); err != nil {
				fmt.Printf("%s: missing install directory\n", name)
				problems++
			} else {
				for _, file := range p.Files {
					if _, err := os.Stat(filepath.Join(p.InstallDir, filepath.FromSlash(file))); err != nil {
						fmt.Printf("%s: missing file %s\n", name, file)
						problems++
					}
				}
			}
		}
		if p.Command != "" {
			bin, e := config.BinDir()
			if e != nil {
				return e
			}
			link := filepath.Join(bin, p.Command)
			if _, err := os.Lstat(link); err != nil {
				fmt.Printf("%s: missing command link %s\n", name, p.Command)
				problems++
			}
		}
	}
	if problems > 0 {
		return fmt.Errorf("integrity check found %d problem(s)", problems)
	}
	fmt.Println("Package database looks consistent.")
	return nil
}

func (m *Manager) Release() error {
	db, err := store.LoadDB()
	if err != nil {
		return err
	}
	idx, err := m.index()
	if err != nil {
		return err
	}
	fmt.Printf("Installed release: %s\nRepository release: %s\nChannel:            %s\nPackages installed: %d\n", db.Release, idx.Release, idx.Channel, len(db.Packages))
	if idx.Release != db.Release {
		fmt.Println("Warning: repository release differs; yspm will not silently migrate releases.")
	}
	return nil
}

func (m *Manager) History() error {
	db, err := store.LoadDB()
	if err != nil {
		return err
	}
	for i := len(db.Transactions) - 1; i >= 0; i-- {
		tx := db.Transactions[i]
		fmt.Printf("%-16s %-8s %-10s %s\n", tx.ID, tx.Status, tx.Action, tx.StartedAt.Format(time.RFC3339))
	}
	return nil
}

func (m *Manager) Transaction(id string) error {
	tx, err := store.GetTransaction(id)
	if err != nil {
		return err
	}
	fmt.Printf("ID:         %s\nAction:     %s\nStatus:     %s\nStarted:    %s\n", tx.ID, tx.Action, tx.Status, tx.StartedAt.Format(time.RFC3339))
	if !tx.FinishedAt.IsZero() {
		fmt.Printf("Finished:   %s\n", tx.FinishedAt.Format(time.RFC3339))
	}
	if len(tx.Packages) > 0 {
		fmt.Printf("Packages:   %s\n", strings.Join(tx.Packages, ", "))
	}
	if tx.Error != "" {
		fmt.Printf("Error:      %s\n", tx.Error)
	}
	return nil
}

func (m *Manager) RunBackground(action string, args []string) error {
	if action == "install" && len(args) == 0 {
		return errors.New("install requires at least one package")
	}
	id := newID()
	tx := model.Transaction{ID: id, StartedAt: time.Now(), Action: action, Packages: append([]string(nil), args...), Status: "running"}
	if err := store.AddTransaction(tx); err != nil {
		return err
	}
	logDir, err := config.DataDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(logDir, "transactions"), 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(logDir, "transactions", id+".log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	cmd := exec.Command(os.Args[0], "__worker", action, id, "--", strings.Join(args, "\x00"))
	cmd.Env = append(os.Environ(), "YSPM_BACKGROUND_TX="+id)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	_ = logFile.Close()
	fmt.Printf("Transaction %s started in background.\n", id)
	return nil
}

func (m *Manager) Worker(action, id, packed string) error {
	var args []string
	if packed != "" {
		args = strings.Split(packed, "\x00")
	}
	tx, err := store.GetTransaction(id)
	if err != nil {
		return err
	}
	err = func() error {
		switch action {
		case "install":
			return m.InstallMany(args, true)
		case "remove":
			return m.RemoveMany(args, true)
		case "upgrade":
			return m.Upgrade(true)
		case "autoremove":
			return m.Autoremove(true)
		default:
			return fmt.Errorf("unknown background action %q", action)
		}
	}()
	current, getErr := store.GetTransaction(id)
	if getErr != nil {
		return getErr
	}
	if current.Status == "running" {
		tx.Status = "failed"
		if err == nil {
			tx.Status = "success"
		} else {
			tx.Error = err.Error()
		}
		tx.FinishedAt = time.Now()
		return store.UpdateTransaction(tx)
	}
	return err
}

func (m *Manager) runTransaction(action string, requested []string, yes bool, upgrade bool) error {
	return withLock(func() error {
		if upgrade {
			if err := m.Update(); err != nil {
				return err
			}
		}
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
		}
		if db.Release != idx.Release {
			return fmt.Errorf("installed packages belong to release %s, repository is release %s", db.Release, idx.Release)
		}
		plan, err := m.resolve(idx, db, requested, upgrade)
		if err != nil {
			return err
		}
		explicitChanged := false
		for _, name := range requested {
			if pkg, ok := db.Packages[name]; ok && !pkg.Explicit {
				pkg.Explicit = true
				db.Packages[name] = pkg
				explicitChanged = true
			}
		}
		if len(plan.Packages) == 0 {
			if explicitChanged {
				if err := store.SaveDB(db); err != nil {
					return err
				}
			}
			fmt.Println("Nothing to do.")
			return nil
		}
		if !yes {
			printInstallPlan(plan, db)
			fmt.Print("Continue? [y/N] ")
			var ans string
			_, _ = fmt.Scanln(&ans)
			if strings.ToLower(strings.TrimSpace(ans)) != "y" {
				fmt.Println("Aborted.")
				return nil
			}
		}
		tx := m.startTransaction(action, plan.Requested)
		db, err = store.LoadDB()
		if err != nil {
			return m.finishFailed(tx, err)
		}
		results, err := m.prepareDownloadsAndStages(plan.Packages)
		if err != nil {
			return m.finishFailed(tx, err)
		}
		if err := validatePlannedFiles(results, db); err != nil {
			cleanupStages(results)
			return m.finishFailed(tx, err)
		}
		old := cloneDB(db)
		state, err := commitResults(results)
		if err != nil {
			cleanupStages(results)
			return m.finishFailed(tx, err)
		}
		for _, p := range results {
			desktopFile := ""
			if p.pkg.Desktop || p.pkg.Kind == "app" || p.pkg.Kind == "appimage" {
				if dir, e := config.ApplicationsDir(); e == nil {
					desktopFile = filepath.Join(dir, p.pkg.Name+".desktop")
				}
			}
			installed := model.InstalledPackage{Name: p.pkg.Name, Version: p.pkg.Version, Revision: p.pkg.Revision, Kind: p.pkg.Kind, Dependencies: append([]model.Dependency(nil), p.pkg.Dependencies...), InstallDir: finalInstallDir(p.pkg), Executable: p.executable, Command: p.command, DesktopFile: desktopFile, Files: p.files, Checksum: p.pkg.SHA256, Explicit: containsName(plan.Requested, p.pkg.Name), InstalledAt: time.Now()}
			db.Packages[p.pkg.Name] = installed
		}
		for name, p := range db.Packages {
			if containsPackage(results, name) {
				p.Explicit = p.Explicit || containsName(plan.Requested, name)
				db.Packages[name] = p
			}
		}
		if err := store.SaveDB(db); err != nil {
			state.Rollback()
			return m.finishFailed(tx, err)
		}
		state.Finalize()
		cleanupOldVersions(old, db, results)
		for _, r := range results {
			cleanupStage(r)
		}
		fmt.Printf("Transaction complete: %d package(s).\n", len(results))
		return m.finishSuccess(tx)
	})
}

func (m *Manager) resolve(idx model.Index, db model.Database, requested []string, upgrade bool) (resolvedPlan, error) {
	chosen := map[string]model.Package{}
	constraints := map[string][]string{}
	queue := append([]string(nil), requested...)
	if upgrade {
		for name, installed := range db.Packages {
			candidate, err := m.findSatisfying(name, "", idx)
			if err == nil && (compareVersion(candidate.Version, installed.Version) > 0 || candidate.Revision > installed.Revision) {
				queue = append(queue, name)
			}
		}
	}
	seenQueue := map[string]bool{}
	for len(queue) > 0 {
		nameConstraint := queue[0]
		queue = queue[1:]
		r := parseDependency(nameConstraint)
		constraints[r.Name] = append(constraints[r.Name], r.Op+r.Version)
		if seenQueue[nameConstraint] {
			continue
		}
		seenQueue[nameConstraint] = true
		p, err := chooseWithConstraints(r.Name, constraints[r.Name], idx)
		if err != nil {
			return resolvedPlan{}, fmt.Errorf("resolve %q: %w", r.Name, err)
		}
		chosen[p.Name] = p
		for _, dep := range p.Dependencies {
			queue = append(queue, string(dep))
		}
	}
	for name, p := range chosen {
		for _, c := range p.Conflicts {
			for installedName := range db.Packages {
				if installedName == name {
					continue
				}
				if installedName == c && !containsName(requested, installedName) {
					return resolvedPlan{}, fmt.Errorf("%s conflicts with installed package %s", name, c)
				}
			}
		}
	}
	chosenNames := make([]string, 0, len(chosen))
	for name := range chosen {
		chosenNames = append(chosenNames, name)
	}
	sort.Strings(chosenNames)
	for _, name := range chosenNames {
		for _, conflict := range chosen[name].Conflicts {
			if _, ok := chosen[conflict]; ok {
				return resolvedPlan{}, fmt.Errorf("%s conflicts with %s in the requested transaction", name, conflict)
			}
		}
	}
	var packages []model.Package
	for _, p := range chosen {
		current, ok := db.Packages[p.Name]
		if !ok || compareVersion(p.Version, current.Version) > 0 || p.Revision > current.Revision {
			packages = append(packages, p)
		}
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].Name < packages[j].Name })
	return resolvedPlan{Packages: packages, Requested: requested}, nil
}

func chooseWithConstraints(name string, constraints []string, idx model.Index) (model.Package, error) {
	var candidates []model.Package
	for _, p := range idx.Packages {
		if repo.SupportsCurrentSystem(p) && matchesName(&p, name) && allSatisfied(p.Version, constraints) {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		if strings.Contains(name, "|") {
			for _, alt := range strings.Split(name, "|") {
				if p, e := chooseWithConstraints(strings.TrimSpace(alt), constraints, idx); e == nil {
					return p, nil
				}
			}
		}
		return model.Package{}, fmt.Errorf("no candidate satisfies %s (%s)", name, strings.Join(constraints, ", "))
	}
	sort.Slice(candidates, func(i, j int) bool {
		c := compareVersion(candidates[i].Version, candidates[j].Version)
		if c == 0 {
			return candidates[i].Revision > candidates[j].Revision
		}
		return c > 0
	})
	return candidates[0], nil
}

func allSatisfied(version string, constraints []string) bool {
	for _, c := range constraints {
		if !satisfies(version, c) {
			return false
		}
	}
	return true
}

func parseDependency(s string) dependencyRequest {
	s = strings.TrimSpace(s)
	ops := []string{"!=", ">=", "<=", "=", ">", "<"}
	for _, op := range ops {
		if i := strings.Index(s, op); i > 0 {
			return dependencyRequest{Name: strings.TrimSpace(s[:i]), Op: op, Version: strings.TrimSpace(s[i+len(op):])}
		}
	}
	return dependencyRequest{Name: s}
}
func satisfies(version, constraint string) bool {
	if constraint == "" {
		return true
	}
	r := parseDependency("x" + constraint)
	switch r.Op {
	case "=":
		return compareVersion(version, r.Version) == 0
	case "!=":
		return compareVersion(version, r.Version) != 0
	case ">":
		return compareVersion(version, r.Version) > 0
	case ">=":
		return compareVersion(version, r.Version) >= 0
	case "<":
		return compareVersion(version, r.Version) < 0
	case "<=":
		return compareVersion(version, r.Version) <= 0
	default:
		return true
	}
}

func printInstallPlan(plan resolvedPlan, db model.Database) {
	fmt.Printf("Transaction plan (%d package(s)):\n", len(plan.Packages))
	for _, p := range plan.Packages {
		if old, ok := db.Packages[p.Name]; ok {
			fmt.Printf("  upgrade %-16s %s -> %s\n", p.Name, old.Version, p.Version)
		} else {
			fmt.Printf("  install %-16s %s\n", p.Name, p.Version)
		}
	}
}

func (m *Manager) prepareDownloadsAndStages(packages []model.Package) ([]installResult, error) {
	cache, err := config.CacheDir()
	if err != nil {
		return nil, err
	}
	dataDir, err := config.DataDir()
	if err != nil {
		return nil, err
	}
	stageRoot := filepath.Join(dataDir, ".staging")
	if err := os.MkdirAll(filepath.Join(cache, "packages"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(stageRoot, 0o755); err != nil {
		return nil, err
	}

	results := make([]installResult, len(packages))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	errCh := make(chan error, len(packages))

	for i, pkg := range packages {
		i, pkg := i, pkg
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := repo.ValidateInstallPackage(pkg); err != nil {
				errCh <- err
				return
			}

			archive := filepath.Join(cache, "packages", packageFilename(pkg))
			valid := false
			if _, err := os.Stat(archive); err == nil && pkg.SHA256 != "" {
				valid = repo.VerifySHA256(archive, pkg.SHA256) == nil
			}
			if !valid {
				fmt.Printf("Downloading %s %s...\n", pkg.Name, pkg.Version)
				if err := repo.Download(pkg.URL, archive); err != nil {
					errCh <- fmt.Errorf("download %s: %w", pkg.Name, err)
					return
				}
				if err := repo.VerifySHA256(archive, pkg.SHA256); err != nil {
					_ = os.Remove(archive)
					errCh <- fmt.Errorf("verify %s: %w", pkg.Name, err)
					return
				}
			}

			stage, err := os.MkdirTemp(stageRoot, pkg.Name+"-*")
			if err != nil {
				errCh <- err
				return
			}

			var executable string
			switch pkg.Kind {
			case "appimage":
				name := filepath.Base(pkg.Entry)
				if name == "." || name == "" {
					name = pkg.Name + ".AppImage"
				}
				target := filepath.Join(stage, name)
				if err := copyFile(archive, target, 0o755); err != nil {
					_ = os.RemoveAll(stage)
					errCh <- err
					return
				}
				executable = target
			case "meta":
			default:
				if err := repo.ExtractArchive(archive, pkg.Format, stage); err != nil {
					_ = os.RemoveAll(stage)
					errCh <- fmt.Errorf("extract %s: %w", pkg.Name, err)
					return
				}
			}

			if pkg.Kind != "meta" && pkg.Kind != "appimage" {
				executable, err = repo.FindEntry(stage, pkg.Entry)
				if err != nil {
					_ = os.RemoveAll(stage)
					errCh <- fmt.Errorf("locate executable for %s: %w", pkg.Name, err)
					return
				}
			}
			if executable != "" {
				if info, err := os.Stat(executable); err == nil && info.Mode()&0o111 == 0 {
					_ = os.Chmod(executable, info.Mode()|0o755)
				}
			}

			files, err := repo.FileList(stage)
			if err != nil {
				_ = os.RemoveAll(stage)
				errCh <- err
				return
			}
			commandName := pkg.Command
			if commandName == "" {
				commandName = pkg.Name
			}
			results[i] = installResult{pkg: pkg, archive: archive, stage: stage, executable: executable, files: files, command: commandName}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		cleanupStages(results)
		return nil, err
	}
	return results, nil
}

func packageFilename(p model.Package) string {
	if p.Kind == "appimage" || p.Format == "appimage" {
		return p.Name + "-" + p.Version + ".AppImage"
	}
	if p.Format == "" {
		return p.Name + "-" + p.Version
	}
	return p.Name + "-" + p.Version + "." + strings.ReplaceAll(p.Format, "/", "-")
}
func finalInstallDir(p model.Package) string {
	d, _ := config.DataDir()
	return filepath.Join(d, "packages", p.Name, p.Version)
}

func validatePlannedFiles(results []installResult, db model.Database) error {
	owned := map[string]string{}
	for name, p := range db.Packages {
		for _, f := range p.Files {
			owned[f] = name
		}
	}
	for _, r := range results {
		for _, f := range r.files {
			if owner, ok := owned[f]; ok && owner != r.pkg.Name && !containsString(r.pkg.Replaces, owner) {
				return fmt.Errorf("file conflict: %s is owned by %s", f, owner)
			}
		}
	}
	return nil
}

type commitRecord struct {
	result         installResult
	dest           string
	backup         string
	link           string
	oldLinkTarget  string
	linkWasPresent bool
	desktop        string
	oldDesktop     []byte
	desktopMode    os.FileMode
	desktopPresent bool
	committed      bool
}

type commitState struct{ records []*commitRecord }

func (s *commitState) Rollback() {
	for i := len(s.records) - 1; i >= 0; i-- {
		r := s.records[i]
		if r.link != "" {
			_ = os.Remove(r.link)
			if r.linkWasPresent {
				_ = os.Symlink(r.oldLinkTarget, r.link)
			}
		}
		if r.desktop != "" {
			_ = os.Remove(r.desktop)
			if r.desktopPresent {
				_ = os.WriteFile(r.desktop, r.oldDesktop, r.desktopMode.Perm())
			}
		}
		if r.committed {
			_ = os.RemoveAll(r.dest)
		}
		if r.backup != "" {
			if _, err := os.Stat(r.backup); err == nil {
				_ = os.Rename(r.backup, r.dest)
			}
		}
	}
}

func (s *commitState) Finalize() {
	for _, r := range s.records {
		if r.backup != "" {
			_ = os.RemoveAll(r.backup)
		}
	}
}

func commitResults(results []installResult) (*commitState, error) {
	state := &commitState{records: make([]*commitRecord, 0, len(results))}
	fail := func(err error) (*commitState, error) { state.Rollback(); return nil, err }

	for _, item := range results {
		rec := &commitRecord{result: item, dest: finalInstallDir(item.pkg)}
		state.records = append(state.records, rec)
		if item.pkg.Kind == "meta" {
			if err := os.MkdirAll(rec.dest, 0o755); err != nil {
				return fail(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(rec.dest), 0o755); err != nil {
			return fail(err)
		}

		if _, err := os.Stat(rec.dest); err == nil {
			rec.backup = rec.dest + ".old"
			_ = os.RemoveAll(rec.backup)
			if err := os.Rename(rec.dest, rec.backup); err != nil {
				return fail(err)
			}
		}
		if err := os.Rename(item.stage, rec.dest); err != nil {
			if rec.backup != "" {
				_ = os.Rename(rec.backup, rec.dest)
			}
			return fail(err)
		}
		rec.committed = true

		if item.command != "" {
			bin, err := config.BinDir()
			if err != nil {
				return fail(err)
			}
			if err := os.MkdirAll(bin, 0o755); err != nil {
				return fail(err)
			}
			rec.link = filepath.Join(bin, item.command)
			if info, err := os.Lstat(rec.link); err == nil {
				if info.Mode()&os.ModeSymlink == 0 {
					return fail(fmt.Errorf("refusing to overwrite existing non-symlink %s", rec.link))
				}
				rec.oldLinkTarget, err = os.Readlink(rec.link)
				if err != nil {
					return fail(err)
				}
				rec.linkWasPresent = true
				if err := os.Remove(rec.link); err != nil {
					return fail(err)
				}
			} else if !os.IsNotExist(err) {
				return fail(err)
			}
			if err := linkCommand(filepath.Join(rec.dest, relFromStage(item)), rec.link); err != nil {
				return fail(err)
			}
		}

		if item.pkg.Desktop || item.pkg.Kind == "app" || item.pkg.Kind == "appimage" {
			dir, err := config.ApplicationsDir()
			if err != nil {
				return fail(err)
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fail(err)
			}
			rec.desktop = filepath.Join(dir, item.pkg.Name+".desktop")
			if info, err := os.Stat(rec.desktop); err == nil {
				rec.oldDesktop, err = os.ReadFile(rec.desktop)
				if err != nil {
					return fail(err)
				}
				rec.desktopMode = info.Mode()
				rec.desktopPresent = true
			} else if !os.IsNotExist(err) {
				return fail(err)
			}
			if err := installDesktopEntry(item.pkg, item.command); err != nil {
				return fail(err)
			}
		}
	}
	return state, nil
}

func relFromStage(r installResult) string {
	if r.executable == "" {
		return ""
	}
	rel, _ := filepath.Rel(r.stage, r.executable)
	return rel
}

func installDesktopEntry(p model.Package, command string) error {
	if command == "" {
		return nil
	}
	dir, err := config.ApplicationsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := p.DesktopName
	if name == "" {
		name = p.Name
	}
	cats := strings.Join(p.Categories, ";")
	if cats == "" {
		cats = "Utility;"
	}
	content := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=%s\nComment=%s\nExec=%s %%U\nTerminal=false\nCategories=%s\n", escapeDesktop(name), escapeDesktop(p.Description), command, cats)
	path := filepath.Join(dir, p.Name+".desktop")
	return os.WriteFile(path, []byte(content), 0o644)
}
func escapeDesktop(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "\n", " ")
}

type removalRecord struct {
	packageInfo    model.InstalledPackage
	backup         string
	link           string
	oldLinkTarget  string
	linkPresent    bool
	desktop        string
	oldDesktop     []byte
	desktopMode    os.FileMode
	desktopPresent bool
}

type removalState struct{ records []*removalRecord }

func (s *removalState) Rollback() {
	for i := len(s.records) - 1; i >= 0; i-- {
		r := s.records[i]
		if r.link != "" {
			_ = os.Remove(r.link)
			if r.linkPresent {
				_ = os.Symlink(r.oldLinkTarget, r.link)
			}
		}
		if r.desktop != "" {
			_ = os.Remove(r.desktop)
			if r.desktopPresent {
				_ = os.WriteFile(r.desktop, r.oldDesktop, r.desktopMode.Perm())
			}
		}
		if r.backup != "" {
			if _, err := os.Stat(r.backup); err == nil {
				_ = os.Rename(r.backup, r.packageInfo.InstallDir)
			}
		}
	}
}

func (s *removalState) Finalize() {
	for _, r := range s.records {
		if r.backup != "" {
			_ = os.RemoveAll(r.backup)
		}
	}
}

func stageRemoval(db model.Database, packages []model.InstalledPackage, txID string) (*removalState, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return nil, err
	}
	stageRoot := filepath.Join(dataDir, ".staging", "remove-"+txID)
	if err := os.MkdirAll(stageRoot, 0o755); err != nil {
		return nil, err
	}
	state := &removalState{}
	fail := func(err error) (*removalState, error) { state.Rollback(); return nil, err }
	bin, err := config.BinDir()
	if err != nil {
		return nil, err
	}
	apps, err := config.ApplicationsDir()
	if err != nil {
		return nil, err
	}
	for _, p := range packages {
		r := &removalRecord{packageInfo: p}
		state.records = append(state.records, r)
		if p.InstallDir != "" {
			if _, err := os.Stat(p.InstallDir); err == nil {
				r.backup = filepath.Join(stageRoot, fmt.Sprintf("%s-%d", p.Name, len(state.records)))
				if err := os.Rename(p.InstallDir, r.backup); err != nil {
					return fail(err)
				}
			}
		}
		if p.Command != "" {
			r.link = filepath.Join(bin, p.Command)
			if info, err := os.Lstat(r.link); err == nil {
				if info.Mode()&os.ModeSymlink == 0 {
					return fail(fmt.Errorf("refusing to remove non-symlink %s", r.link))
				}
				r.oldLinkTarget, err = os.Readlink(r.link)
				if err != nil {
					return fail(err)
				}
				r.linkPresent = true
				if err := os.Remove(r.link); err != nil {
					return fail(err)
				}
			} else if !os.IsNotExist(err) {
				return fail(err)
			}
		}
		r.desktop = filepath.Join(apps, p.Name+".desktop")
		if info, err := os.Stat(r.desktop); err == nil {
			r.oldDesktop, err = os.ReadFile(r.desktop)
			if err != nil {
				return fail(err)
			}
			r.desktopMode = info.Mode()
			r.desktopPresent = true
			if err := os.Remove(r.desktop); err != nil {
				return fail(err)
			}
		} else if !os.IsNotExist(err) {
			return fail(err)
		}
		delete(db.Packages, p.Name)
	}
	return state, nil
}

func dependsOn(deps []model.Dependency, name string) bool {
	for _, d := range deps {
		if parseDependency(string(d)).Name == name {
			return true
		}
	}
	return false
}
func containsName(list []string, name string) bool {
	for _, x := range list {
		if x == name {
			return true
		}
	}
	return false
}
func containsString(list []string, name string) bool { return containsName(list, name) }
func containsPackage(results []installResult, name string) bool {
	for _, r := range results {
		if r.pkg.Name == name {
			return true
		}
	}
	return false
}

func withLock(fn func() error) error {
	data, err := config.DataDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(data, 0o755); err != nil {
		return err
	}
	path := filepath.Join(data, "lock")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return errors.New("another yspm transaction is already running")
		}
		return err
	}
	defer func() { _ = f.Close(); _ = os.Remove(path) }()
	return fn()
}
func cloneDB(db model.Database) model.Database {
	out := model.Database{Release: db.Release, Packages: map[string]model.InstalledPackage{}, Transactions: append([]model.Transaction(nil), db.Transactions...)}
	for k, v := range db.Packages {
		v.Files = append([]string(nil), v.Files...)
		v.Dependencies = append([]model.Dependency(nil), v.Dependencies...)
		out.Packages[k] = v
	}
	return out
}
func cleanupOldVersions(old, current model.Database, results []installResult) {
	for _, r := range results {
		previous, ok := old.Packages[r.pkg.Name]
		if !ok || previous.InstallDir == "" {
			continue
		}
		installed := current.Packages[r.pkg.Name]
		if previous.InstallDir != installed.InstallDir {
			_ = os.RemoveAll(previous.InstallDir)
		}
	}
}

func cleanupStages(results []installResult) {
	for _, r := range results {
		if r.stage != "" {
			_ = os.RemoveAll(r.stage)
		}
	}
}
func cleanupStage(r installResult) {
	if r.stage != "" {
		_ = os.RemoveAll(r.stage)
	}
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
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, ce := io.Copy(out, in)
	ee := out.Close()
	if ce != nil {
		return ce
	}
	return ee
}
func linkCommand(executable, link string) error {
	if executable == "" {
		return errors.New("cannot create command link: executable not found")
	}
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

func (m *Manager) startTransaction(action string, packages []string) model.Transaction {
	if id := os.Getenv("YSPM_BACKGROUND_TX"); id != "" {
		if tx, err := store.GetTransaction(id); err == nil {
			return tx
		}
	}
	tx := model.Transaction{ID: newID(), StartedAt: time.Now(), Action: action, Packages: append([]string(nil), packages...), Status: "running"}
	_ = store.AddTransaction(tx)
	return tx
}
func (m *Manager) finishSuccess(tx model.Transaction) error {
	tx.Status = "success"
	tx.FinishedAt = time.Now()
	return store.UpdateTransaction(tx)
}
func (m *Manager) finishFailed(tx model.Transaction, err error) error {
	tx.Status = "failed"
	tx.Error = err.Error()
	tx.FinishedAt = time.Now()
	_ = store.UpdateTransaction(tx)
	return err
}
func newID() string { buf := make([]byte, 6); _, _ = rand.Read(buf); return hex.EncodeToString(buf) }
