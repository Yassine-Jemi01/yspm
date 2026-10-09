package repo

import (
	"archive/tar"
	"archive/zip"
	"bytes"
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

type indexCacheEnvelope struct {
	FormatVersion int    `json:"format_version"`
	RawIndex      []byte `json:"raw_index"`
	Signature     []byte `json:"signature,omitempty"`
}

func indexCachePathFor(user bool) (string, error) {
	d, err := config.CacheDirFor(user)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "index.json"), nil
}

const (
	maxRepositoryMetadataBytes int64 = 64 << 20
	maxDetachedSignatureBytes  int64 = 4 << 10
	maxDownloadBytes           int64 = 4 << 30
	maxZipEntries                    = 100_000
	maxZipEntryBytes           uint64 = 4 << 30
	maxZipExpandedBytes        uint64 = 8 << 30
)

func readSource(source string) ([]byte, error) {
	return readSourceLimit(source, maxRepositoryMetadataBytes)
}

func readSourceLimit(source string, limit int64) ([]byte, error) {
	if limit <= 0 {
		return nil, errors.New("invalid source size limit")
	}
	if u, err := url.Parse(source); err == nil && u.Scheme != "" && u.Scheme != "file" {
		client := &http.Client{Timeout: 45 * time.Second}
		resp, err := client.Get(source)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("source returned HTTP %s", resp.Status)
		}
		if resp.ContentLength > limit {
			return nil, fmt.Errorf("source size %d exceeds %d-byte limit", resp.ContentLength, limit)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > limit {
			return nil, fmt.Errorf("source exceeds %d-byte limit", limit)
		}
		return data, nil
	}
	if u, err := url.Parse(source); err == nil && u.Scheme == "file" {
		source = u.Path
	}
	file, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil {
		return nil, err
	} else if info.Size() > limit {
		return nil, fmt.Errorf("source size %d exceeds %d-byte limit", info.Size(), limit)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("source exceeds %d-byte limit", limit)
	}
	return data, nil
}

func FetchIndex(source string) (model.Index, error) {
	idx, _, _, err := FetchIndexData(source)
	return idx, err
}

// FetchIndexData returns the parsed index, the exact bytes that were signed by
// the repository, and the decoded detached signature. Keeping the original bytes
// is essential: re-marshalling JSON changes the signed message.
func FetchIndexData(source string) (model.Index, []byte, []byte, error) {
	data, err := readSource(source)
	if err != nil {
		return model.Index{}, nil, nil, fmt.Errorf("fetch repository index: %w", err)
	}
	var idx model.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return model.Index{}, nil, nil, fmt.Errorf("invalid repository index: %w", err)
	}
	if idx.Release == "" || idx.Channel == "" {
		return model.Index{}, nil, nil, errors.New("repository index is missing release/channel")
	}
	if err := ValidateStableIndex(idx); err != nil {
		return model.Index{}, nil, nil, err
	}
	var signature []byte
	if config.RequireRepositorySignature() {
		signature, err = fetchAndVerifyIndexSignature(data)
		if err != nil {
			return model.Index{}, nil, nil, err
		}
	}
	return idx, data, signature, nil
}

func decodePublicKey() (ed25519.PublicKey, error) {
	keyText := strings.TrimSpace(config.RepositoryPublicKey())
	keyRaw, err := hex.DecodeString(keyText)
	if err != nil {
		keyRaw, err = base64.StdEncoding.DecodeString(keyText)
	}
	if err != nil || len(keyRaw) != ed25519.PublicKeySize {
		return nil, errors.New("invalid Ed25519 repository public key")
	}
	return ed25519.PublicKey(keyRaw), nil
}

func decodeSignature(signatureText []byte) ([]byte, error) {
	text := strings.TrimSpace(string(signatureText))
	sig, err := hex.DecodeString(text)
	if err != nil {
		sig, err = base64.StdEncoding.DecodeString(text)
	}
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, errors.New("invalid repository signature")
	}
	return sig, nil
}

func verifyIndexSignatureBytes(data, signature []byte) error {
	if len(signature) != ed25519.SignatureSize {
		return errors.New("repository cache has no valid detached signature")
	}
	key, err := decodePublicKey()
	if err != nil {
		return err
	}
	if !ed25519.Verify(key, data, signature) {
		return errors.New("repository signature verification failed")
	}
	return nil
}

func fetchAndVerifyIndexSignature(data []byte) ([]byte, error) {
	sigURL := strings.TrimSpace(config.RepositorySignatureURL())
	if sigURL == "" {
		return nil, errors.New("repository signatures are required but signature URL is not configured")
	}
	key, err := decodePublicKey()
	if err != nil {
		return nil, err
	}
	sigRaw, err := readSourceLimit(sigURL, maxDetachedSignatureBytes)
	if err != nil {
		return nil, fmt.Errorf("fetch repository signature: %w", err)
	}
	sig, err := decodeSignature(sigRaw)
	if err != nil {
		return nil, err
	}
	if !ed25519.Verify(key, data, sig) {
		return nil, errors.New("repository signature verification failed")
	}
	return sig, nil
}

func CacheIndex(idx model.Index) error {
	return CacheIndexFor(idx, os.Geteuid() != 0)
}

func CacheIndexFor(idx model.Index, user bool) error {
	if err := ValidateStableIndex(idx); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return CacheFetchedIndexFor(data, nil, user)
}

func CacheFetchedIndexFor(rawIndex, signature []byte, user bool) error {
	var idx model.Index
	if err := json.Unmarshal(rawIndex, &idx); err != nil {
		return fmt.Errorf("refusing to cache invalid repository index: %w", err)
	}
	if err := ValidateStableIndex(idx); err != nil {
		return err
	}
	if config.RequireRepositorySignature() {
		if err := verifyIndexSignatureBytes(rawIndex, signature); err != nil {
			return fmt.Errorf("refusing to cache unverified repository index: %w", err)
		}
	}
	path, err := indexCachePathFor(user)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	cache := indexCacheEnvelope{
		FormatVersion: 1,
		RawIndex:      append([]byte(nil), rawIndex...),
		Signature:     append([]byte(nil), signature...),
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	return writeAtomic(bytes.NewReader(data), path)
}

func LoadCachedIndex() (model.Index, error) {
	return LoadCachedIndexFor(os.Geteuid() != 0)
}

func LoadCachedIndexFor(user bool) (model.Index, error) {
	path, err := indexCachePathFor(user)
	if err != nil {
		return model.Index{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Index{}, err
	}
	var cached indexCacheEnvelope
	if err := json.Unmarshal(data, &cached); err == nil && cached.FormatVersion == 1 {
		if len(cached.RawIndex) == 0 {
			return model.Index{}, errors.New("cached repository index is empty")
		}
		if config.RequireRepositorySignature() && len(cached.Signature) == 0 {
			return model.Index{}, errors.New("cached repository index is unsigned; run yspm update")
		}
		if len(cached.Signature) > 0 {
			if err := verifyIndexSignatureBytes(cached.RawIndex, cached.Signature); err != nil {
				return model.Index{}, fmt.Errorf("cached repository signature verification failed: %w", err)
			}
		}
		var idx model.Index
		if err := json.Unmarshal(cached.RawIndex, &idx); err != nil {
			return model.Index{}, fmt.Errorf("invalid cached repository index: %w", err)
		}
		if err := ValidateStableIndex(idx); err != nil {
			return model.Index{}, fmt.Errorf("invalid cached repository index: %w", err)
		}
		return idx, nil
	}

	// Accept old unsigned cache files only when signatures are not required.
	// An old cache cannot prove authenticity because it does not retain the exact
	// signed bytes or detached signature.
	if config.RequireRepositorySignature() {
		return model.Index{}, errors.New("legacy cached index cannot be authenticated; run yspm update")
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

func SupportsArchitecture(p model.Package, requested string) bool {
	requested = config.NormalizeArch(requested)
	if p.Architecture == "" { return true }
	return config.NormalizeArch(p.Architecture) == requested
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
	if u, err := url.Parse(source); err == nil && u.Scheme != "" && u.Scheme != "file" {
		resp, err := (&http.Client{Timeout: 10 * time.Minute}).Get(source)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("download failed: HTTP %s", resp.Status)
		}
		if resp.ContentLength > maxDownloadBytes {
			return fmt.Errorf("download size %d exceeds %d-byte limit", resp.ContentLength, maxDownloadBytes)
		}
		return writeAtomicLimit(resp.Body, destination, maxDownloadBytes)
	}
	if u, err := url.Parse(source); err == nil && u.Scheme == "file" {
		source = u.Path
	}
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil {
		return err
	} else if info.Size() > maxDownloadBytes {
		return fmt.Errorf("download size %d exceeds %d-byte limit", info.Size(), maxDownloadBytes)
	}
	return writeAtomicLimit(file, destination, maxDownloadBytes)
}

func writeAtomic(r io.Reader, destination string) error {
	return writeAtomicLimit(r, destination, maxDownloadBytes)
}

func writeAtomicLimit(r io.Reader, destination string, limit int64) (retErr error) {
	if limit <= 0 {
		return errors.New("invalid download size limit")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".download-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if cleanupErr := os.Remove(tmpPath); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
			retErr = errors.Join(retErr, fmt.Errorf("remove temporary download %s: %w", tmpPath, cleanupErr))
		}
	}()
	n, err := io.Copy(tmp, io.LimitReader(r, limit+1))
	if err != nil {
		if closeErr := tmp.Close(); closeErr != nil {
			return errors.Join(err, fmt.Errorf("close partial download: %w", closeErr))
		}
		return err
	}
	if n > limit {
		closeErr := tmp.Close()
		limitErr := fmt.Errorf("download exceeds %d-byte limit", limit)
		if closeErr != nil {
			return errors.Join(limitErr, fmt.Errorf("close oversized download: %w", closeErr))
		}
		return limitErr
	}
	if err := tmp.Sync(); err != nil {
		if closeErr := tmp.Close(); closeErr != nil {
			return errors.Join(err, fmt.Errorf("close unsynced download: %w", closeErr))
		}
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
		if _, readErr := file.ReadAt(magic[:], 0); readErr != nil && !errors.Is(readErr, io.EOF) {
			if closeErr := file.Close(); closeErr != nil {
				return nil, errors.Join(readErr, fmt.Errorf("close tar archive: %w", closeErr))
			}
			return nil, fmt.Errorf("inspect tar compression header: %w", readErr)
		}
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
		unsupportedErr := fmt.Errorf("unsupported tar compression format %q", format)
		if closeErr := file.Close(); closeErr != nil {
			return nil, errors.Join(unsupportedErr, fmt.Errorf("close tar archive: %w", closeErr))
		}
		return nil, unsupportedErr
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return nil, errors.Join(err, fmt.Errorf("close tar archive: %w", closeErr))
		}
		return nil, err
	}

	switch compression {
	case "":
		return file, nil
	case "gzip":
		reader, err := gzip.NewReader(file)
		if err != nil {
			openErr := fmt.Errorf("open gzip archive: %w", err)
			if closeErr := file.Close(); closeErr != nil {
				return nil, errors.Join(openErr, fmt.Errorf("close archive: %w", closeErr))
			}
			return nil, openErr
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
			if closeErr := file.Close(); closeErr != nil {
				return nil, errors.Join(err, fmt.Errorf("close tar archive: %w", closeErr))
			}
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			startErr := fmt.Errorf("start %s decompressor: %w", program, err)
			if closeErr := stdout.Close(); closeErr != nil {
				startErr = errors.Join(startErr, fmt.Errorf("close decompressor pipe: %w", closeErr))
			}
			if closeErr := file.Close(); closeErr != nil {
				startErr = errors.Join(startErr, fmt.Errorf("close tar archive: %w", closeErr))
			}
			return nil, startErr
		}
		return &commandTarReadCloser{ReadCloser: stdout, cmd: cmd, input: file}, nil
	default:
		unsupportedErr := fmt.Errorf("unsupported tar compression %q", compression)
		if closeErr := file.Close(); closeErr != nil {
			return nil, errors.Join(unsupportedErr, fmt.Errorf("close tar archive: %w", closeErr))
		}
		return nil, unsupportedErr
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
		if closeErr := source.Close(); closeErr != nil {
			return nil, errors.Join(err, fmt.Errorf("close source archive: %w", closeErr))
		}
		return nil, err
	}
	cleanup := func() error {
		var cleanupErr error
		if closeErr := tmp.Close(); closeErr != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("close temporary tar: %w", closeErr))
		}
		if removeErr := os.Remove(tmp.Name()); removeErr != nil && !os.IsNotExist(removeErr) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove temporary tar: %w", removeErr))
		}
		return cleanupErr
	}

	n, copyErr := io.Copy(tmp, io.LimitReader(source, maxTarStreamBytes+1))
	closeErr := source.Close()
	if n > maxTarStreamBytes {
		limitErr := fmt.Errorf("decompressed tar stream exceeds the %d-byte limit", maxTarStreamBytes)
		return nil, errors.Join(limitErr, closeErr, cleanup())
	}
	if copyErr != nil {
		return nil, errors.Join(fmt.Errorf("read tar stream: %w", copyErr), closeErr, cleanup())
	}
	if closeErr != nil {
		return nil, errors.Join(closeErr, cleanup())
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, errors.Join(err, cleanup())
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

func extractTar(archivePath, format, destination string) (retErr error) {
	// Decompress once into a bounded private file, then validate all members
	// before creating any archive-controlled filesystem entries.
	archive, err := spoolTarStream(archivePath, format)
	if err != nil {
		return fmt.Errorf("prepare tar archive: %w", err)
	}
	defer func() {
		name := archive.Name()
		if closeErr := archive.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close validated tar: %w", closeErr))
		}
		if removeErr := os.Remove(name); removeErr != nil && !os.IsNotExist(removeErr) {
			retErr = errors.Join(retErr, fmt.Errorf("remove validated tar %s: %w", name, removeErr))
		}
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
				copyErr = fmt.Errorf("extract archive file %q: %w", entry.name, copyErr)
				if removeErr := os.Remove(target); removeErr != nil && !os.IsNotExist(removeErr) {
					copyErr = errors.Join(copyErr, fmt.Errorf("remove partial archive file %s: %w", target, removeErr))
				}
				return copyErr
			}
			if closeErr != nil {
				if removeErr := os.Remove(target); removeErr != nil && !os.IsNotExist(removeErr) {
					return errors.Join(closeErr, fmt.Errorf("remove incomplete archive file %s: %w", target, removeErr))
				}
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

func extractZip(archivePath, destination string) (retErr error) {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := r.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close ZIP archive: %w", closeErr))
		}
	}()
	if len(r.File) > maxZipEntries {
		return fmt.Errorf("ZIP archive exceeds the %d-entry limit", maxZipEntries)
	}

	type zipEntry struct {
		file *zip.File
		name string
	}
	entries := make([]zipEntry, 0, len(r.File))
	byName := make(map[string]zipEntry, len(r.File))
	var expanded uint64
	for _, f := range r.File {
		name, err := normalizeTarPath(f.Name)
		if err != nil {
			return err
		}
		if name == "." && !f.FileInfo().IsDir() {
			return fmt.Errorf("ZIP file entry cannot name extraction root")
		}
		if _, exists := byName[name]; exists {
			return fmt.Errorf("duplicate ZIP archive path %q", name)
		}
		entry := zipEntry{file: f, name: name}
		byName[name] = entry
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("ZIP symlink entries are not supported: %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if f.UncompressedSize64 != 0 {
				return fmt.Errorf("ZIP directory entry %q has unexpected data", f.Name)
			}
		} else {
			if f.UncompressedSize64 > maxZipEntryBytes {
				return fmt.Errorf("ZIP entry %q exceeds the per-file size limit", name)
			}
			if expanded > maxZipExpandedBytes-f.UncompressedSize64 {
				return fmt.Errorf("ZIP archive exceeds the expanded-size limit")
			}
			expanded += f.UncompressedSize64
		}
		entries = append(entries, entry)
	}
	for _, entry := range entries {
		for parent := pathpkg.Dir(entry.name); parent != "." && parent != "/"; parent = pathpkg.Dir(parent) {
			if parentEntry, exists := byName[parent]; exists && !parentEntry.file.FileInfo().IsDir() {
				return fmt.Errorf("ZIP path %q is nested beneath non-directory entry %q", entry.name, parent)
			}
		}
	}

	root, err := prepareTarDestination(destination)
	if err != nil {
		return err
	}
	var dirs []zipEntry
	for _, entry := range entries {
		if entry.name == "." {
			continue
		}
		if entry.file.FileInfo().IsDir() {
			if _, err := ensureTarDirectories(root, entry.name); err != nil {
				return err
			}
			dirs = append(dirs, entry)
			continue
		}
		parent, err := ensureTarDirectories(root, pathpkg.Dir(entry.name))
		if err != nil {
			return err
		}
		target := filepath.Join(parent, filepath.Base(filepath.FromSlash(entry.name)))
		in, err := entry.file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			if closeErr := in.Close(); closeErr != nil {
				return errors.Join(err, fmt.Errorf("close ZIP entry %s: %w", entry.name, closeErr))
			}
			return err
		}
		n, copyErr := io.CopyN(out, in, int64(entry.file.UncompressedSize64))
		closeOutErr := out.Close()
		closeInErr := in.Close()
		if copyErr != nil {
			copyErr = fmt.Errorf("extract ZIP entry %q after %d bytes: %w", entry.name, n, copyErr)
			if removeErr := os.Remove(target); removeErr != nil && !os.IsNotExist(removeErr) {
				copyErr = errors.Join(copyErr, fmt.Errorf("remove partial ZIP entry %s: %w", target, removeErr))
			}
			return copyErr
		}
		if closeOutErr != nil {
			if removeErr := os.Remove(target); removeErr != nil && !os.IsNotExist(removeErr) {
				return errors.Join(closeOutErr, fmt.Errorf("remove incomplete ZIP entry %s: %w", target, removeErr))
			}
			return closeOutErr
		}
		if closeInErr != nil {
			if removeErr := os.Remove(target); removeErr != nil && !os.IsNotExist(removeErr) {
				return errors.Join(closeInErr, fmt.Errorf("remove incomplete ZIP entry %s: %w", target, removeErr))
			}
			return closeInErr
		}
		if err := os.Chmod(target, entry.file.Mode().Perm()&0o777); err != nil {
			return err
		}
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.Count(dirs[i].name, "/") > strings.Count(dirs[j].name, "/")
	})
	for _, entry := range dirs {
		if err := os.Chmod(filepath.Join(root, filepath.FromSlash(entry.name)), entry.file.Mode().Perm()&0o777); err != nil {
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
