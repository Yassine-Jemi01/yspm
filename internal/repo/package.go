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
	pathpkg "path"
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

type boundedPackageTarReader struct {
	file     *os.File
	gzip     *gzip.Reader
	limited  *io.LimitedReader
	tar      *tar.Reader
	entries  int
	expanded int64
}

func openPackageTarReader(path string) (*boundedPackageTarReader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	gr, err := gzip.NewReader(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("open package gzip stream: %w", err)
	}
	limited := &io.LimitedReader{R: gr, N: maxTarStreamBytes + 1}
	return &boundedPackageTarReader{
		file: file, gzip: gr, limited: limited, tar: tar.NewReader(limited),
	}, nil
}

func (r *boundedPackageTarReader) Next() (*tar.Header, error) {
	header, err := r.tar.Next()
	if err == io.EOF {
		if r.limited.N <= 0 {
			return nil, fmt.Errorf("package expanded archive exceeds %d-byte limit", maxTarStreamBytes)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.entries++
	if r.entries > maxTarEntries {
		return nil, fmt.Errorf("package archive exceeds %d-entry limit", maxTarEntries)
	}
	if header.Size < 0 || header.Size > maxTarEntryBytes {
		return nil, fmt.Errorf("package entry %q has invalid or excessive size %d", header.Name, header.Size)
	}
	name, err := normalizeTarPath(header.Name)
	if err != nil {
		return nil, err
	}
	switch header.Typeflag {
	case tar.TypeReg, tar.TypeRegA:
		if r.expanded > maxTarExpandedBytes-header.Size {
			return nil, fmt.Errorf("package archive exceeds %d-byte expanded-size limit", maxTarExpandedBytes)
		}
		r.expanded += header.Size
	case tar.TypeDir:
		if header.Size != 0 {
			return nil, fmt.Errorf("package directory %q contains data", header.Name)
		}
	case tar.TypeSymlink:
		if err := safeSymlinkTarget(name, header.Linkname); err != nil {
			return nil, err
		}
	case tar.TypeLink:
		if _, err := normalizeTarPath(header.Linkname); err != nil {
			return nil, fmt.Errorf("unsafe package hardlink target %q: %w", header.Linkname, err)
		}
	default:
		return nil, fmt.Errorf("unsupported package archive entry type %q for %s", header.Typeflag, header.Name)
	}
	return header, nil
}

func (r *boundedPackageTarReader) Close() error {
	var closeErr error
	if err := r.gzip.Close(); err != nil {
		closeErr = fmt.Errorf("close package gzip stream: %w", err)
	}
	if err := r.file.Close(); err != nil {
		closeErr = errors.Join(closeErr, fmt.Errorf("close package archive: %w", err))
	}
	return closeErr
}

func ReadPackageMetadata(path string) (pkg model.Package, retErr error) {
	r, err := openPackageTarReader(path)
	if err != nil {
		return model.Package{}, err
	}
	defer func() { retErr = errors.Join(retErr, r.Close()) }()
	for {
		h, err := r.Next()
		if err != nil {
			return model.Package{}, err
		}
		if h == nil {
			break
		}
		if h.Name != "metadata.json" || h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(r.tar, (64<<20)+1))
		if err != nil {
			return model.Package{}, err
		}
		if len(data) > 64<<20 {
			return model.Package{}, errors.New("package metadata exceeds 64 MiB limit")
		}
		if err := json.Unmarshal(data, &pkg); err != nil {
			return model.Package{}, fmt.Errorf("invalid package metadata: %w", err)
		}
		return pkg, nil
	}
	return model.Package{}, errors.New("package does not contain metadata.json")
}

func ReadPackageData(path string) (out PackageData, retErr error) {
	r, err := openPackageTarReader(path)
	if err != nil {
		return PackageData{}, err
	}
	defer func() { retErr = errors.Join(retErr, r.Close()) }()
	out = PackageData{Scripts: map[string]string{}}
	metadataSeen := false
	for {
		h, err := r.Next()
		if err != nil {
			return PackageData{}, err
		}
		if h == nil {
			break
		}
		switch {
		case h.Name == "metadata.json" && (h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA):
			data, err := io.ReadAll(io.LimitReader(r.tar, (64<<20)+1))
			if err != nil {
				return PackageData{}, err
			}
			if len(data) > 64<<20 {
				return PackageData{}, errors.New("package metadata exceeds 64 MiB limit")
			}
			if err := json.Unmarshal(data, &out.Metadata); err != nil {
				return PackageData{}, fmt.Errorf("invalid package metadata: %w", err)
			}
			metadataSeen = true
		case strings.HasPrefix(h.Name, "scripts/") && (h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA):
			data, err := io.ReadAll(io.LimitReader(r.tar, (8<<20)+1))
			if err != nil {
				return PackageData{}, err
			}
			if len(data) > 8<<20 {
				return PackageData{}, fmt.Errorf("package script %q exceeds 8 MiB limit", h.Name)
			}
			out.Scripts[strings.TrimPrefix(h.Name, "scripts/")] = string(data)
		}
	}
	if !metadataSeen || out.Metadata.Name == "" {
		return PackageData{}, errors.New("package metadata missing name")
	}
	return out, nil
}

func ExtractPackage(path, destination string) error {
	// The compressed archive is first expanded into a bounded private file and
	// all headers/links are validated before any package-controlled file is made.
	archive, err := spoolTarStream(path, "tar.gz")
	if err != nil {
		return fmt.Errorf("prepare native package archive: %w", err)
	}
	defer func() {
		name := archive.Name()
		_ = archive.Close()
		_ = os.Remove(name)
	}()
	entries, err := validateTarArchive(archive)
	if err != nil {
		return fmt.Errorf("validate native package archive: %w", err)
	}

	var directories, hardlinks, symlinks []tarArchiveEntry
	for _, entry := range entries {
		switch {
		case entry.name == ".":
			if entry.typeflag != tar.TypeDir {
				return fmt.Errorf("invalid package root entry")
			}
		case entry.name == "metadata.json":
			if entry.typeflag != tar.TypeReg && entry.typeflag != tar.TypeRegA {
				return errors.New("metadata.json must be a regular file")
			}
		case entry.name == "scripts" || strings.HasPrefix(entry.name, "scripts/"):
			if entry.typeflag != tar.TypeDir && entry.typeflag != tar.TypeReg && entry.typeflag != tar.TypeRegA {
				return fmt.Errorf("invalid script entry %q", entry.name)
			}
		case entry.name == "root":
			if entry.typeflag != tar.TypeDir {
				return errors.New("package root must be a directory")
			}
		case strings.HasPrefix(entry.name, "root/"):
			rel := strings.TrimPrefix(entry.name, "root/")
			if rel == "" {
				return fmt.Errorf("invalid package path %q", entry.name)
			}
			if entry.typeflag == tar.TypeSymlink {
				if err := safeSymlinkTarget(rel, entry.linkname); err != nil {
					return fmt.Errorf("unsafe installed symlink %q: %w", rel, err)
				}
			}
			if entry.typeflag == tar.TypeLink {
				if !strings.HasPrefix(entry.linkname, "root/") {
					return fmt.Errorf("hardlink %q points outside package root", entry.name)
				}
				target := strings.TrimPrefix(entry.linkname, "root/")
				if _, err := normalizeTarPath(target); err != nil || target == "" || target == "." {
					return fmt.Errorf("unsafe installed hardlink target %q", entry.linkname)
				}
			}
		default:
			return fmt.Errorf("invalid package entry %q", entry.name)
		}
	}
	root, err := prepareTarDestination(destination)
	if err != nil {
		return err
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	reader := tar.NewReader(archive)
	index := 0
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if index >= len(entries) {
			return errors.New("package archive changed between validation and extraction")
		}
		entry := entries[index]
		index++
		if entry.name == "." || entry.name == "metadata.json" || entry.name == "scripts" || strings.HasPrefix(entry.name, "scripts/") || entry.name == "root" {
			continue
		}
		if !strings.HasPrefix(entry.name, "root/") {
			return fmt.Errorf("invalid package entry %q", entry.name)
		}
		rel := strings.TrimPrefix(entry.name, "root/")
		if entry.typeflag == tar.TypeLink {
			hardlinks = append(hardlinks, entry)
			continue
		}
		if entry.typeflag == tar.TypeSymlink {
			symlinks = append(symlinks, entry)
			continue
		}
		if entry.typeflag == tar.TypeDir {
			if _, err := ensureTarDirectories(root, rel); err != nil {
				return err
			}
			directories = append(directories, entry)
			continue
		}
		if entry.typeflag != tar.TypeReg && entry.typeflag != tar.TypeRegA {
			return fmt.Errorf("unsupported package entry type %q", header.Typeflag)
		}
		parent, err := ensureTarDirectories(root, pathpkg.Dir(rel))
		if err != nil {
			return err
		}
		target := filepath.Join(parent, filepath.Base(filepath.FromSlash(rel)))
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("create package file %q: %w", rel, err)
		}
		_, copyErr := io.CopyN(out, reader, entry.size)
		closeErr := out.Close()
		if copyErr != nil {
			_ = os.Remove(target)
			return fmt.Errorf("extract package file %q: %w", rel, copyErr)
		}
		if closeErr != nil {
			_ = os.Remove(target)
			return closeErr
		}
		if err := os.Chmod(target, os.FileMode(entry.mode)&0o777); err != nil {
			return err
		}
	}
	if index != len(entries) {
		return errors.New("package archive entry count changed between validation and extraction")
	}

	// Links are materialized last, after all regular files exist.
	for _, entry := range hardlinks {
		rel := strings.TrimPrefix(entry.name, "root/")
		targetRel := strings.TrimPrefix(entry.linkname, "root/")
		parent, err := ensureTarDirectories(root, pathpkg.Dir(rel))
		if err != nil {
			return err
		}
		sourceParent, err := ensureTarDirectories(root, pathpkg.Dir(targetRel))
		if err != nil {
			return err
		}
		target := filepath.Join(parent, filepath.Base(filepath.FromSlash(rel)))
		source := filepath.Join(sourceParent, filepath.Base(filepath.FromSlash(targetRel)))
		info, err := os.Lstat(source)
		if err != nil {
			return fmt.Errorf("hardlink source %q: %w", targetRel, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("hardlink source %q is not a regular file", targetRel)
		}
		if err := os.Link(source, target); err != nil {
			return err
		}
	}
	for _, entry := range symlinks {
		rel := strings.TrimPrefix(entry.name, "root/")
		parent, err := ensureTarDirectories(root, pathpkg.Dir(rel))
		if err != nil {
			return err
		}
		target := filepath.Join(parent, filepath.Base(filepath.FromSlash(rel)))
		if err := os.Symlink(entry.linkname, target); err != nil {
			return err
		}
	}
	sort.Slice(directories, func(i, j int) bool {
		return strings.Count(directories[i].name, "/") > strings.Count(directories[j].name, "/")
	})
	for _, entry := range directories {
		rel := strings.TrimPrefix(entry.name, "root/")
		if err := os.Chmod(filepath.Join(root, filepath.FromSlash(rel)), os.FileMode(entry.mode)&0o777); err != nil {
			return err
		}
	}
	return nil
}

func ListPackageFiles(path string) (out []model.FileEntry, retErr error) {
	r, err := openPackageTarReader(path)
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, r.Close()) }()
	for {
		h, err := r.Next()
		if err != nil {
			return nil, err
		}
		if h == nil {
			break
		}
		if !strings.HasPrefix(h.Name, "root/") || h.Name == "root/" {
			continue
		}
		rel := filepath.ToSlash(strings.TrimPrefix(h.Name, "root/"))
		entry := model.FileEntry{Path: rel, Mode: uint32(h.Mode)}
		switch h.Typeflag {
		case tar.TypeDir:
			entry.Type = "dir"
		case tar.TypeSymlink:
			if err := safeSymlinkTarget(rel, h.Linkname); err != nil {
				return nil, fmt.Errorf("unsafe package symlink %q: %w", rel, err)
			}
			entry.Type, entry.LinkTarget = "symlink", h.Linkname
		case tar.TypeReg, tar.TypeRegA:
			hash := sha256.New()
			if _, err := io.CopyN(hash, r.tar, h.Size); err != nil {
				return nil, err
			}
			entry.Type = "file"
			entry.SHA256 = hex.EncodeToString(hash.Sum(nil))
		default:
			return nil, fmt.Errorf("unsupported package archive entry type %q", h.Typeflag)
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
		for _, line := range strings.Split(string(out), "\n") {
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
	idx := model.Index{Repository: baseURL, Release: release, Channel: "stable", Generated: time.Now().UTC().Format(time.RFC3339), ABI: abi}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil { return err }
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".yspkg") { return nil }
		p, err := ReadPackageMetadata(path)
		if err != nil { return err }
		sum, err := PackageSHA256(path)
		if err != nil { return err }
		rel, err := filepath.Rel(dir, path)
		if err != nil { return err }
		relURL := strings.ReplaceAll(filepath.ToSlash(rel), " ", "%20")
		p.URL = strings.TrimRight(baseURL, "/") + "/" + relURL
		st, err := os.Stat(path)
		if err != nil { return err }
		p.SHA256, p.Size = sum, st.Size()
		idx.Packages = append(idx.Packages, p)
		return nil
	})
	if err != nil { return model.Index{}, err }
	sort.Slice(idx.Packages, func(i,j int) bool {
		if idx.Packages[i].Name==idx.Packages[j].Name { return idx.Packages[i].Architecture < idx.Packages[j].Architecture }
		return idx.Packages[i].Name < idx.Packages[j].Name
	})
	if err := ValidateStableIndex(idx); err != nil {
		return model.Index{}, fmt.Errorf("refusing to write invalid repository index: %w", err)
	}
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
