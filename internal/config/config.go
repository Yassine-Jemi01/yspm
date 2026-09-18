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

func homePath(parts ...string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	all := append([]string{home}, parts...)
	return filepath.Join(all...), nil
}

func CacheDir() (string, error) { return homePath(".cache", "yspm") }
func DataDir() (string, error)  { return homePath(".local", "share", "yspm") }
func BinDir() (string, error)   { return homePath(".local", "bin") }
