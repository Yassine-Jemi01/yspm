package model

import "time"

// Dependency is stored as a compact expression such as "curl", "openssl>=3.0" or "editor|editor-bin".
type Dependency string

type Package struct {
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Revision     int          `json:"revision,omitempty"`
	Description  string       `json:"description"`
	OS           string       `json:"os"`
	Architecture string       `json:"architecture"`
	License      string       `json:"license"`
	Maintainer   string       `json:"maintainer,omitempty"`
	Size         int64        `json:"size,omitempty"`
	URL          string       `json:"url,omitempty"`
	SHA256       string       `json:"sha256,omitempty"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
	Recommends   []Dependency `json:"recommends,omitempty"`
	Suggests     []Dependency `json:"suggests,omitempty"`
	Conflicts    []string     `json:"conflicts,omitempty"`
	Provides     []string     `json:"provides,omitempty"`
	Replaces     []string     `json:"replaces,omitempty"`
	Kind         string       `json:"kind"` // binary, app, appimage, system, meta
	Format       string       `json:"format,omitempty"`
	Entry        string       `json:"entry,omitempty"`
	Command      string       `json:"command,omitempty"`
	Desktop      bool         `json:"desktop,omitempty"`
	DesktopName  string       `json:"desktop_name,omitempty"`
	Categories   []string     `json:"categories,omitempty"`
	Homepage     string       `json:"homepage,omitempty"`
}

type Index struct {
	Repository   string    `json:"repository"`
	Release      string    `json:"release"`
	Channel      string    `json:"channel"`
	Generated    string    `json:"generated"`
	ReleaseDate  string    `json:"release_date,omitempty"`
	EOL          string    `json:"eol,omitempty"`
	Architecture string    `json:"architecture,omitempty"`
	SupportedOS  []string  `json:"supported_os,omitempty"`
	KeyID        string    `json:"key_id,omitempty"`
	Signature    string    `json:"signature,omitempty"`
	Packages     []Package `json:"packages"`
}

type InstalledPackage struct {
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Revision     int          `json:"revision,omitempty"`
	Kind         string       `json:"kind"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
	InstallDir   string       `json:"install_dir,omitempty"`
	Executable   string       `json:"executable,omitempty"`
	Command      string       `json:"command,omitempty"`
	DesktopFile  string       `json:"desktop_file,omitempty"`
	Files        []string     `json:"files,omitempty"`
	Checksum     string       `json:"checksum,omitempty"`
	Explicit     bool         `json:"explicit"`
	InstalledAt  time.Time    `json:"installed_at"`
}

type Transaction struct {
	ID         string    `json:"id"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Action     string    `json:"action"`
	Packages   []string  `json:"packages,omitempty"`
	Status     string    `json:"status"` // running, success, failed, cancelled
	Error      string    `json:"error,omitempty"`
}

type Database struct {
	Release      string                      `json:"release,omitempty"`
	Packages     map[string]InstalledPackage `json:"packages"`
	Transactions []Transaction               `json:"transactions,omitempty"`
}
