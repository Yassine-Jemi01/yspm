package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Yassine-Jemi01/yspm/internal/config"
	"github.com/Yassine-Jemi01/yspm/internal/model"
)

func LoadDB() (model.Database, error) {
	dir, err := config.DataDir()
	if err != nil {
		return model.Database{}, err
	}
	path := filepath.Join(dir, "database.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return model.Database{Packages: map[string]model.InstalledPackage{}}, nil
	}
	if err != nil {
		return model.Database{}, err
	}
	var db model.Database
	if err := json.Unmarshal(data, &db); err != nil {
		return model.Database{}, fmt.Errorf("invalid database: %w", err)
	}
	if db.Packages == nil {
		db.Packages = map[string]model.InstalledPackage{}
	}
	return db, nil
}

func SaveDB(db model.Database) error {
	dir, err := config.DataDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "database-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, filepath.Join(dir, "database.json"))
}
