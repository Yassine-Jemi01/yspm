package manager

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Yassine-Jemi01/yspm/internal/config"

	"github.com/Yassine-Jemi01/yspm/internal/model"
	"github.com/Yassine-Jemi01/yspm/internal/repo"
	"github.com/Yassine-Jemi01/yspm/internal/store"
)

func (m *Manager) InstallLocal(paths []string, yes, autoSnapshot bool) error {
	if len(paths)==0{return fmt.Errorf("install local requires at least one package")}
	if err:=m.requirePrivileges("install");err!=nil{return err}
	return withLock(m.Paths.State,func()error{
		idx,err:=m.index();if err!=nil{return err}
		db,err:=store.LoadDBFor(m.User);if err!=nil{return err}
		if config.IsHardcoreRepository(){if _,err:=m.syncLegacyPackages(&db);err!=nil{return err}}
		if db.Release==""{db.Release=idx.Release;db.ABI=idx.ABI}
		local:=make([]model.Package,0,len(paths))
		for _,name:=range paths{
			abs,err:=filepath.Abs(name);if err!=nil{return err}
			info,err:=os.Stat(abs);if err!=nil{return err}
			if info.IsDir(){return fmt.Errorf("local package path is a directory: %s",name)}
			p,err:=repo.ReadPackageMetadata(abs)
			legacy:=false
			if err!=nil {
				h,he:=repo.InspectHardcoreArchive(abs);if he!=nil{return fmt.Errorf("read package %s: %w",name,he)}
				p=h.Package
				legacy=true
			}
			if p.Format==""{p.Format=repo.PackageFormat}
			if p.Kind==""{p.Kind="system"}
			if p.OS==""{p.OS="linux"}
			if p.Architecture==""{p.Architecture=m.arch}
			if p.ABI==""{p.ABI=idx.ABI}
			if p.SHA256==""{p.SHA256,err=repo.PackageSHA256(abs);if err!=nil{return err}}
			p.URL="file://"+filepath.ToSlash(abs)
			if legacy { p.Format=repo.HardcorePackageFormat }
			local=append(local,p)
			idx.Packages=append(idx.Packages,p)
		}
		legacyCount:=0
		for _,p:=range local{if p.Format==repo.HardcorePackageFormat{legacyCount++}}
		if legacyCount>0&&legacyCount<len(local){return fmt.Errorf("do not mix HardcoreLinux legacy archives and .yspkg archives in one transaction")}
		for _,p:=range local{if idx.ABI!=""&&p.ABI!=""&&p.ABI!=idx.ABI{return fmt.Errorf("package %s targets ABI %s, installed system uses %s",p.Name,p.ABI,idx.ABI)}}
		var requested []string
		for _,p:=range local{requested=append(requested,p.Name)}
		var plan resolvedPlan
		if legacyCount>0 {
			if err:=legacyDependenciesSatisfied(local,db);err!=nil{return err}
			plan=resolvedPlan{Packages:local,Requested:requested}
		} else {
			plan,err=m.resolve(idx,db,requested,false);if err!=nil{return err}
		}
		if !yes{printInstallPlan(plan,db);if !confirm("Continue? [y/N] "){fmt.Println("Aborted.");return nil}}
		snapshotID:="";if autoSnapshot&&!m.User{snapshotID,_=CreateSnapshot(m)}
		tx:=m.startTransaction("install-local",requested,snapshotID)
		staged,err:=m.prepare(plan.Packages);if err!=nil{return m.finishFailed(tx,err)};defer cleanupStaged(staged)
		if err:=m.validateConflicts(staged,db);err!=nil{return m.finishFailed(tx,err)}
		rb:=&transactionRollback{}
		for _,sp:=range staged{
			if err:=m.runScript(sp,"preinstall");err!=nil{rb.rollback();return m.finishFailed(tx,err)}
			if err:=m.commitPackage(sp,db,rb);err!=nil{rb.rollback();return m.finishFailed(tx,err)}
			hook:="postinstall";if sp.HardcoreLegacy{hook="install"}
			if err:=m.runScript(sp,hook);err!=nil{rb.rollback();return m.finishFailed(tx,err)}
			if err:=ApplyServices(m,sp.Pkg);err!=nil{rb.rollback();return m.finishFailed(tx,err)};if err:=ApplyTriggers(m,sp.Pkg);err!=nil{rb.rollback();return m.finishFailed(tx,err)}
			ip:=m.installedFromStage(sp);ip.Explicit=containsName(requested,sp.Pkg.Name);db.Packages[sp.Pkg.Name]=ip
			for _,old:=range sp.Pkg.Replaces{if _,ok:=db.Packages[old];ok{delete(db.Packages,old)}}
		}
		if err:=store.SaveDBFor(m.User,db);err!=nil{rb.rollback();return m.finishFailed(tx,err)}
		rb.finalize();return m.finishSuccess(tx)
	})
}
