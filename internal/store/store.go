package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Yassine-Jemi01/yspm/internal/config"
	"github.com/Yassine-Jemi01/yspm/internal/model"
)

func path() (string, error) {
	dir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "database.json"), nil
}

func LoadDB() (model.Database, error) {
	p, err := path()
	if err != nil {
		return model.Database{}, err
	}
	data, err := os.ReadFile(p)
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
	return os.Rename(tmpPath, filepath.Join(dir, "database.json"))
}

func AddTransaction(tx model.Transaction) error {
	db, err := LoadDB()
	if err != nil {
		return err
	}
	db.Transactions = append(db.Transactions, tx)
	if len(db.Transactions) > 100 {
		db.Transactions = db.Transactions[len(db.Transactions)-100:]
	}
	return SaveDB(db)
}

func UpdateTransaction(tx model.Transaction) error {
	db, err := LoadDB()
	if err != nil {
		return err
	}
	for i := range db.Transactions {
		if db.Transactions[i].ID == tx.ID {
			db.Transactions[i] = tx
			return SaveDB(db)
		}
	}
	return fmt.Errorf("transaction %s not found", tx.ID)
}

func GetTransaction(id string) (model.Transaction, error) {
	db, err := LoadDB()
	if err != nil {
		return model.Transaction{}, err
	}
	for _, tx := range db.Transactions {
		if tx.ID == id {
			return tx, nil
		}
	}
	return model.Transaction{}, fmt.Errorf("transaction %s not found", id)
}
