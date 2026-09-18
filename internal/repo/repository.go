package repo

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Yassine-Jemi01/yspm/internal/config"
	"github.com/Yassine-Jemi01/yspm/internal/model"
)

var httpClient = &http.Client{Timeout: 2 * time.Minute}

func FetchIndex(repository string) (model.Index, error) {
	data, err := readResource(resolveIndexURL(repository))
	if err != nil {
		return model.Index{}, err
	}
	var idx model.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return model.Index{}, fmt.Errorf("invalid repository index: %w", err)
	}
	if idx.Repository == "" {
		idx.Repository = repository
	}
	if err := ValidateStableIndex(idx); err != nil {
		return model.Index{}, err
	}
	for i := range idx.Packages {
		if idx.Packages[i].Name == "" || idx.Packages[i].Version == "" || idx.Packages[i].Kind == "" {
			return model.Index{}, fmt.Errorf("repository index contains an invalid package entry")
		}
		if !SupportsCurrentSystem(idx.Packages[i]) {
			continue
		}
		idx.Packages[i].URL = resolveURL(resolveIndexURL(repository), idx.Packages[i].URL)
	}
	return idx, nil
}

func CacheIndex(idx model.Index) error {
	dir, err := config.CacheDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "repos"), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "repos", "default.json")
	return atomicWrite(path, append(data, '\n'))
}

func LoadCachedIndex() (model.Index, error) {
	dir, err := config.CacheDir()
	if err != nil {
		return model.Index{}, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "repos", "default.json"))
	if err != nil {
		return model.Index{}, err
	}
	var idx model.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return model.Index{}, err
	}
	return idx, nil
}

func Download(resource, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".download-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Close(); err != nil {
		return err
	}

	if isHTTP(resource) {
		req, err := http.NewRequest(http.MethodGet, resource, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "yspm/0.2")
		resp, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("download failed: HTTP %s", resp.Status)
		}
		out, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, resp.Body)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	} else {
		path := resource
		if strings.HasPrefix(resource, "file://") {
			u, err := url.Parse(resource)
			if err != nil {
				return err
			}
			path = u.Path
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		out, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, input)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return os.Rename(tmpPath, destination)
}

func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func VerifySHA256(path, expected string) error {
	if expected == "" {
		return nil
	}
	got, err := SHA256File(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("sha256 mismatch: expected %s, got %s", expected, got)
	}
	return nil
}

// ExtractArchive safely extracts tar.gz, tar.xz and zip packages.
func ExtractArchive(archivePath, format, destination string) (string, error) {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return "", err
	}
	switch strings.ToLower(format) {
	case "tar.gz":
		f, err := os.Open(archivePath)
		if err != nil {
			return "", err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return "", fmt.Errorf("open gzip archive: %w", err)
		}
		defer gz.Close()
		return extractTar(tar.NewReader(gz), destination)
	case "tar.xz":
		cmd := exec.Command("xz", "-dc", archivePath)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return "", err
		}
		if err := cmd.Start(); err != nil {
			return "", fmt.Errorf("run xz: %w", err)
		}
		entry, extractErr := extractTar(tar.NewReader(stdout), destination)
		waitErr := cmd.Wait()
		if extractErr != nil {
			return "", extractErr
		}
		if waitErr != nil {
			return "", fmt.Errorf("xz extraction failed: %w", waitErr)
		}
		return entry, nil
	case "zip":
		return extractZip(archivePath, destination)
	default:
		return "", fmt.Errorf("unsupported archive format %q", format)
	}
}

func extractTar(tr *tar.Reader, destination string) (string, error) {
	var firstRoot string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		name, err := safeArchivePath(h.Name)
		if err != nil {
			return "", err
		}
		if firstRoot == "" {
			firstRoot = strings.Split(name, string(os.PathSeparator))[0]
		}
		target := filepath.Join(destination, name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", err
			}
			mode := os.FileMode(h.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return "", err
			}
			if err := out.Close(); err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("unsupported archive entry %q", h.Name)
		}
	}
	return firstRoot, nil
}

func extractZip(archivePath, destination string) (string, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	var firstRoot string
	for _, f := range zr.File {
		name, err := safeArchivePath(f.Name)
		if err != nil {
			return "", err
		}
		if firstRoot == "" {
			firstRoot = strings.Split(name, string(os.PathSeparator))[0]
		}
		target := filepath.Join(destination, name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		in, err := f.Open()
		if err != nil {
			return "", err
		}
		mode := f.Mode() & 0o777
		if mode == 0 {
			mode = 0o644
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			in.Close()
			return "", err
		}
		_, copyErr := io.Copy(out, in)
		closeInErr := in.Close()
		closeOutErr := out.Close()
		if copyErr != nil {
			return "", copyErr
		}
		if closeInErr != nil {
			return "", closeInErr
		}
		if closeOutErr != nil {
			return "", closeOutErr
		}
	}
	return firstRoot, nil
}

func safeArchivePath(name string) (string, error) {
	if strings.Contains(name, "\\") {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	name = filepath.Clean(filepath.FromSlash(name))
	if name == "." || filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return name, nil
}

func FindEntry(root, entry string) (string, error) {
	if entry == "" {
		return "", fmt.Errorf("package has no executable entry")
	}
	candidate := filepath.Join(root, filepath.FromSlash(entry))
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate, nil
	}
	var matches []string
	base := filepath.Base(filepath.FromSlash(entry))
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) == base {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("could not uniquely locate executable %q (found %d matches)", entry, len(matches))
	}
	return matches[0], nil
}

func SupportsCurrentSystem(p model.Package) bool {
	if p.OS != "" && p.OS != runtime.GOOS {
		return false
	}
	if p.Architecture != "" && p.Architecture != runtime.GOARCH {
		return false
	}
	return true
}

func CompareVersion(a, b string) int {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == b {
		return 0
	}
	ap := strings.FieldsFunc(a, func(r rune) bool { return r == '.' || r == '-' || r == '+' })
	bp := strings.FieldsFunc(b, func(r rune) bool { return r == '.' || r == '-' || r == '+' })
	n := len(ap)
	if len(bp) > n {
		n = len(bp)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(ap) {
			av = numericPrefix(ap[i])
		}
		if i < len(bp) {
			bv = numericPrefix(bp[i])
		}
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
		if i < len(ap) && i < len(bp) && ap[i] != bp[i] {
			if ap[i] == "stable" {
				return 1
			}
			if bp[i] == "stable" {
				return -1
			}
			if ap[i] > bp[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

func numericPrefix(s string) int {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0
	}
	v, _ := strconv.Atoi(s[:i])
	return v
}

func resolveIndexURL(base string) string {
	if isHTTP(base) {
		if strings.HasSuffix(base, ".json") {
			return base
		}
		return strings.TrimRight(base, "/") + "/index.json"
	}
	if strings.HasPrefix(base, "file://") {
		return strings.TrimRight(base, "/") + "/index.json"
	}
	info, err := os.Stat(base)
	if err == nil && !info.IsDir() {
		return base
	}
	return filepath.Join(base, "index.json")
}

func resolveURL(base, child string) string {
	if child == "" {
		return ""
	}
	if isHTTP(child) || strings.HasPrefix(child, "file://") {
		return child
	}
	if isHTTP(base) {
		u, err := url.Parse(base)
		if err == nil {
			ref, err := url.Parse(child)
			if err == nil {
				return u.ResolveReference(ref).String()
			}
		}
	}
	path := base
	if strings.HasPrefix(path, "file://") {
		u, _ := url.Parse(path)
		path = u.Path
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		path = filepath.Dir(path)
	}
	return filepath.Join(path, child)
}

func readResource(resource string) ([]byte, error) {
	if isHTTP(resource) {
		req, err := http.NewRequest(http.MethodGet, resource, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "yspm/0.2")
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("request failed: HTTP %s", resp.Status)
		}
		return io.ReadAll(resp.Body)
	}
	if strings.HasPrefix(resource, "file://") {
		u, err := url.Parse(resource)
		if err != nil {
			return nil, err
		}
		resource = u.Path
	}
	return os.ReadFile(resource)
}

func isHTTP(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func SortPackages(pkgs []model.Package) {
	sort.Slice(pkgs, func(i, j int) bool {
		if pkgs[i].Name == pkgs[j].Name {
			return CompareVersion(pkgs[i].Version, pkgs[j].Version) > 0
		}
		return pkgs[i].Name < pkgs[j].Name
	})
}
