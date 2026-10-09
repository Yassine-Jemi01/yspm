package manager

import (
	"testing"
	"time"

	"github.com/Yassine-Jemi01/yspm/internal/model"
	"github.com/Yassine-Jemi01/yspm/internal/store"
)

func TestSaveDatabasePreservesTransactionAddedAfterLoad(t *testing.T) {
	t.Setenv("YSPM_DATA_DIR", t.TempDir())

	initial := model.Database{
		SchemaVersion: store.CurrentSchema,
		Packages:      map[string]model.InstalledPackage{},
	}
	if err := store.SaveDBFor(true, initial); err != nil {
		t.Fatalf("save initial database: %v", err)
	}

	// Simulate runTransaction loading the DB before startTransaction persists
	// its new record.
	staleDB, err := store.LoadDBFor(true)
	if err != nil {
		t.Fatalf("load stale database: %v", err)
	}

	tx := model.Transaction{
		ID:        "transaction-regression",
		StartedAt: time.Now(),
		Action:    "install",
		Packages:  []string{"example"},
		Status:    "running",
	}
	if err := store.AddTransactionFor(true, tx); err != nil {
		t.Fatalf("record started transaction: %v", err)
	}

	// Simulate package changes made during the successful transaction.
	staleDB.Packages["example"] = model.InstalledPackage{
		Name:    "example",
		Version: "1.0.0",
	}

	m := New(true, "x86_64")
	if err := store.SaveDBFor(true, staleDB); err != nil {
		t.Fatalf("save package database while preserving history: %v", err)
	}
	if err := m.finishSuccess(tx); err != nil {
		t.Fatalf("finish successful transaction: %v", err)
	}

	got, err := store.LoadDBFor(true)
	if err != nil {
		t.Fatalf("reload final database: %v", err)
	}
	if _, ok := got.Packages["example"]; !ok {
		t.Fatal("successful package changes were not saved")
	}
	if len(got.Transactions) != 1 {
		t.Fatalf("expected one transaction in history, got %d", len(got.Transactions))
	}
	if got.Transactions[0].ID != tx.ID {
		t.Fatalf("unexpected transaction in history: got %q, want %q", got.Transactions[0].ID, tx.ID)
	}
	if got.Transactions[0].Status != "success" {
		t.Fatalf("transaction status = %q, want %q", got.Transactions[0].Status, "success")
	}
}
