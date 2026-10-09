package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/Yassine-Jemi01/yspm/internal/config"
	"github.com/Yassine-Jemi01/yspm/internal/model"
)

const CurrentSchema = 3

func LoadDBFor(user bool) (model.Database, error) {
	p, err := config.NewPaths(user)
	if err != nil {
		return model.Database{}, err
	}
	return loadDBPath(p.Database)
}

func loadDBPath(path string) (model.Database, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return model.Database{
			SchemaVersion: CurrentSchema,
			ABI:           config.DefaultSystemABI,
			Packages:      map[string]model.InstalledPackage{},
		}, nil
	}
	if err != nil {
		return model.Database{}, err
	}
	var db model.Database
	if err := json.Unmarshal(data, &db); err != nil {
		return model.Database{}, fmt.Errorf("invalid database: %w", err)
	}
	if db.SchemaVersion == 0 {
		db.SchemaVersion = 1
	}
	if db.Packages == nil {
		db.Packages = map[string]model.InstalledPackage{}
	}
	for name, pkg := range db.Packages {
		if pkg.Name == "" {
			pkg.Name = name
		}
		if pkg.FileHashes == nil {
			pkg.FileHashes = map[string]string{}
		}
		if pkg.ConfigHashes == nil {
			pkg.ConfigHashes = map[string]string{}
		}
		if len(pkg.Manifest) == 0 && len(pkg.Files) > 0 {
			for _, path := range pkg.Files {
				pkg.Manifest = append(pkg.Manifest, model.FileEntry{Path: path, Type: "file"})
			}
		}
		db.Packages[name] = pkg
	}
	if db.ABI == "" {
		db.ABI = config.DefaultSystemABI
	}
	return db, nil
}

func withDBLock(databasePath string, fn func() error) (retErr error) {
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o755); err != nil {
		return err
	}
	lockPath := databasePath + ".lock"
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open database lock: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close database lock: %w", closeErr))
		}
	}()
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err == nil {
			break
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return fmt.Errorf("acquire database lock: %w", err)
	}
	defer func() {
		if unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); unlockErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("release database lock: %w", unlockErr))
		}
	}()
	return fn()
}

func SaveDBFor(user bool, db model.Database) error {
	p, err := config.NewPaths(user)
	if err != nil {
		return err
	}
	return withDBLock(p.Database, func() error {
		latest, err := loadDBPath(p.Database)
		if err != nil {
			return err
		}
		db.Transactions = mergeTransactions(latest.Transactions, db.Transactions)
		db.Snapshots = mergeSnapshots(latest.Snapshots, db.Snapshots)
		return writeDBPath(p.Database, db)
	})
}

func writeDBPath(path string, db model.Database) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	db.SchemaVersion = CurrentSchema
	data, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "database-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		if closeErr := tmp.Close(); closeErr != nil {
			return errors.Join(err, fmt.Errorf("close database temp file: %w", closeErr))
		}
		return err
	}
	if err := tmp.Sync(); err != nil {
		if closeErr := tmp.Close(); closeErr != nil {
			return errors.Join(err, fmt.Errorf("close database temp file: %w", closeErr))
		}
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return err
	}
	return nil
}

func mergeTransactions(current, incoming []model.Transaction) []model.Transaction {
	out := append([]model.Transaction(nil), current...)
	positions := make(map[string]int, len(out)+len(incoming))
	for i, tx := range out {
		if tx.ID != "" {
			positions[tx.ID] = i
		}
	}
	for _, tx := range incoming {
		if tx.ID == "" {
			out = append(out, tx)
			continue
		}
		if i, ok := positions[tx.ID]; ok {
			old := out[i]
			// A completed record must not be reverted to stale "running" state
			// by a database snapshot that was loaded before the completion.
			switch {
			case old.FinishedAt.After(tx.FinishedAt):
				continue
			case !old.FinishedAt.IsZero() && tx.FinishedAt.IsZero():
				continue
			case old.FinishedAt.Equal(tx.FinishedAt) && !old.FinishedAt.IsZero():
				// Keep equal completion timestamps deterministic.
				if old.Status != "running" && tx.Status == "running" {
					continue
				}
			}
			out[i] = tx
			continue
		}
		positions[tx.ID] = len(out)
		out = append(out, tx)
	}
	if len(out) > 200 {
		out = append([]model.Transaction(nil), out[len(out)-200:]...)
	}
	return out
}

func mergeSnapshots(current, incoming []model.Snapshot) []model.Snapshot {
	out := append([]model.Snapshot(nil), current...)
	positions := make(map[string]int, len(out)+len(incoming))
	for i, snapshot := range out {
		if snapshot.ID != "" {
			positions[snapshot.ID] = i
		}
	}
	for _, snapshot := range incoming {
		if snapshot.ID == "" {
			out = append(out, snapshot)
			continue
		}
		if i, ok := positions[snapshot.ID]; ok {
			if snapshot.CreatedAt.After(out[i].CreatedAt) {
				out[i] = snapshot
			}
			continue
		}
		positions[snapshot.ID] = len(out)
		out = append(out, snapshot)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func AddTransactionFor(user bool, tx model.Transaction) error {
	p, err := config.NewPaths(user)
	if err != nil {
		return err
	}
	return withDBLock(p.Database, func() error {
		db, err := loadDBPath(p.Database)
		if err != nil {
			return err
		}
		db.Transactions = append(db.Transactions, tx)
		if len(db.Transactions) > 200 {
			db.Transactions = append([]model.Transaction(nil), db.Transactions[len(db.Transactions)-200:]...)
		}
		return writeDBPath(p.Database, db)
	})
}

func UpdateTransactionFor(user bool, tx model.Transaction) error {
	p, err := config.NewPaths(user)
	if err != nil {
		return err
	}
	return withDBLock(p.Database, func() error {
		db, err := loadDBPath(p.Database)
		if err != nil {
			return err
		}
		for i := range db.Transactions {
			if db.Transactions[i].ID == tx.ID {
				db.Transactions[i] = tx
				return writeDBPath(p.Database, db)
			}
		}
		return fmt.Errorf("transaction %s not found", tx.ID)
	})
}

func GetTransactionFor(user bool, id string) (model.Transaction, error) {
	db, err := LoadDBFor(user)
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

func SaveSnapshotFor(user bool, snapshot model.Snapshot) error {
	p, err := config.NewPaths(user)
	if err != nil {
		return err
	}
	return withDBLock(p.Database, func() error {
		db, err := loadDBPath(p.Database)
		if err != nil {
			return err
		}
		db.Snapshots = mergeSnapshots(db.Snapshots, []model.Snapshot{snapshot})
		return writeDBPath(p.Database, db)
	})
}
