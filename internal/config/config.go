package config

import (
	"os"
	"path/filepath"
)

const DefaultRepository = "https://raw.githubusercontent.com/Yassine-Jemi01/yspm/main/repo/releases/1/index.json"

func RepositoryURL() string {
	if value := os.Getenv("YSPM_REPOSITORY"); value != "" {
		return value
	}
	return DefaultRepository
}

func RepositorySignatureURL() string   { return os.Getenv("YSPM_REPOSITORY_SIGNATURE") }
func RepositoryPublicKey() string      { return os.Getenv("YSPM_REPOSITORY_PUBLIC_KEY") }
func RequireRepositorySignature() bool { return os.Getenv("YSPM_REQUIRE_SIGNATURES") == "1" }

func envPath(name string) string { return os.Getenv(name) }

func homePath(parts ...string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	all := append([]string{home}, parts...)
	return filepath.Join(all...), nil
}

func CacheDir() (string, error) {
	if p := envPath("YSPM_CACHE_DIR"); p != "" {
		return p, nil
	}
	return homePath(".cache", "yspm")
}

func DataDir() (string, error) {
	if p := envPath("YSPM_DATA_DIR"); p != "" {
		return p, nil
	}
	return homePath(".local", "share", "yspm")
}

func BinDir() (string, error) {
	if p := envPath("YSPM_BIN_DIR"); p != "" {
		return p, nil
	}
	return homePath(".local", "bin")
}

func ApplicationsDir() (string, error) {
	if p := envPath("YSPM_APPLICATIONS_DIR"); p != "" {
		return p, nil
	}
	return homePath(".local", "share", "applications")
}

func RootDir() string { return envPath("YSPM_ROOT") }
