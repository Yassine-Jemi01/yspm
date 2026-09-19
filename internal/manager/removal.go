package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Yassine-Jemi01/yspm/internal/model"
)

func RemovePackageFiles(m *Manager, p model.InstalledPackage, rb *transactionRollback) error {
	for _,e:=range p.Manifest{
		if e.Type=="dir"{continue}
		rel:=filepath.Clean(filepath.FromSlash(e.Path))
		if filepath.IsAbs(rel)||rel==".."||strings.HasPrefix(rel,".."+string(os.PathSeparator)){return fmt.Errorf("unsafe installed path %q",e.Path)}
		target:=filepath.Join(m.Paths.Root,rel)
		if !within(m.Paths.Root,target){return fmt.Errorf("installed path escapes root: %s",e.Path)}
		info,err:=os.Lstat(target)
		if os.IsNotExist(err){continue}
		if err!=nil{return err}
		if isInstalledConfig(p,e.Path){
			old:=p.ConfigHashes[e.Path]
			if old!=""&&hashPath(target)!=old{
				save:=target+".yspm-save"
				if err:=os.RemoveAll(save);err!=nil{return err}
				if err:=os.Rename(target,save);err!=nil{return err}
				rb.entries=append(rb.entries,rollbackEntry{target:target,backup:save})
				continue
			}
		}
		backup:=filepath.Join(m.Paths.Staging,"rollback-remove",p.Name,rel)
		if err:=os.MkdirAll(filepath.Dir(backup),0o755);err!=nil{return err}
		_ = info
		if err:=os.Rename(target,backup);err!=nil{
			if err:=copyNode(target,backup);err!=nil{return err}
			if err:=os.RemoveAll(target);err!=nil{return err}
		}
		rb.entries=append(rb.entries,rollbackEntry{target:target,backup:backup})
	}
	return nil
}

func isInstalledConfig(p model.InstalledPackage,path string) bool {
	_,ok:=p.ConfigHashes[path]
	return ok
}
