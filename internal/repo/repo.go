package repo

import (
	"archive/zip"
	"bufio"
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
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Yassine-Jemi01/yspm/internal/config"
	"github.com/Yassine-Jemi01/yspm/internal/model"
)

func indexCachePath() (string, error) {
	d, err := config.CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "index.json"), nil
}
func readSource(source string) ([]byte, error) {
	if u, err := url.Parse(source); err == nil && u.Scheme != "" && u.Scheme != "file" {
		r := &http.Client{Timeout: 45 * time.Second}
		resp, err := r.Get(source)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("repository returned HTTP %s", resp.Status)
		}
		return io.ReadAll(resp.Body)
	}
	if u, err := url.Parse(source); err == nil && u.Scheme == "file" {
		source = u.Path
	}
	return os.ReadFile(source)
}

func FetchIndex(source string) (model.Index, error) {
	data, err := readSource(source)
	if err != nil {
		return model.Index{}, fmt.Errorf("fetch repository index: %w", err)
	}
	var idx model.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return model.Index{}, fmt.Errorf("invalid repository index: %w", err)
	}
	if idx.Release == "" || idx.Channel == "" {
		return model.Index{}, errors.New("repository index is missing release/channel")
	}
	if config.RequireRepositorySignature() {
		if err := verifyIndexSignature(data); err != nil {
			return model.Index{}, err
		}
	}
	return idx, nil
}

func verifyIndexSignature(data []byte) error {
	keyText := strings.TrimSpace(config.RepositoryPublicKey())
	sigURL := strings.TrimSpace(config.RepositorySignatureURL())
	if keyText == "" || sigURL == "" {
		return errors.New("repository signatures are required but public key/signature URL are not configured")
	}
	keyRaw, err := hex.DecodeString(strings.TrimSpace(keyText))
	if err != nil {
		keyRaw, err = base64.StdEncoding.DecodeString(keyText)
	}
	if err != nil || len(keyRaw) != ed25519.PublicKeySize {
		return errors.New("invalid Ed25519 repository public key")
	}
	sigRaw, err := readSource(sigURL)
	if err != nil {
		return fmt.Errorf("fetch repository signature: %w", err)
	}
	sigText := strings.TrimSpace(string(sigRaw))
	sig, err := hex.DecodeString(sigText)
	if err != nil {
		sig, err = base64.StdEncoding.DecodeString(sigText)
	}
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("invalid repository signature")
	}
	if !ed25519.Verify(ed25519.PublicKey(keyRaw), data, sig) {
		return errors.New("repository signature verification failed")
	}
	return nil
}

func CacheIndex(idx model.Index) error {
	path, err := indexCachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "index-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
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

func LoadCachedIndex() (model.Index, error) {
	path, err := indexCachePath()
	if err != nil {
		return model.Index{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Index{}, err
	}
	var idx model.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return model.Index{}, fmt.Errorf("invalid cached repository index: %w", err)
	}
	return idx, nil
}

func CurrentSystem() (string, string) { return runtime.GOOS, runtime.GOARCH }
func SupportsCurrentSystem(p model.Package) bool {
	osName, arch := CurrentSystem()
	return (p.OS == "" || p.OS == osName || (osName == "linux" && p.OS == "linux")) && (p.Architecture == "" || p.Architecture == arch || (arch == "amd64" && p.Architecture == "x86_64"))
}

func ValidateInstallPackage(p model.Package) error {
	if p.Kind == "meta" {
		return nil
	}
	if strings.TrimSpace(p.SHA256) == "" {
		return fmt.Errorf("package %q has no SHA-256 checksum", p.Name)
	}
	if len(strings.TrimSpace(p.SHA256)) != 64 {
		return fmt.Errorf("package %q has an invalid SHA-256 checksum", p.Name)
	}
	if _, err := hex.DecodeString(p.SHA256); err != nil {
		return fmt.Errorf("package %q has an invalid SHA-256 checksum", p.Name)
	}
	if p.URL == "" {
		return fmt.Errorf("package %q has no download URL", p.Name)
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
		return writeAtomic(resp.Body, destination)
	}
	if u, err := url.Parse(source); err == nil && u.Scheme == "file" {
		source = u.Path
	}
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeAtomic(f, destination)
}

func writeAtomic(r io.Reader, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".download-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, r); err != nil {
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
	return os.Rename(tmpPath, destination)
}

func ExtractArchive(archivePath, format, destination string) error {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	switch strings.ToLower(format) {
	case "zip":
		return extractZip(archivePath, destination)
	case "tar.gz", "tgz", "tar.xz", "tar.bz2", "tar.zst", "tar":
		return extractTar(archivePath, destination)
	default:
		return fmt.Errorf("unsupported archive format %q", format)
	}
}

func validateTarList(out string) error {
	s := bufio.NewScanner(strings.NewReader(out))
	for s.Scan() {
		name := strings.TrimSpace(s.Text())
		name = strings.TrimPrefix(name, "./")
		if name == "" {
			continue
		}
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") || strings.Contains(name, string(filepath.Separator)+"../") {
			return fmt.Errorf("unsafe archive path %q", name)
		}
	}
	return s.Err()
}

func extractTar(archivePath, destination string) error {
	list := exec.Command("tar", "-tf", archivePath)
	out, err := list.Output()
	if err != nil {
		return fmt.Errorf("list tar archive: %w", err)
	}
	if err := validateTarList(string(out)); err != nil {
		return err
	}
	cmd := exec.Command("tar", "-xf", archivePath, "-C", destination)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extract tar: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func extractZip(archivePath, destination string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		name := filepath.Clean(f.Name)
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("unsafe archive path %q", f.Name)
		}
		target := filepath.Join(destination, name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destination)+string(os.PathSeparator)) && target != destination {
			return fmt.Errorf("archive escapes destination: %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeOutErr := out.Close()
		closeInErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOutErr != nil {
			return closeOutErr
		}
		if closeInErr != nil {
			return closeInErr
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
