package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	DefaultRepository = "https://raw.githubusercontent.com/Yassine-Jemi01/yspm/main/repo/releases/1/index.json"
	DefaultSystemABI  = "yspm-abi-1"
)

type Paths struct {
	Root             string
	State            string
	Cache            string
	Bin              string
	Applications     string
	Staging          string
	Database         string
	Transactions     string
	Snapshots        string
}

func homeDir() (string, error) { return os.UserHomeDir() }

func NewPaths(user bool) (Paths, error) {
	if user {
		home, err := homeDir()
		if err != nil { return Paths{}, err }
		data := filepath.Join(home, ".local", "share", "yspm")
		return Paths{
			Root: "/", State: data, Cache: filepath.Join(home, ".cache", "yspm"),
			Bin: filepath.Join(home, ".local", "bin"),
			Applications: filepath.Join(home, ".local", "share", "applications"),
			Staging: filepath.Join(data, "staging"), Database: filepath.Join(data, "database.json"),
			Transactions: filepath.Join(data, "transactions"), Snapshots: filepath.Join(data, "snapshots"),
		}, nil
	}
	root := strings.TrimSuffix(os.Getenv("YSPM_ROOT"), string(os.PathSeparator))
	if root == "" { root = "/" }
	state := filepath.Join(root, "var", "lib", "yspm")
	cache := filepath.Join(root, "var", "cache", "yspm")
	bin := filepath.Join(root, "usr", "local", "bin")
	apps := filepath.Join(root, "usr", "share", "applications")
	return Paths{
		Root: root, State: state, Cache: cache, Bin: bin, Applications: apps,
		Staging: filepath.Join(state, "staging"), Database: filepath.Join(state, "database.json"),
		Transactions: filepath.Join(state, "transactions"), Snapshots: filepath.Join(state, "snapshots"),
	}, nil
}

func RepositoryURL() string {
	if v := os.Getenv("YSPM_REPOSITORY"); v != "" { return v }
	return DefaultRepository
}

func ReleaseRepositoryURL(release string) string {
	if v := os.Getenv("YSPM_RELEASE_REPOSITORY_TEMPLATE"); v != "" {
		return strings.ReplaceAll(v, "{release}", release)
	}
	source := RepositoryURL()
	const marker = "/releases/"
	if i := strings.Index(source, marker); i >= 0 {
		rest := source[i+len(marker):]
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			return source[:i+len(marker)] + release + rest[slash:]
		}
	}
	return source
}

func RepositorySignatureURL() string { return os.Getenv("YSPM_REPOSITORY_SIGNATURE") }
func RepositoryPublicKey() string { return os.Getenv("YSPM_REPOSITORY_PUBLIC_KEY") }
func RequireRepositorySignature() bool { return os.Getenv("YSPM_REQUIRE_SIGNATURES") == "1" }

func SnapshotRoot() string {
	if v := os.Getenv("YSPM_SNAPSHOT_ROOT"); v != "" { return v }
	return "/"
}
func SnapshotStorage() string {
	if v := os.Getenv("YSPM_SNAPSHOT_DIR"); v != "" { return v }
	return "/var/lib/yspm/snapshots"
}

func ForeignArchitectures() map[string]bool {
	out := map[string]bool{}
	for _, a := range strings.Split(os.Getenv("YSPM_FOREIGN_ARCHS"), ",") {
		a = strings.TrimSpace(a)
		if a != "" { out[NormalizeArch(a)] = true }
	}
	return out
}

func NormalizeArch(a string) string {
	switch strings.ToLower(strings.TrimSpace(a)) {
	case "amd64": return "x86_64"
	case "x86-64": return "x86_64"
	case "arm64": return "aarch64"
	case "armhf": return "armv7"
	case "386": return "i386"
	default: return strings.ToLower(strings.TrimSpace(a))
	}
}

func HostOS() string { return runtime.GOOS }
func HostArch() string { return NormalizeArch(runtime.GOARCH) }

func CacheDir() (string,error) {
	user := os.Geteuid() != 0
	p, err := NewPaths(user)
	if err != nil { return "", err }
	return p.Cache, nil
}
func DataDir() (string,error) {
	user := os.Geteuid() != 0
	p, err := NewPaths(user)
	if err != nil { return "", err }
	return p.State, nil
}
func BinDir() (string,error) {
	user := os.Geteuid() != 0
	p, err := NewPaths(user)
	if err != nil { return "", err }
	return p.Bin, nil
}
func ApplicationsDir() (string,error) {
	user := os.Geteuid() != 0
	p, err := NewPaths(user)
	if err != nil { return "", err }
	return p.Applications, nil
}
