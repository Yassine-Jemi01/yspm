package store

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Yassine-Jemi01/yspm/internal/model"
)

func TestDatabaseWritesPreserveNewerPackageStateAndHistory(t *testing.T) {
	t.Setenv("YSPM_DATA_DIR", t.TempDir())
	initial := model.Database{
		SchemaVersion: CurrentSchema,
		Packages: map[string]model.InstalledPackage{
			"existing": {Name: "existing", Version: "1.0.0"},
		},
	}
	if err := SaveDBFor(true, initial); err != nil {
		t.Fatal(err)
	}

	latest, err := LoadDBFor(true)
	if err != nil {
		t.Fatal(err)
	}
	latest.Packages["newer"] = model.InstalledPackage{Name: "newer", Version: "2.0.0"}
	if err := SaveDBFor(true, latest); err != nil {
		t.Fatal(err)
	}
	if err := AddTransactionFor(true, model.Transaction{
		ID: "transaction-race", Action: "install", Status: "running", StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := LoadDBFor(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Packages["newer"]; !ok {
		t.Fatal("atomic transaction-history mutation overwrote a newer package database")
	}
	if len(got.Transactions) != 1 || got.Transactions[0].ID != "transaction-race" {
		t.Fatalf("transaction history was lost: %#v", got.Transactions)
	}
}

func TestConcurrentAddTransactionForDoesNotDropRecords(t *testing.T) {
	t.Setenv("YSPM_DATA_DIR", t.TempDir())
	if err := SaveDBFor(true, model.Database{Packages: map[string]model.InstalledPackage{}}); err != nil {
		t.Fatal(err)
	}
	const count = 32
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- AddTransactionFor(true, model.Transaction{
				ID: fmt.Sprintf("tx-%02d", i), Action: "install", Status: "running", StartedAt: time.Now(),
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("add transaction: %v", err)
		}
	}
	got, err := LoadDBFor(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Transactions) != count {
		t.Fatalf("got %d records, want %d", len(got.Transactions), count)
	}
}

func TestSavingStaleRunningTransactionCannotUndoSuccess(t *testing.T) {
	t.Setenv("YSPM_DATA_DIR", t.TempDir())
	tx := model.Transaction{
		ID: "status-race", Action: "install", Status: "running", StartedAt: time.Now(),
	}
	if err := AddTransactionFor(true, tx); err != nil {
		t.Fatal(err)
	}
	stale, err := LoadDBFor(true)
	if err != nil {
		t.Fatal(err)
	}
	completed := tx
	completed.Status = "success"
	completed.FinishedAt = time.Now().Add(time.Second)
	if err := UpdateTransactionFor(true, completed); err != nil {
		t.Fatal(err)
	}
	if err := SaveDBFor(true, stale); err != nil {
		t.Fatal(err)
	}
	got, err := GetTransactionFor(true, tx.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "success" {
		t.Fatalf("stale DB write reverted transaction to %q", got.Status)
	}
}

func TestSaveSnapshotPreservesConcurrentSnapshots(t *testing.T) {
	t.Setenv("YSPM_DATA_DIR", t.TempDir())
	now := time.Now()
	const count = 12
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- SaveSnapshotFor(true, model.Snapshot{
				ID: fmt.Sprintf("snapshot-%02d", i), Path: fmt.Sprintf("/snapshots/%d", i), CreatedAt: now.Add(time.Duration(i) * time.Second),
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("save snapshot: %v", err)
		}
	}
	db, err := LoadDBFor(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(db.Snapshots) != count {
		t.Fatalf("got %d snapshots, want %d", len(db.Snapshots), count)
	}
}
