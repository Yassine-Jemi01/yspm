package model

// Package describes a single installable item in the repository.
type Package struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Description  string   `json:"description"`
	OS           string   `json:"os"`
	Architecture string   `json:"architecture"`
	License      string   `json:"license"`
	Size         int64    `json:"size,omitempty"`
	URL          string   `json:"url,omitempty"`
	SHA256       string   `json:"sha256,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
	Kind         string   `json:"kind"`              // binary, app, appimage, meta
	Format       string   `json:"format,omitempty"`  // tar.gz, tar.xz, zip, appimage
	Entry        string   `json:"entry,omitempty"`   // executable path/name inside an archive
	Command      string   `json:"command,omitempty"` // command exposed in ~/.local/bin
	Homepage     string   `json:"homepage,omitempty"`
}

type Index struct {
	Repository  string    `json:"repository"`
	Release     string    `json:"release"`
	Channel     string    `json:"channel"`
	Generated   string    `json:"generated"`
	ReleaseDate string    `json:"release_date,omitempty"`
	EOL         string    `json:"eol,omitempty"`
	Packages    []Package `json:"packages"`
}

type InstalledPackage struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Kind         string   `json:"kind"`
	Dependencies []string `json:"dependencies,omitempty"`
	InstallDir   string   `json:"install_dir,omitempty"`
	Executable   string   `json:"executable,omitempty"`
	Command      string   `json:"command,omitempty"`
}

type Database struct {
	Release  string                      `json:"release,omitempty"`
	Packages map[string]InstalledPackage `json:"packages"`
}
