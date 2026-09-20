package repo

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Yassine-Jemi01/yspm/internal/model"
)

const HardcorePackageFormat = "hardcore-tar"

type HardcorePackageData struct {
	Package         model.Package
	InstallScript   string
	UninstallScript string
}

func FetchHardcoreIndex(source string) (model.Index, error) {
	listSource, base, err := hardcoreListSource(source)
	if err != nil {
		return model.Index{}, err
	}
	data, err := readSource(listSource)
	if err != nil {
		return model.Index{}, fmt.Errorf("fetch HardcoreLinux package list: %w", err)
	}
	idx := model.Index{
		Repository: source,
		Release:    "hardcore-main",
		Channel:    "stable",
		Generated:  time.Now().UTC().Format(time.RFC3339),
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return model.Index{}, fmt.Errorf("invalid HardcoreLinux list line %d", lineNo)
		}
		checksum, filename, name := fields[0], fields[1], fields[2]
		if len(checksum) != 64 {
			return model.Index{}, fmt.Errorf("invalid SHA-256 for %s", filename)
		}
		if _, err := hex.DecodeString(checksum); err != nil {
			return model.Index{}, fmt.Errorf("invalid SHA-256 for %s: %w", filename, err)
		}
		if filepath.Base(filename) != filename {
			return model.Index{}, fmt.Errorf("unsafe package filename %q", filename)
		}
		idx.Packages = append(idx.Packages, model.Package{
			Name: name, Version: "legacy", Description: "HardcoreLinux legacy package",
			OS: "linux", Architecture: "", License: "unknown", Kind: "system",
			Format: HardcorePackageFormat,
			URL: strings.TrimRight(base, "/") + "/" + filename,
			SHA256: checksum,
		})
	}
	if err := scanner.Err(); err != nil {
		return model.Index{}, err
	}
	if len(idx.Packages) == 0 {
		return model.Index{}, fmt.Errorf("HardcoreLinux repository contains no packages")
	}
	sort.Slice(idx.Packages, func(i, j int) bool {
		return idx.Packages[i].Name < idx.Packages[j].Name
	})
	return idx, nil
}

func hardcoreListSource(source string) (string, string, error) {
	if source == "" {
		source = "."
	}
	if u, err := url.Parse(source); err == nil && u.Scheme != "" && u.Scheme != "file" {
		base := strings.TrimRight(source, "/")
		if strings.HasSuffix(base, "/list.sha256") {
			return base, strings.TrimSuffix(base, "/list.sha256"), nil
		}
		return base + "/list.sha256", base, nil
	}
	if strings.HasPrefix(source, "file://") {
		u, err := url.Parse(source)
		if err != nil {
			return "", "", err
		}
		source = u.Path
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return filepath.Join(source, "list.sha256"), "file://" + filepath.ToSlash(source), nil
	}
	if filepath.Base(source) != "list.sha256" {
		return "", "", fmt.Errorf("HardcoreLinux source must be a repo directory or list.sha256")
	}
	return source, "file://" + filepath.ToSlash(filepath.Dir(source)), nil
}

func InspectHardcoreArchive(path string) (HardcorePackageData, error) {
	tmp, err := os.MkdirTemp("", "yspm-hardcore-*")
	if err != nil {
		return HardcorePackageData{}, err
	}
	defer os.RemoveAll(tmp)
	if err := ExtractArchive(path, "tar", tmp); err != nil {
		return HardcorePackageData{}, fmt.Errorf("extract legacy package: %w", err)
	}
	return ReadHardcorePackageRoot(tmp)
}

func ReadHardcorePackageRoot(root string) (HardcorePackageData, error) {
	installerRoot := filepath.Join(root, "etc", "installer")
	entries, err := os.ReadDir(installerRoot)
	if err != nil {
		return HardcorePackageData{}, fmt.Errorf("legacy package has no /etc/installer: %w", err)
	}
	var found []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(installerRoot, entry.Name(), "pkinfo")); err == nil {
			found = append(found, entry.Name())
		}
	}
	if len(found) != 1 {
		return HardcorePackageData{}, fmt.Errorf("legacy package must contain exactly one installer directory (found %d)", len(found))
	}
	return ReadHardcoreInstaller(filepath.Join(installerRoot, found[0]))
}

func ReadHardcoreInstaller(installerDir string) (HardcorePackageData, error) {
	b, err := os.ReadFile(filepath.Join(installerDir, "pkinfo"))
	if err != nil {
		return HardcorePackageData{}, err
	}
	fields := strings.Fields(string(b))
	if len(fields) < 2 {
		return HardcorePackageData{}, fmt.Errorf("invalid pkinfo in %s", installerDir)
	}
	name := filepath.Base(installerDir)
	if fields[0] != name {
		return HardcorePackageData{}, fmt.Errorf("pkinfo package %q does not match installer directory %q", fields[0], name)
	}
	p := model.Package{
		Name: name, Version: fields[1], Description: "HardcoreLinux legacy package",
		OS: "linux", License: "unknown", Kind: "system", Format: HardcorePackageFormat,
	}
	for _, dep := range fields[2:] {
		if dep != "" {
			p.Dependencies = append(p.Dependencies, model.Dependency(dep))
		}
	}
	install, _ := os.ReadFile(filepath.Join(installerDir, "install"))
	uninstall, _ := os.ReadFile(filepath.Join(installerDir, "uninstall"))
	return HardcorePackageData{
		Package: p, InstallScript: string(install), UninstallScript: string(uninstall),
	}, nil
}

func HardcoreInstalledManifest(systemRoot, name string) ([]model.FileEntry, error) {
	installerDir := filepath.Join(systemRoot, "etc", "installer", name)
	data, err := ReadHardcoreInstaller(installerDir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, p := range parseHardcoreUninstallPaths(data.UninstallScript) {
		seen[p] = true
	}
	_ = filepath.Walk(installerDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || path == installerDir {
			return err
		}
		rel, er := filepath.Rel(systemRoot, path)
		if er == nil {
			seen[filepath.ToSlash(rel)] = true
		}
		return nil
	})

	var out []model.FileEntry
	for rel := range seen {
		target := filepath.Join(systemRoot, filepath.FromSlash(rel))
		info, err := os.Lstat(target)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		entry := model.FileEntry{Path: filepath.ToSlash(rel), Mode: uint32(info.Mode().Perm())}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			entry.Type = "symlink"
			entry.LinkTarget, _ = os.Readlink(target)
		case info.IsDir():
			entry.Type = "dir"
		case info.Mode().IsRegular():
			entry.Type = "file"
			entry.SHA256, _ = fileSHA256(target)
		default:
			continue
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func parseHardcoreUninstallPaths(script string) []string {
	var out []string
	for _, line := range strings.Split(script, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		if fields[0] != "rm" && fields[0] != "rmdir" {
			continue
		}
		for _, arg := range fields[1:] {
			if strings.HasPrefix(arg, "-") || strings.Contains(arg, "$") || strings.ContainsAny(arg, "'\"*?") {
				continue
			}
			p := strings.TrimPrefix(arg, "./")
			if p == "" {
				continue
			}
			if !strings.HasPrefix(p, "/") {
				p = "/" + p
			}
			p = strings.TrimPrefix(filepath.Clean(p), "/")
			if p == "." || p == ".." || strings.HasPrefix(p, "../") {
				continue
			}
			out = append(out, filepath.ToSlash(p))
			break
		}
	}
	return out
}
