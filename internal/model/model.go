package model

import "time"

type Dependency string

type Service struct {
	Name    string `json:"name"`
	Enable  bool   `json:"enable,omitempty"`
	Start   bool   `json:"start,omitempty"`
	Restart bool   `json:"restart,omitempty"`
}

type Vulnerability struct {
	ID           string `json:"id"`
	Severity     string `json:"severity,omitempty"`
	AffectedLess string `json:"affected_less,omitempty"`
	FixedVersion string `json:"fixed_version,omitempty"`
	URL          string `json:"url,omitempty"`
}

type FileEntry struct {
	Path       string `json:"path"`
	Type       string `json:"type"` // file, dir, symlink
	Mode       uint32 `json:"mode"`
	SHA256     string `json:"sha256,omitempty"`
	LinkTarget string `json:"link_target,omitempty"`
}

type Package struct {
	Name              string            `json:"name"`
	Version           string            `json:"version"`
	Revision          int               `json:"revision,omitempty"`
	Description       string            `json:"description"`
	OS                string            `json:"os"`
	Architecture      string            `json:"architecture"`
	License           string            `json:"license"`
	Maintainer        string            `json:"maintainer,omitempty"`
	Size              int64             `json:"size,omitempty"`
	URL               string            `json:"url,omitempty"`
	SHA256            string            `json:"sha256,omitempty"`
	Dependencies      []Dependency      `json:"dependencies,omitempty"`
	Recommends        []Dependency      `json:"recommends,omitempty"`
	Suggests          []Dependency      `json:"suggests,omitempty"`
	Conflicts         []string          `json:"conflicts,omitempty"`
	Provides          []string          `json:"provides,omitempty"`
	Replaces          []string          `json:"replaces,omitempty"`
	Kind              string            `json:"kind"` // binary, app, appimage, system, meta
	Format            string            `json:"format,omitempty"`
	Entry             string            `json:"entry,omitempty"`
	Command           string            `json:"command,omitempty"`
	Desktop           bool              `json:"desktop,omitempty"`
	DesktopName       string            `json:"desktop_name,omitempty"`
	Categories        []string          `json:"categories,omitempty"`
	Homepage          string            `json:"homepage,omitempty"`
	ABI               string            `json:"abi,omitempty"`
	SharedProvides    []string          `json:"shared_provides,omitempty"`
	SharedRequires    []string          `json:"shared_requires,omitempty"`
	ConfigFiles       []string          `json:"config_files,omitempty"`
	Services          []Service         `json:"services,omitempty"`
	Vulnerabilities   []Vulnerability   `json:"vulnerabilities,omitempty"`
	Triggers          []string          `json:"triggers,omitempty"`
	Files             []FileEntry       `json:"files,omitempty"`
	BuildDependencies []Dependency      `json:"build_dependencies,omitempty"`
}

type Index struct {
	Repository        string    `json:"repository"`
	Release           string    `json:"release"`
	Channel           string    `json:"channel"`
	Generated         string    `json:"generated"`
	ReleaseDate       string    `json:"release_date,omitempty"`
	EOL               string    `json:"eol,omitempty"`
	ABI               string    `json:"abi,omitempty"`
	Architecture      string    `json:"architecture,omitempty"`
	SupportedArchs    []string  `json:"supported_architectures,omitempty"`
	SupportedOS       []string  `json:"supported_os,omitempty"`
	KeyID             string    `json:"key_id,omitempty"`
	Signature         string    `json:"signature,omitempty"`
	Packages          []Package `json:"packages"`
}

type InstalledPackage struct {
	Name            string          `json:"name"`
	Version         string          `json:"version"`
	Revision        int             `json:"revision,omitempty"`
	Kind            string          `json:"kind"`
	Format          string          `json:"format,omitempty"`
	Architecture    string          `json:"architecture,omitempty"`
	ABI             string          `json:"abi,omitempty"`
	Dependencies    []Dependency    `json:"dependencies,omitempty"`
	InstallDir      string          `json:"install_dir,omitempty"`
	Executable      string          `json:"executable,omitempty"`
	Command         string          `json:"command,omitempty"`
	DesktopFile     string          `json:"desktop_file,omitempty"`
	Files           []string        `json:"files,omitempty"`
	Manifest        []FileEntry     `json:"manifest,omitempty"`
	FileHashes      map[string]string `json:"file_hashes,omitempty"`
	ConfigHashes    map[string]string `json:"config_hashes,omitempty"`
	Checksum        string          `json:"checksum,omitempty"`
	Explicit        bool            `json:"explicit"`
	Services        []Service       `json:"services,omitempty"`
	Hooks           map[string]string `json:"hooks,omitempty"`
	InstalledAt     time.Time       `json:"installed_at"`
}

type Transaction struct {
	ID             string    `json:"id"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at,omitempty"`
	Action         string    `json:"action"`
	Packages       []string  `json:"packages,omitempty"`
	Status         string    `json:"status"`
	Error          string    `json:"error,omitempty"`
	SnapshotBefore string    `json:"snapshot_before,omitempty"`
}

type Snapshot struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	ReadOnly  bool      `json:"read_only"`
}

type Database struct {
	SchemaVersion int                         `json:"schema_version"`
	Release       string                      `json:"release,omitempty"`
	ABI           string                      `json:"abi,omitempty"`
	Packages      map[string]InstalledPackage `json:"packages"`
	Transactions  []Transaction               `json:"transactions,omitempty"`
	Snapshots     []Snapshot                  `json:"snapshots,omitempty"`
}
