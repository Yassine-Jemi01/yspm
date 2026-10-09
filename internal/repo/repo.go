package repo

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Yassine-Jemi01/yspm/internal/config"
	"github.com/Yassine-Jemi01/yspm/internal/model"
)

const (
	maxRepositorySourceBytes int64 = 16 << 20
	maxDownloadBytes         int64 = 4 << 30
	maxZipEntries                  = 100_000
	maxZipEntryBytes         uint64 = 4 << 30
	maxZipExpandedBytes      uint64 = 8 << 30
)

type indexCacheEnvelope struct {
	CacheVersion int    `json:"cache_version"`
	Source       string `json:"source"`
	IndexData    []byte `json:"index_data"`
	Signature    []byte `json:"signature,omitempty"`
}

func indexCachePathFor(user bool) (string, error) {
	d, err := config.CacheDirFor(user)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "index.json"), nil
}

func readBounded(r io.Reader, limit int64, what string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds the %d-byte size limit", what, limit)
	}
	return data, nil
}

func readSource(source string) ([]byte, error) {
	if u, err := url.Parse(source); err == nil && u.Scheme != "" && u.Scheme != "file" {
		resp, err := (&http.Client{Timeout: 45 * time.Second}).Get(source)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("repository returned HTTP %s", resp.Status)
		}
		return readBounded(resp.Body, maxRepositorySourceBytes, "repository source")
	}
	if u, err := url.Parse(source); err == nil && u.Scheme == "file" {
		source = u.Path
	}
	f, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readBounded(f, maxRepositorySourceBytes, "repository source")
}

// FetchIndexWithProof returns the exact index bytes and detached signature that
// were verified. The proof is retained in cache so signature-required clients
// can verify cached metadata offline instead of trusting a re-serialized JSON
// object or bypassing signature verification on cache hits.
func FetchIndexWithProof(source string) (model.Index, []byte, []byte, error) {
	data, err := readSource(source)
	if err != nil {
		return model.Index{}, nil, nil, fmt.Errorf("fetch repository index: %w", err)
	}
	var idx model.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return model.Index{}, nil, nil, fmt.Errorf("invalid repository index: %w", err)
	}
	if err := ValidateStableIndex(idx); err != nil {
		return model.Index{}, nil, nil, err
	}
	var signature []byte
	if config.RequireRepositorySignature() {
		signature, err = fetchRepositorySignature()
		if err != nil {
			return model.Index{}, nil, nil, err
		}
		if err := verifyIndexSignatureBytes(data, signature); err != nil {
			return model.Index{}, nil, nil, err
		}
	}
	return idx, data, signature, nil
}

func FetchIndex(source string) (model.Index, error) {
	idx, _, _, err := FetchIndexWithProof(source)
	return idx, err
}

func decodePublicKey() (ed25519.PublicKey, error) {
	keyText := strings.TrimSpace(config.RepositoryPublicKey())
	if keyText == "" {
		return nil, errors.New("repository signatures are required but no public key is configured")
	}
	keyRaw, err := hex.DecodeString(keyText)
	if err != nil {
		keyRaw, err = base64.StdEncoding.DecodeString(keyText)
	}
	if err != nil || len(keyRaw) != ed25519.PublicKeySize {
		return nil, errors.New("invalid Ed25519 repository public key")
	}
	return ed25519.PublicKey(keyRaw), nil
}

func fetchRepositorySignature() ([]byte, error) {
	sigURL := strings.TrimSpace(config.RepositorySignatureURL())
	if sigURL == "" {
		return nil, errors.New("repository signatures are required but signature URL is not configured")
	}
	raw, err := readSource(sigURL)
	if err != nil {
		return nil, fmt.Errorf("fetch repository signature: %w", err)
	}
	text := strings.TrimSpace(string(raw))
	signature, err := hex.DecodeString(text)
	if err != nil {
		signature, err = base64.StdEncoding.DecodeString(text)
	}
	if err != nil || len(signature) != ed25519.SignatureSize {
		return nil, errors.New("invalid repository signature")
	}
	return signature, nil
}

func verifyIndexSignatureBytes(data, signature []byte) error {
	key, err := decodePublicKey()
	if err != nil {
		return err
	}
	if len(signature) != ed25519.SignatureSize {
		return errors.New("invalid repository signature")
	}
	if !ed25519.Verify(key, data, signature) {
		return errors.New("repository signature verification failed")
	}
	return nil
}

func verifyIndexSignature(data []byte) error {
	signature, err := fetchRepositorySignature()
	if err != nil {
		return err
	}
	return verifyIndexSignatureBytes(data, signature)
}

func writeIndexCache(user bool, envelope indexCacheEnvelope) error {
	path, err := indexCachePathFor(user)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".index-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func CacheIndexProofFor(user bool, source string, indexData, signature []byte) error {
	var idx model.Index
	if err := json.Unmarshal(indexData, &idx); err != nil {
		return fmt.Errorf("refusing to cache invalid index JSON: %w", err)
	}
	if err := ValidateStableIndex(idx); err != nil {
		return fmt.Errorf("refusing to cache invalid repository index: %w", err)
	}
	if config.RequireRepositorySignature() {
		if err := verifyIndexSignatureBytes(indexData, signature); err != nil {
			return fmt.Errorf("refusing to cache unverified repository index: %w", err)
		}
	}
	return writeIndexCache(user, indexCacheEnvelope{
		CacheVersion: 1,
		Source:       source,
		IndexData:    append([]byte(nil), indexData...),
		Signature:    append([]byte(nil), signature...),
	})
}

func CacheIndexFor(user bool, source string, idx model.Index) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return CacheIndexProofFor(user, source, append(data, '\n'), nil)
}

// CacheIndex is retained for internal callers that use the conventional system
// cache. Signed callers should use CacheIndexProofFor to preserve original bytes.
func CacheIndex(idx model.Index) error {
	return CacheIndexFor(os.Geteuid() != 0, "", idx)
}

func LoadCachedIndexFor(user bool, source string) (model.Index, error) {
	path, err := indexCachePathFor(user)
	if err != nil {
		return model.Index{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Index{}, err
	}

	var envelope indexCacheEnvelope
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.CacheVersion == 1 && len(envelope.IndexData) != 0 {
		if envelope.Source != source {
			return model.Index{}, fmt.Errorf("cached repository index source mismatch: cached %q, requested %q", envelope.Source, source)
		}
		if config.RequireRepositorySignature() {
			if err := verifyIndexSignatureBytes(envelope.IndexData, envelope.Signature); err != nil {
				return model.Index{}, fmt.Errorf("cached repository signature verification failed: %w", err)
			}
		}
		var idx model.Index
		if err := json.Unmarshal(envelope.IndexData, &idx); err != nil {
			return model.Index{}, fmt.Errorf("invalid cached repository index: %w", err)
		}
		if err := ValidateStableIndex(idx); err != nil {
			return model.Index{}, fmt.Errorf("invalid cached repository index: %w", err)
		}
		return idx, nil
	}

	// Legacy caches had no retained signature proof. They are not trusted when
	// signatures are required; the caller will fetch a fresh verified index.
	if config.RequireRepositorySignature() {
		return model.Index{}, errors.New("cached repository index has no verifiable signature proof")
	}
	var idx model.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return model.Index{}, fmt.Errorf("invalid cached repository index: %w", err)
	}
	if err := ValidateStableIndex(idx); err != nil {
		return model.Index{}, fmt.Errorf("invalid cached repository index: %w", err)
	}
	return idx, nil
}

func LoadCachedIndex() (model.Index, error) {
	return LoadCachedIndexFor(os.Geteuid() != 0, "")
}

func ValidateInstallPackage(p model.Package) error {
	if p.Kind == "meta" {
		return nil
	}
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("package is missing a name")
	}
	if strings.TrimSpace(p.ABI) == "" {
		return fmt.Errorf("package %q has no ABI identifier", p.Name)
	}
	checksum := strings.TrimSpace(p.SHA256)
	if checksum == "" {
		return fmt.Errorf("package %q has no SHA-256 checksum", p.Name)
	}
	if len(checksum) != sha256.Size*2 {
		return fmt.Errorf("package %q has an invalid SHA-256 checksum: expected %d hexadecimal characters", p.Name, sha256.Size*2)
	}
	if _, err := hex.DecodeString(checksum); err != nil {
		return fmt.Errorf("package %q has an invalid SHA-256 checksum: %w", p.Name, err)
	}
	rawURL := strings.TrimSpace(p.URL)
	if rawURL == "" {
		return fmt.Errorf("package %q has no download URL", p.Name)
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" {
		return fmt.Errorf("package %q has an invalid download URL", p.Name)
	}
	switch u.Scheme {
	case "https", "http":
		if u.Host == "" {
			return fmt.Errorf("package %q has a download URL without a host", p.Name)
		}
	case "file":
		if u.Path == "" {
			return fmt.Errorf("package %q has an empty local file URL", p.Name)
		}
	default:
		return fmt.Errorf("package %q uses unsupported download URL scheme %q", p.Name, u.Scheme)
	}
	return nil
}

func VerifySHA256(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, strings.TrimSpace(expected)) {
		return fmt.Errorf("SHA-256 mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

func Download(source, destination string) error {
	return DownloadWithLimit(source, destination, maxDownloadBytes)
}

// DownloadPackage enforces the global maximum and, if provided, the exact
// repository-declared archive size.
func DownloadPackage(source, destination string, expectedSize int64) error {
	limit := maxDownloadBytes
	if expectedSize > 0 {
		if expectedSize > maxDownloadBytes {
			return fmt.Errorf("package download size %d exceeds the %d-byte policy limit", expectedSize, maxDownloadBytes)
		}
		limit = expectedSize
	}
	if err := DownloadWithLimit(source, destination, limit); err != nil {
		return err
	}
	if expectedSize > 0 {
		info, err := os.Stat(destination)
		if err != nil {
			_ = os.Remove(destination)
			return err
		}
		if info.Size() != expectedSize {
			_ = os.Remove(destination)
			return fmt.Errorf("download size mismatch: expected %d bytes, got %d", expectedSize, info.Size())
		}
	}
	return nil
}

func DownloadWithLimit(source, destination string, maxBytes int64) error {
	if maxBytes <= 0 || maxBytes > maxDownloadBytes {
		maxBytes = maxDownloadBytes
	}
	if u, err := url.Parse(source); err == nil && u.Scheme != "" && u.Scheme != "file" {
		resp, err := (&http.Client{Timeout: 10 * time.Minute}).Get(source)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("download failed: HTTP %s", resp.Status)
		}
		if resp.ContentLength > maxBytes {
			return fmt.Errorf("download content length %d exceeds the %d-byte limit", resp.ContentLength, maxBytes)
		}
		return writeAtomicLimit(resp.Body, destination, maxBytes)
	}
	if u, err := url.Parse(source); err == nil && u.Scheme == "file" {
		source = u.Path
	}
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeAtomicLimit(f, destination, maxBytes)
}

func writeAtomicLimit(r io.Reader, destination string, maxBytes int64) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".download-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	n, err := io.Copy(tmp, io.LimitReader(r, maxBytes+1))
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if n > maxBytes {
		_ = tmp.Close()
		return fmt.Errorf("download exceeds the %d-byte limit", maxBytes)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, destination)
}


const (
	maxTarEntries       = 100_000
	maxTarEntryBytes    int64 = 4 << 30
	maxTarExpandedBytes int64 = 8 << 30
	maxTarStreamBytes         = maxTarExpandedBytes + (128 << 20)
)

type tarArchiveEntry struct {
	name     string
	typeflag byte
	linkname string
	size     int64
	mode     int64
}

type tarReadCloser struct {
	io.Reader
	closers []io.Closer
}

func (r *tarReadCloser) Close() error {
	var firstErr error
	for _, c := range r.closers {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

type commandTarReadCloser struct {
	io.ReadCloser
	cmd   *exec.Cmd
	input *os.File
}

func (r *commandTarReadCloser) Close() error {
	pipeErr := r.ReadCloser.Close()
	waitErr := r.cmd.Wait()
	inputErr := r.input.Close()
	if waitErr != nil {
		return fmt.Errorf("%s decompressor failed: %w", filepath.Base(r.cmd.Path), waitErr)
	}
	if pipeErr != nil {
		return pipeErr
	}
	return inputErr
}

func ExtractArchive(archivePath, format, destination string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "zip":
		if err := os.MkdirAll(destination, 0o755); err != nil {
			return err
		}
		return extractZip(archivePath, destination)
	case "tar.gz", "tgz", "tar.xz", "tar.bz2", "tar.zst", "tar":
		return extractTar(archivePath, format, destination)
	default:
		return fmt.Errorf("unsupported archive format %q", format)
	}
}

func openTarStream(archivePath, format string) (io.ReadCloser, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}

	format = strings.ToLower(strings.TrimSpace(format))
	compression := ""
	switch format {
	case "tar.gz", "tgz":
		compression = "gzip"
	case "tar.xz":
		compression = "xz"
	case "tar.bz2":
		compression = "bzip2"
	case "tar.zst":
		compression = "zstd"
	case "tar":
		var magic [6]byte
		_, _ = file.ReadAt(magic[:], 0)
		switch {
		case magic[0] == 0x1f && magic[1] == 0x8b:
			compression = "gzip"
		case string(magic[:3]) == "BZh":
			compression = "bzip2"
		case string(magic[:6]) == "\xfd7zXZ\x00":
			compression = "xz"
		case string(magic[:4]) == "\x28\xb5\x2f\xfd":
			compression = "zstd"
		}
	default:
		_ = file.Close()
		return nil, fmt.Errorf("unsupported tar compression format %q", format)
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, err
	}

	switch compression {
	case "":
		return file, nil
	case "gzip":
		reader, err := gzip.NewReader(file)
		if err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("open gzip archive: %w", err)
		}
		return &tarReadCloser{Reader: reader, closers: []io.Closer{reader, file}}, nil
	case "bzip2":
		return &tarReadCloser{Reader: bzip2.NewReader(file), closers: []io.Closer{file}}, nil
	case "xz", "zstd":
		program := compressionProgram(compression)
		cmd := exec.Command(program, "-dc")
		cmd.Stdin = file
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			_ = stdout.Close()
			_ = file.Close()
			return nil, fmt.Errorf("start %s decompressor: %w", program, err)
		}
		return &commandTarReadCloser{ReadCloser: stdout, cmd: cmd, input: file}, nil
	default:
		_ = file.Close()
		return nil, fmt.Errorf("unsupported tar compression %q", compression)
	}
}

func compressionProgram(compression string) string {
	if compression == "xz" {
		return "xz"
	}
	return "zstd"
}

// spoolTarStream decompresses to a private temporary file before validation and
// extraction. Both passes therefore inspect the exact same bytes, preventing a
// source archive from being swapped between the validation and extraction pass.
func spoolTarStream(archivePath, format string) (*os.File, error) {
	source, err := openTarStream(archivePath, format)
	if err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp("", "yspm-validated-tar-*.tar")
	if err != nil {
		_ = source.Close()
		return nil, err
	}
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}

	n, copyErr := io.Copy(tmp, io.LimitReader(source, maxTarStreamBytes+1))
	closeErr := source.Close()
	if n > maxTarStreamBytes {
		cleanup()
		return nil, fmt.Errorf("decompressed tar stream exceeds the %d-byte limit", maxTarStreamBytes)
	}
	if copyErr != nil {
		cleanup()
		if closeErr != nil {
			return nil, fmt.Errorf("read tar stream: %v; close decompressor: %w", copyErr, closeErr)
		}
		return nil, fmt.Errorf("read tar stream: %w", copyErr)
	}
	if closeErr != nil {
		cleanup()
		return nil, closeErr
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, err
	}
	return tmp, nil
}

func normalizeTarPath(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') {
		return "", fmt.Errorf("unsafe empty or NUL-containing archive path %q", name)
	}
	// Backslashes and drive-qualified names are rejected so the same archive
	// cannot become an absolute/traversal path on another supported host OS.
	if strings.Contains(name, "\\") || isDriveQualifiedPath(name) || pathpkg.IsAbs(name) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	clean := pathpkg.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") || pathpkg.IsAbs(clean) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return clean, nil
}

func isDriveQualifiedPath(name string) bool {
	return len(name) >= 2 &&
		((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z')) &&
		name[1] == ':'
}

func safeSymlinkTarget(name, target string) error {
	if target == "" || strings.ContainsRune(target, '\x00') ||
		strings.Contains(target, "\\") || isDriveQualifiedPath(target) || pathpkg.IsAbs(target) {
		return fmt.Errorf("unsafe symlink target %q for %q", target, name)
	}
	resolved := pathpkg.Clean(pathpkg.Join(pathpkg.Dir(name), target))
	if resolved == ".." || strings.HasPrefix(resolved, "../") || pathpkg.IsAbs(resolved) {
		return fmt.Errorf("symlink %q escapes extraction root through target %q", name, target)
	}
	return nil
}

func validateTarArchive(file *os.File) ([]tarArchiveEntry, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	reader := tar.NewReader(file)
	entries := make([]tarArchiveEntry, 0, 128)
	byName := make(map[string]tarArchiveEntry)
	var expandedBytes int64

	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar headers: %w", err)
		}
		if len(entries) >= maxTarEntries {
			return nil, fmt.Errorf("tar archive exceeds the %d-entry limit", maxTarEntries)
		}
		if header.Size < 0 {
			return nil, fmt.Errorf("negative size for archive entry %q", header.Name)
		}

		name, err := normalizeTarPath(header.Name)
		if err != nil {
			return nil, err
		}
		entry := tarArchiveEntry{
			name:     name,
			typeflag: header.Typeflag,
			linkname: header.Linkname,
			size:     header.Size,
			mode:     header.Mode,
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if header.Size != 0 {
				return nil, fmt.Errorf("directory entry %q has unexpected data", header.Name)
			}
		case tar.TypeReg, tar.TypeRegA:
			if name == "." {
				return nil, fmt.Errorf("regular file entry cannot name extraction root")
			}
			if header.Size > maxTarEntryBytes {
				return nil, fmt.Errorf("archive entry %q exceeds the %d-byte per-file limit", name, maxTarEntryBytes)
			}
			if expandedBytes > maxTarExpandedBytes-header.Size {
				return nil, fmt.Errorf("tar archive exceeds the %d-byte expanded-size limit", maxTarExpandedBytes)
			}
			expandedBytes += header.Size
		case tar.TypeSymlink:
			if name == "." || header.Size != 0 {
				return nil, fmt.Errorf("invalid symlink entry %q", header.Name)
			}
			if err := safeSymlinkTarget(name, header.Linkname); err != nil {
				return nil, err
			}
		case tar.TypeLink:
			if name == "." || header.Size != 0 {
				return nil, fmt.Errorf("invalid hardlink entry %q", header.Name)
			}
			linkTarget, err := normalizeTarPath(header.Linkname)
			if err != nil || linkTarget == "." {
				return nil, fmt.Errorf("unsafe hardlink target %q for %q", header.Linkname, name)
			}
			entry.linkname = linkTarget
		default:
			return nil, fmt.Errorf("unsupported tar entry type %q for %s", header.Typeflag, header.Name)
		}

		if name == "." && header.Typeflag != tar.TypeDir {
			return nil, fmt.Errorf("only a directory may name the extraction root")
		}
		if _, duplicate := byName[name]; duplicate {
			return nil, fmt.Errorf("duplicate archive path %q", name)
		}
		byName[name] = entry
		entries = append(entries, entry)
	}

	for _, entry := range entries {
		if entry.name == "." {
			continue
		}
		for parent := pathpkg.Dir(entry.name); parent != "." && parent != "/"; parent = pathpkg.Dir(parent) {
			if parentEntry, ok := byName[parent]; ok && parentEntry.typeflag != tar.TypeDir {
				return nil, fmt.Errorf("archive path %q is nested beneath non-directory entry %q", entry.name, parent)
			}
		}
		if entry.typeflag == tar.TypeLink {
			target, ok := byName[entry.linkname]
			if !ok || (target.typeflag != tar.TypeReg && target.typeflag != tar.TypeRegA) {
				return nil, fmt.Errorf("hardlink %q must target a regular file in the same archive", entry.name)
			}
		}
	}
	return entries, nil
}

func prepareTarDestination(destination string) (string, error) {
	root, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return "", err
		}
		info, err = os.Lstat(root)
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("tar extraction destination must be a real directory, not a symlink: %s", root)
	}
	return root, nil
}

// ensureTarDirectories creates every component below root without following
// symlinks. This is checked again at write time in addition to archive-wide
// validation, so pre-existing symlinks in a destination cannot redirect writes.
func ensureTarDirectories(root, relative string) (string, error) {
	if relative == "" || relative == "." {
		return root, nil
	}
	clean, err := normalizeTarPath(relative)
	if err != nil {
		return "", err
	}
	current := root
	for _, component := range strings.Split(clean, "/") {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if mkdirErr := os.Mkdir(current, 0o755); mkdirErr != nil && !os.IsExist(mkdirErr) {
				return "", mkdirErr
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("unsafe non-directory path component %q in extraction destination", current)
		}
	}
	return current, nil
}

func extractTar(archivePath, format, destination string) error {
	// Decompress once into a bounded private file, then validate all members
	// before creating any archive-controlled filesystem entries.
	archive, err := spoolTarStream(archivePath, format)
	if err != nil {
		return fmt.Errorf("prepare tar archive: %w", err)
	}
	defer func() {
		name := archive.Name()
		_ = archive.Close()
		_ = os.Remove(name)
	}()

	entries, err := validateTarArchive(archive)
	if err != nil {
		return err
	}
	root, err := prepareTarDestination(destination)
	if err != nil {
		return err
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	reader := tar.NewReader(archive)
	entryIndex := 0
	var directoryEntries []tarArchiveEntry
	var hardlinks []tarArchiveEntry
	var symlinks []tarArchiveEntry

	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar during extraction: %w", err)
		}
		if entryIndex >= len(entries) {
			return fmt.Errorf("tar archive changed between validation and extraction")
		}
		entry := entries[entryIndex]
		entryIndex++
		if entry.name == "." {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if _, err := ensureTarDirectories(root, entry.name); err != nil {
				return err
			}
			directoryEntries = append(directoryEntries, entry)
		case tar.TypeReg, tar.TypeRegA:
			parent, err := ensureTarDirectories(root, pathpkg.Dir(entry.name))
			if err != nil {
				return err
			}
			target := filepath.Join(parent, filepath.Base(filepath.FromSlash(entry.name)))
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return fmt.Errorf("create archive file %q: %w", entry.name, err)
			}
			_, copyErr := io.CopyN(out, reader, entry.size)
			closeErr := out.Close()
			if copyErr != nil {
				_ = os.Remove(target)
				return fmt.Errorf("extract archive file %q: %w", entry.name, copyErr)
			}
			if closeErr != nil {
				_ = os.Remove(target)
				return closeErr
			}
			if err := os.Chmod(target, os.FileMode(entry.mode)&0o777); err != nil {
				return err
			}
		case tar.TypeLink:
			hardlinks = append(hardlinks, entry)
		case tar.TypeSymlink:
			symlinks = append(symlinks, entry)
		default:
			return fmt.Errorf("unsupported tar entry type %q for %s", header.Typeflag, header.Name)
		}
	}
	if entryIndex != len(entries) {
		return fmt.Errorf("tar archive entry count changed between validation and extraction")
	}

	// Link creation is deferred until all regular files exist. No archive member
	// may be nested under a symlink, as enforced by validateTarArchive.
	for _, entry := range hardlinks {
		parent, err := ensureTarDirectories(root, pathpkg.Dir(entry.name))
		if err != nil {
			return err
		}
		target := filepath.Join(parent, filepath.Base(filepath.FromSlash(entry.name)))
		sourceParent, err := ensureTarDirectories(root, pathpkg.Dir(entry.linkname))
		if err != nil {
			return err
		}
		source := filepath.Join(sourceParent, filepath.Base(filepath.FromSlash(entry.linkname)))
		info, err := os.Lstat(source)
		if err != nil {
			return fmt.Errorf("hardlink source %q: %w", entry.linkname, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("hardlink source %q is not a regular file", entry.linkname)
		}
		if err := os.Link(source, target); err != nil {
			return fmt.Errorf("create hardlink %q: %w", entry.name, err)
		}
	}
	for _, entry := range symlinks {
		parent, err := ensureTarDirectories(root, pathpkg.Dir(entry.name))
		if err != nil {
			return err
		}
		target := filepath.Join(parent, filepath.Base(filepath.FromSlash(entry.name)))
		if err := os.Symlink(entry.linkname, target); err != nil {
			return fmt.Errorf("create symlink %q: %w", entry.name, err)
		}
	}

	// Set archived directory modes only after all writes have completed, so
	// read-only modes cannot block extraction of their children.
	sort.Slice(directoryEntries, func(i, j int) bool {
		return strings.Count(directoryEntries[i].name, "/") > strings.Count(directoryEntries[j].name, "/")
	})
	for _, entry := range directoryEntries {
		target := filepath.Join(root, filepath.FromSlash(entry.name))
		if err := os.Chmod(target, os.FileMode(entry.mode)&0o777); err != nil {
			return err
		}
	}
	return nil
}

func extractZip(archivePath, destination string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()

	type zipEntry struct {
		file *zip.File
		name string
	}
	entries := make([]zipEntry, 0, len(r.File))
	seen := make(map[string]bool, len(r.File))
	var expanded uint64
	if len(r.File) > maxZipEntries {
		return fmt.Errorf("ZIP archive exceeds the %d-entry limit", maxZipEntries)
	}
	for _, f := range r.File {
		name := strings.TrimSuffix(f.Name, "/")
		if name == "" {
			continue
		}
		clean, err := normalizeTarPath(name)
		if err != nil || clean == "." {
			return fmt.Errorf("unsafe ZIP archive path %q", f.Name)
		}
		if seen[clean] {
			return fmt.Errorf("duplicate ZIP archive path %q", clean)
		}
		seen[clean] = true
		isDir := f.FileInfo().IsDir()
		mode := f.Mode()
		if !isDir && mode.Type() != 0 && !mode.IsRegular() {
			return fmt.Errorf("unsupported ZIP entry type for %q", f.Name)
		}
		if !isDir {
			if f.UncompressedSize64 > maxZipEntryBytes {
				return fmt.Errorf("ZIP entry %q exceeds the %d-byte per-file limit", clean, maxZipEntryBytes)
			}
			if expanded > maxZipExpandedBytes-f.UncompressedSize64 {
				return fmt.Errorf("ZIP archive exceeds the %d-byte expanded-size limit", maxZipExpandedBytes)
			}
			expanded += f.UncompressedSize64
		}
		entries = append(entries, zipEntry{file: f, name: clean})
	}
	root, err := prepareTarDestination(destination)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		f, name := entry.file, entry.name
		if f.FileInfo().IsDir() {
			if _, err := ensureTarDirectories(root, name); err != nil {
				return err
			}
			continue
		}
		parent, err := ensureTarDirectories(root, pathpkg.Dir(name))
		if err != nil {
			return err
		}
		target := filepath.Join(parent, filepath.Base(filepath.FromSlash(name)))
		in, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = in.Close()
			return fmt.Errorf("create ZIP entry %q: %w", name, err)
		}
		n, copyErr := io.Copy(out, io.LimitReader(in, int64(f.UncompressedSize64)+1))
		closeOutErr := out.Close()
		closeInErr := in.Close()
		if copyErr != nil {
			_ = os.Remove(target)
			return copyErr
		}
		if uint64(n) != f.UncompressedSize64 {
			_ = os.Remove(target)
			return fmt.Errorf("ZIP entry %q expanded to %d bytes; header declared %d", name, n, f.UncompressedSize64)
		}
		if closeOutErr != nil {
			return closeOutErr
		}
		if closeInErr != nil {
			return closeInErr
		}
		mode := f.Mode().Perm() & 0o777
		if mode == 0 {
			mode = 0o644
		}
		if err := os.Chmod(target, mode); err != nil {
			return err
		}
	}
	return nil
}
func FindEntry(root, entry string) (string, error) {
	if entry == "" {
		return "", errors.New("package has no entry")
	}
	candidate := filepath.Join(root, filepath.FromSlash(entry))
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate, nil
	}
	base := filepath.Base(entry)
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return err
		}
		if info.Name() == base {
			if found != "" {
				return errors.New("multiple matching entries")
			}
			found = path
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("entry %q not found", entry)
	}
	return found, nil
}

func FileList(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}
