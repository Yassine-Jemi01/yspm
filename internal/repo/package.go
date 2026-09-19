package repo

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Yassine-Jemi01/yspm/internal/model"
)

const PackageFormat = "yspkg"

type PackageData struct {
	Metadata model.Package
	Scripts  map[string]string
}

func ReadPackageMetadata(path string) (model.Package, error) {
	f, err := os.Open(path)
	if err != nil { return model.Package{}, err }
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil { return model.Package{}, fmt.Errorf("open package: %w", err) }
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		h, err := tr.Next()
		if err == io.EOF { break }
		if err != nil { return model.Package{}, err }
		if h.Name == "metadata.json" {
			data, err := io.ReadAll(io.LimitReader(tr, 4<<20))
			if err != nil { return model.Package{}, err }
			var p model.Package
			if err := json.Unmarshal(data, &p); err != nil { return model.Package{}, fmt.Errorf("invalid package metadata: %w", err) }
			return p, nil
		}
	}
	return model.Package{}, errors.New("package does not contain metadata.json")
}

func ReadPackageData(path string) (PackageData, error) {
	f, err := os.Open(path)
	if err != nil { return PackageData{}, err }
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil { return PackageData{}, err }
	defer gr.Close()
	tr := tar.NewReader(gr)
	out := PackageData{Scripts: map[string]string{}}
	for {
		h, err := tr.Next()
		if err == io.EOF { break }
		if err != nil { return PackageData{}, err }
		switch {
		case h.Name == "metadata.json":
			data, err := io.ReadAll(io.LimitReader(tr, 4<<20))
			if err != nil { return PackageData{}, err }
			if err := json.Unmarshal(data, &out.Metadata); err != nil { return PackageData{}, err }
		case strings.HasPrefix(h.Name, "scripts/") && h.Typeflag == tar.TypeReg:
			data, err := io.ReadAll(io.LimitReader(tr, 8<<20))
			if err != nil { return PackageData{}, err }
			out.Scripts[strings.TrimPrefix(h.Name, "scripts/")] = string(data)
		}
	}
	if out.Metadata.Name == "" { return PackageData{}, errors.New("package metadata missing name") }
	return out, nil
}

func ExtractPackage(path, destination string) error {
	f, err := os.Open(path)
	if err != nil { return err }
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil { return err }
	defer gr.Close()
	tr := tar.NewReader(gr)
	root := filepath.Clean(destination)
	if err := os.MkdirAll(root, 0o755); err != nil { return err }
	for {
		h, err := tr.Next()
		if err == io.EOF { break }
		if err != nil { return err }
		name := filepath.ToSlash(h.Name)
		if name == "." || name == "" || name == "metadata.json" || strings.HasPrefix(name, "scripts/") {
			continue
		}
		if !strings.HasPrefix(name, "root/") {
			return fmt.Errorf("invalid package entry %q", h.Name)
		}
		rel := strings.TrimPrefix(name, "root/")
		rel = filepath.Clean(filepath.FromSlash(rel))
		if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe package path %q", h.Name)
		}
		target := filepath.Join(root, rel)
		if !withinRoot(root, target) { return fmt.Errorf("package escapes destination: %q", h.Name) }
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(h.Mode)); err != nil { return err }
			_ = os.Chmod(target, os.FileMode(h.Mode))
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil { return err }
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(h.Mode))
			if err != nil { return err }
			_, cpErr := io.Copy(out, tr)
			closeErr := out.Close()
			if cpErr != nil { return cpErr }
			if closeErr != nil { return closeErr }
			_ = os.Chmod(target, os.FileMode(h.Mode))
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil { return err }
			_ = os.Remove(target)
			if err := os.Symlink(h.Linkname, target); err != nil { return err }
		case tar.TypeLink:
			link := filepath.Clean(filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(h.Linkname, "root/"))))
			if !withinRoot(root, link) { return fmt.Errorf("hardlink escapes destination: %q", h.Linkname) }
			if err := os.Link(link, target); err != nil { return err }
		default:
			return fmt.Errorf("unsupported tar entry type %q for %s", h.Typeflag, h.Name)
		}
	}
	return nil
}

func ListPackageFiles(path string) ([]model.FileEntry, error) {
	f, err := os.Open(path)
	if err != nil { return nil, err }
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil { return nil, err }
	defer gr.Close()
	tr := tar.NewReader(gr)
	var out []model.FileEntry
	for {
		h, err := tr.Next()
		if err == io.EOF { break }
		if err != nil { return nil, err }
		if !strings.HasPrefix(h.Name, "root/") || h.Name == "root/" { continue }
		rel := filepath.ToSlash(strings.TrimPrefix(h.Name, "root/"))
		entry := model.FileEntry{Path: rel, Mode: uint32(h.Mode)}
		switch h.Typeflag {
		case tar.TypeDir:
			entry.Type = "dir"
		case tar.TypeSymlink:
			entry.Type, entry.LinkTarget = "symlink", h.Linkname
		case tar.TypeReg, tar.TypeRegA:
			entry.Type = "file"
			hash := sha256.New()
			if _, err := io.Copy(hash, tr); err != nil { return nil, err }
			entry.SHA256 = hex.EncodeToString(hash.Sum(nil))
		default:
			continue
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func BuildPackage(root, output string, p model.Package, scriptsDir string) error {
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() { return fmt.Errorf("package root is not a directory: %s", root) }
	if p.Format == "" { p.Format = PackageFormat }
	p.Kind = "system"
	if p.OS == "" { p.OS = "linux" }
	if p.Architecture == "" { p.Architecture = "x86_64" }
	if p.ABI == "" { p.ABI = "yspm-abi-1" }
	p.Files, err = filesystemManifest(root)
	if err != nil { return err }
	if len(p.SharedRequires) == 0 || len(p.SharedProvides) == 0 {
		req, prov := scanELFRequirements(root)
		if len(p.SharedRequires) == 0 { p.SharedRequires = req }
		if len(p.SharedProvides) == 0 { p.SharedProvides = prov }
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil { return err }
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil { return err }
	tmp, err := os.CreateTemp(filepath.Dir(output), ".yspkg-*")
	if err != nil { return err }
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	gw := gzip.NewWriter(tmp)
	tw := tar.NewWriter(gw)
	metaHeader := &tar.Header{Name: "metadata.json", Mode: 0o644, Size: int64(len(data)), ModTime: time.Now()}
	if err := tw.WriteHeader(metaHeader); err != nil { return err }
	if _, err := tw.Write(data); err != nil { return err }
	if scriptsDir != "" {
		entries, err := os.ReadDir(scriptsDir)
		if err != nil { return err }
		for _, e := range entries {
			if e.IsDir() { continue }
			name := e.Name()
			switch name {
			case "preinstall", "postinstall", "preremove", "postremove":
			default: continue
			}
			src := filepath.Join(scriptsDir, name)
			b, err := os.ReadFile(src)
			if err != nil { return err }
			h := &tar.Header{Name: "scripts/"+name, Mode: 0o755, Size: int64(len(b)), ModTime: time.Now()}
			if err := tw.WriteHeader(h); err != nil { return err }
			if _, err := tw.Write(b); err != nil { return err }
		}
	}
	err = addTree(tw, root, "root")
	if err != nil { return err }
	if err := tw.Close(); err != nil { return err }
	if err := gw.Close(); err != nil { return err }
	if err := tmp.Sync(); err != nil { _ = tmp.Close(); return err }
	if err := tmp.Close(); err != nil { return err }
	return os.Rename(tmpPath, output)
}

func addTree(tw *tar.Writer, root, prefix string) error {
	entries, err := os.ReadDir(root)
	if err != nil { return err }
	sort.Slice(entries, func(i,j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		full := filepath.Join(root, e.Name())
		info, err := os.Lstat(full)
		if err != nil { return err }
		rel := prefix + "/" + filepath.ToSlash(e.Name())
		h, err := tar.FileInfoHeader(info, "")
		if err != nil { return err }
		h.Name = rel
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(full)
			if err != nil { return err }
			h.Linkname = target
		}
		if err := tw.WriteHeader(h); err != nil { return err }
		if info.Mode().IsRegular() {
			f, err := os.Open(full)
			if err != nil { return err }
			_, cpErr := io.Copy(tw, f)
			closeErr := f.Close()
			if cpErr != nil { return cpErr }
			if closeErr != nil { return closeErr }
		}
		if info.IsDir() {
			if err := addTree(tw, full, rel); err != nil { return err }
		}
	}
	return nil
}

func filesystemManifest(root string) ([]model.FileEntry, error) {
	var out []model.FileEntry
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil { return err }
		if path == root { return nil }
		rel, err := filepath.Rel(root, path)
		if err != nil { return err }
		e := model.FileEntry{Path: filepath.ToSlash(rel), Mode: uint32(info.Mode().Perm())}
		if info.Mode()&os.ModeSymlink != 0 {
			e.Type = "symlink"
			target, err := os.Readlink(path)
			if err != nil { return err }
			e.LinkTarget = target
		} else if info.IsDir() {
			e.Type = "dir"
		} else if info.Mode().IsRegular() {
			e.Type = "file"
			h, err := fileSHA256(path)
			if err != nil { return err }
			e.SHA256 = h
		} else {
			return nil
		}
		out = append(out, e)
		return nil
	})
	sort.Slice(out, func(i,j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil { return "", err }
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil { return "", err }
	return hex.EncodeToString(h.Sum(nil)), nil
}

func scanELFRequirements(root string) ([]string, []string) {
	reqSet, provSet := map[string]bool{}, map[string]bool{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || !info.Mode().IsRegular() { return nil }
		readelf, err := exec.LookPath("readelf")
		if err != nil { return nil }
		out, err := exec.Command(readelf, "-d", path).Output()
		if err != nil { return nil }
		for _, line := range strings.Split(string(out), "
") {
			if i := strings.Index(line, "NEEDED"); i >= 0 {
				if j := strings.Index(line[i:], "["); j >= 0 {
					v := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line[i+j:]), "["), "]")
					if v != "" { reqSet[v] = true }
				}
			}
			if i := strings.Index(line, "SONAME"); i >= 0 {
				if j := strings.Index(line[i:], "["); j >= 0 {
					v := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line[i+j:]), "["), "]")
					if v != "" { provSet[v] = true }
				}
			}
		}
		return nil
	})
	req, prov := make([]string,0,len(reqSet)), make([]string,0,len(provSet))
	for v := range reqSet { req = append(req, v) }
	for v := range provSet { prov = append(prov, v) }
	sort.Strings(req); sort.Strings(prov)
	return req, prov
}

func withinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil { return false }
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func PackageSHA256(path string) (string, error) { return fileSHA256(path) }

func BuildRepository(dir, output, baseURL, release, abi string) (model.Index, error) {
	entries, err := os.ReadDir(dir)
	if err != nil { return model.Index{}, err }
	idx := model.Index{Repository: baseURL, Release: release, Channel: "stable", Generated: time.Now().UTC().Format(time.RFC3339), ABI: abi}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yspkg") { continue }
		path := filepath.Join(dir, e.Name())
		p, err := ReadPackageMetadata(path)
		if err != nil { return model.Index{}, err }
		sum, err := PackageSHA256(path)
		if err != nil { return model.Index{}, err }
		st, _ := os.Stat(path)
		p.URL = strings.TrimRight(baseURL, "/") + "/" + e.Name()
		p.SHA256, p.Size = sum, st.Size()
		idx.Packages = append(idx.Packages, p)
	}
	sort.Slice(idx.Packages, func(i,j int) bool { return idx.Packages[i].Name < idx.Packages[j].Name })
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil { return model.Index{}, err }
	if err := os.WriteFile(output, append(data,'\n'), 0o644); err != nil { return model.Index{}, err }
	return idx, nil
}

func SignRepositoryIndex(indexPath, privateKeyPath, signaturePath string) error {
	b, err := os.ReadFile(privateKeyPath)
	if err != nil { return err }
	key, err := decodePrivateKey(strings.TrimSpace(string(b)))
	if err != nil { return err }
	data, err := os.ReadFile(indexPath)
	if err != nil { return err }
	sig := ed25519.Sign(key, data)
	encoded := base64.StdEncoding.EncodeToString(sig)
	return os.WriteFile(signaturePath, []byte(encoded+"\n"), 0o644)
}

func decodePrivateKey(s string) (ed25519.PrivateKey, error) {
	if raw, err := hex.DecodeString(s); err == nil && len(raw) == ed25519.PrivateKeySize { return ed25519.PrivateKey(raw), nil }
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil && len(raw) == ed25519.PrivateKeySize { return ed25519.PrivateKey(raw), nil }
	return nil, errors.New("invalid Ed25519 private key; expected hex or base64")
}

func GenerateKeypair(publicPath, privatePath string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil { return err }
	if err := os.WriteFile(publicPath, []byte(hex.EncodeToString(pub)+"\n"), 0o644); err != nil { return err }
	return os.WriteFile(privatePath, []byte(hex.EncodeToString(priv)+"\n"), 0o600)
}

func MakePackageURL(dir, name string) string {
	return "file://" + filepath.ToSlash(filepath.Join(dir, name))
}

func EncodeArchitecture(a string) string {
	if a == "" { return "" }
	return strings.ToLower(strings.TrimSpace(a))
}

func ParseSize(s string) int64 {
	s = strings.TrimSpace(strings.ToLower(s))
	mult := int64(1)
	switch {
	case strings.HasSuffix(s,"kib"): mult=1024; s=strings.TrimSuffix(s,"kib")
	case strings.HasSuffix(s,"mib"): mult=1024*1024; s=strings.TrimSuffix(s,"mib")
	case strings.HasSuffix(s,"gib"): mult=1024*1024*1024; s=strings.TrimSuffix(s,"gib")
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(s),64)
	return int64(v*float64(mult))
}

func Manifest(root string) ([]model.FileEntry, error) { return filesystemManifest(root) }
