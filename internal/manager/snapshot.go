package manager

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Yassine-Jemi01/yspm/internal/config"
	"github.com/Yassine-Jemi01/yspm/internal/model"
	"github.com/Yassine-Jemi01/yspm/internal/store"
)

func CreateSnapshot(m *Manager) (string,error) {
	if m.User { return "", errors.New("system snapshots are unavailable in --user mode") }
	if err:=m.requirePrivileges("snapshot create");err!=nil{return "",err}
	btrfs,err:=exec.LookPath("btrfs");if err!=nil{return "",errors.New("btrfs command not found")}
	source:=config.SnapshotRoot()
	base:=config.SnapshotStorage()
	if err:=os.MkdirAll(base,0o755);err!=nil{return "",err}
	id:=fmt.Sprintf("%s-%d",time.Now().UTC().Format("20060102-150405"),time.Now().UnixNano()%100000)
	dst:=filepath.Join(base,id)
	if filepath.Clean(dst)==filepath.Clean(source){return "",errors.New("snapshot destination cannot equal source")}
	cmd:=exec.Command(btrfs,"subvolume","snapshot","-r",source,dst)
	if out,err:=cmd.CombinedOutput();err!=nil{return "",fmt.Errorf("create btrfs snapshot: %w: %s",err,strings.TrimSpace(string(out)))}
	s:=model.Snapshot{ID:id,Path:dst,CreatedAt:time.Now(),ReadOnly:true}
	if err := store.SaveSnapshotFor(m.User, s); err != nil {
		cleanupErr := exec.Command(btrfs, "subvolume", "delete", dst).Run()
		if cleanupErr != nil {
			return "", errors.Join(fmt.Errorf("record created snapshot: %w", err), fmt.Errorf("remove unrecorded snapshot: %w", cleanupErr))
		}
		return "", err
	}
	fmt.Printf("Snapshot created: %s\n",id)
	return id,nil
}

func ListSnapshots(m *Manager) error {
	db,err:=store.LoadDBFor(m.User);if err!=nil{return err}
	if len(db.Snapshots)==0{fmt.Println("No snapshots.");return nil}
	for _,s:=range db.Snapshots{fmt.Printf("%-24s %s %s\n",s.ID,s.CreatedAt.Format(time.RFC3339),s.Path)}
	return nil
}

func RestoreSnapshot(m *Manager,id string) error {
	if m.User{return errors.New("system snapshots are unavailable in --user mode")}
	if err:=m.requirePrivileges("snapshot restore");err!=nil{return err}
	db,err:=store.LoadDBFor(m.User);if err!=nil{return err}
	var snap *model.Snapshot
	for i:=range db.Snapshots{if db.Snapshots[i].ID==id{snap=&db.Snapshots[i];break}}
	if snap==nil{return fmt.Errorf("snapshot %s not found",id)}
	target:=config.SnapshotRoot()
	if target=="/"||filepath.Clean(target)=="/"{return errors.New("refusing live root restore; boot into a rescue environment and set YSPM_SNAPSHOT_ROOT to an unmounted target")}
	btrfs,err:=exec.LookPath("btrfs");if err!=nil{return err}
	if _,err:=os.Stat(snap.Path);err!=nil{return fmt.Errorf("snapshot path is missing: %w",err)}
	backup := target + ".yspm-before-restore"
	if _, err := os.Stat(backup); err == nil {
		return fmt.Errorf("restore backup already exists: %s", backup)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check restore backup path: %w", err)
	}
	if err := os.Rename(target, backup); err != nil {
		return fmt.Errorf("rename target for restore: %w", err)
	}
	if out, err := exec.Command(btrfs, "subvolume", "snapshot", snap.Path, target).CombinedOutput(); err != nil {
		restoreErr := os.Rename(backup, target)
		restoreFailure := fmt.Errorf("restore snapshot: %w: %s", err, strings.TrimSpace(string(out)))
		if restoreErr != nil {
			return errors.Join(restoreFailure, fmt.Errorf("CRITICAL: restore previous target from %s: %w", backup, restoreErr))
		}
		return restoreFailure
	}
	if err := exec.Command(btrfs, "subvolume", "delete", backup).Run(); err != nil {
		return fmt.Errorf("snapshot restored successfully, but deleting previous root %s failed: %w", backup, err)
	}
	return nil
}


 // CreateSnapshotWithLock serializes the standalone snapshot command with
 // package-manager transactions that also persist snapshot metadata.
func CreateSnapshotWithLock(m *Manager) (string, error) {
	var id string
	err := withLock(m.Paths.State, func() error {
		var err error
		id, err = CreateSnapshot(m)
		return err
	})
	return id, err
}
