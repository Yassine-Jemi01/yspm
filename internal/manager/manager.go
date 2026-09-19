package manager

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Yassine-Jemi01/yspm/internal/config"
	"github.com/Yassine-Jemi01/yspm/internal/model"
	"github.com/Yassine-Jemi01/yspm/internal/repo"
	"github.com/Yassine-Jemi01/yspm/internal/store"
)

type Manager struct {
	User       bool
	Paths      config.Paths
	repository string
	arch       string
}

type dependencyRequest struct { Name, Op, Version, Arch string }
type resolvedPlan struct { Packages []model.Package; Requested []string }
type stagedPackage struct {
	Pkg        model.Package
	Archive    string
	Stage      string
	Manifest   []model.FileEntry
	Scripts    map[string]string
	Legacy     bool
	Command    string
	InstallDir string
}

func New(user bool, arch string) *Manager {
	p, _ := config.NewPaths(user)
	if arch == "" { arch = config.HostArch() } else { arch = config.NormalizeArch(arch) }
	return &Manager{User:user, Paths:p, repository:config.RepositoryURL(), arch:arch}
}

func (m *Manager) requirePrivileges(op string) error {
	if m.User { return nil }
	if os.Geteuid() != 0 && m.Paths.Root == "/" {
		return fmt.Errorf("%s requires root privileges; rerun with sudo or use --user", op)
	}
	return nil
}

func (m *Manager) index() (model.Index, error) {
	if idx, err := repo.LoadCachedIndex(); err == nil {
		if err := repo.ValidateStableIndex(idx); err == nil { return idx, nil }
	}
	if err := m.Update(); err != nil { return model.Index{}, err }
	idx, err := repo.LoadCachedIndex()
	if err != nil { return model.Index{}, err }
	return idx, repo.ValidateStableIndex(idx)
}

func (m *Manager) Update() error {
	fmt.Printf("Updating stable repository...\n  %s\n", m.repository)
	idx, err := repo.FetchIndex(m.repository)
	if err != nil { return err }
	if err := repo.ValidateStableIndex(idx); err != nil { return err }
	if err := repo.CacheIndex(idx); err != nil { return err }
	fmt.Printf("Release %s (%s) — %d packages — ABI %s.\n", idx.Release, idx.Channel, len(idx.Packages), valueOr(idx.ABI,"none"))
	return nil
}

func (m *Manager) Search(query string) ([]model.Package,error) {
	idx,err:=m.index();if err!=nil{return nil,err}
	query=strings.ToLower(query); seen:=map[string]model.Package{}
	for _,p:=range idx.Packages{
		if !m.packageUsable(p,m.arch,idx){continue}
		if strings.Contains(strings.ToLower(p.Name),query)||strings.Contains(strings.ToLower(p.Description),query){
			if old,ok:=seen[p.Name];!ok||compareVersion(p.Version,old.Version)>0{seen[p.Name]=p}
		}
	}
	out:=make([]model.Package,0,len(seen));for _,p:=range seen{out=append(out,p)}
	sort.Slice(out,func(i,j int)bool{return out[i].Name<out[j].Name});return out,nil
}

func (m *Manager) Info(name string)(model.Package,error){
	idx,err:=m.index();if err!=nil{return model.Package{},err}
	return m.findSatisfying(name,"",idx)
}

func (m *Manager) List(upgradable,explicit bool)([]model.InstalledPackage,error){
	db,err:=store.LoadDBFor(m.User);if err!=nil{return nil,err}
	var idx model.Index
	if upgradable{idx,err=m.index();if err!=nil{return nil,err}}
	out:=make([]model.InstalledPackage,0,len(db.Packages))
	for _,p:=range db.Packages{
		if explicit&&!p.Explicit{continue}
		if upgradable{
			candidate,e:=m.findSatisfying(p.Name,"",idx)
			if e!=nil||compareVersion(candidate.Version,p.Version)<=0{continue}
		}
		out=append(out,p)
	}
	sort.Slice(out,func(i,j int)bool{return out[i].Name<out[j].Name});return out,nil
}

func (m *Manager) Depends(name string) error {
	idx,err:=m.index();if err!=nil{return err}
	return m.printDependencyTree(name,idx,"",map[string]bool{})
}
func (m *Manager) printDependencyTree(name string,idx model.Index,prefix string,seen map[string]bool) error {
	p,err:=m.findSatisfying(name,"",idx);if err!=nil{return err}
	fmt.Printf("%s%s %s [%s]\n",prefix,p.Name,p.Version,valueOr(p.ABI,"no-abi"))
	if seen[p.Name]{return nil};seen[p.Name]=true
	for _,dep:=range p.Dependencies{
		r:=parseDependency(string(dep))
		child,e:=m.findSatisfying(r.Name,r.Op+r.Version,idx)
		if e!=nil{fmt.Printf("%s  ? %s (%s)\n",prefix,dep,e);continue}
		_ = m.printDependencyTree(child.Name,idx,prefix+"  ",seen)
	}
	return nil
}

func (m *Manager) Why(target string) error {
	db,err:=store.LoadDBFor(m.User);if err!=nil{return err}
	if p,ok:=db.Packages[target];ok&&p.Explicit{fmt.Printf("%s is explicitly installed.\n",target);return nil}
	for name,p:=range db.Packages{
		if name==target||!p.Explicit{continue}
		if chain,ok:=dependencyChain(name,target,db.Packages,map[string]bool{});ok{
			fmt.Printf("%s is required by %s via %s\n",target,name,strings.Join(chain," -> "));return nil
		}
	}
	fmt.Printf("No installed package requires %s.\n",target);return nil
}

func dependencyChain(start,target string,pkgs map[string]model.InstalledPackage,seen map[string]bool)([]string,bool){
	if seen[start]{return nil,false};seen[start]=true
	for _,d:=range pkgs[start].Dependencies{
		r:=parseDependency(string(d))
		if r.Name==target{return []string{start,target},true}
		if _,ok:=pkgs[r.Name];ok{if c,ok:=dependencyChain(r.Name,target,pkgs,seen);ok{return append([]string{start},c...),true}}
	}
	return nil,false
}

func (m *Manager) Explain(name string) error {
	p,err:=m.Info(name);if err!=nil{return err}
	fmt.Printf("Package: %s %s\nKind: %s\nABI: %s\nArchitecture: %s\n",p.Name,p.Version,p.Kind,valueOr(p.ABI,"none"),p.Architecture)
	printDeps("Dependencies",p.Dependencies);printDeps("Recommends",p.Recommends);printDeps("Suggests",p.Suggests)
	if len(p.Conflicts)>0{fmt.Printf("Conflicts: %s\n",strings.Join(p.Conflicts,", "))}
	if len(p.Provides)>0{fmt.Printf("Provides: %s\n",strings.Join(p.Provides,", "))}
	if len(p.SharedProvides)>0{fmt.Printf("Shared provides: %s\n",strings.Join(p.SharedProvides,", "))}
	if len(p.SharedRequires)>0{fmt.Printf("Shared requires: %s\n",strings.Join(p.SharedRequires,", "))}
	return nil
}

func printDeps(label string, ds []model.Dependency){if len(ds)==0{fmt.Printf("%s: none\n",label);return};out:=make([]string,len(ds));for i,d:=range ds{out[i]=string(d)};fmt.Printf("%s: %s\n",label,strings.Join(out,", "))}

func (m *Manager) InstallMany(names []string, yes, autoSnapshot bool) error {
	return m.runTransaction("install",names,yes,false,autoSnapshot)
}

func (m *Manager) Upgrade(yes,autoSnapshot bool) error {
	return m.runTransaction("upgrade",nil,yes,true,autoSnapshot)
}

func (m *Manager) runTransaction(action string,requested []string,yes,upgrade,autoSnapshot bool) error {
	if err:=m.requirePrivileges(action);err!=nil{return err}
	return withLock(m.Paths.State,func() error{
		idx,err:=m.index();if err!=nil{return err}
		db,err:=store.LoadDBFor(m.User);if err!=nil{return err}
		if db.Release==""{db.Release=idx.Release;db.ABI=idx.ABI}
		if db.Release!=idx.Release{return fmt.Errorf("installed release %s differs from repository %s; use explicit release upgrade",db.Release,idx.Release)}
		if db.ABI==""{db.ABI=idx.ABI}
		plan,err:=m.resolve(idx,db,requested,upgrade);if err!=nil{return err}
		if len(plan.Packages)==0{fmt.Println("Nothing to do.");return store.SaveDBFor(m.User,db)}
		if !yes{printInstallPlan(plan,db);if !confirm("Continue? [y/N] "){fmt.Println("Aborted.");return nil}}
		snapshotID:=""
		if autoSnapshot&&!m.User{
			snapshotID,_=CreateSnapshot(m)
		}
		tx:=m.startTransaction(action,namesFromPackages(plan.Packages),snapshotID)
		staged,err:=m.prepare(plan.Packages)
		if err!=nil{return m.finishFailed(tx,err)}
		defer cleanupStaged(staged)
		if err:=m.validateConflicts(staged,db);err!=nil{return m.finishFailed(tx,err)}
		rollback:=&transactionRollback{}
		for _,sp:=range staged{
			if err:=m.runScript(sp,"preinstall");err!=nil{rollback.rollback();return m.finishFailed(tx,err)}
			if err:=m.commitPackage(sp,db,rollback);err!=nil{rollback.rollback();return m.finishFailed(tx,err)}
			if err:=m.runScript(sp,"postinstall");err!=nil{return m.finishFailed(tx,err)}
			if err:=ApplyServices(m,sp.Pkg);err!=nil{return m.finishFailed(tx,err)}
			installed:=m.installedFromStage(sp)
			installed.Explicit=containsName(plan.Requested,sp.Pkg.Name)||installed.Explicit
			db.Packages[sp.Pkg.Name]=installed
		}
		if err:=store.SaveDBFor(m.User,db);err!=nil{rollback.rollback();return m.finishFailed(tx,err)}
		rollback.finalize()
		return m.finishSuccess(tx)
	})
}

func (m *Manager) resolve(idx model.Index,db model.Database,requested []string,upgrade bool)(resolvedPlan,error){
	chosen:=map[string]model.Package{};constraints:=map[string][]string{}
	queue:=append([]string(nil),requested...)
	if upgrade{for name,installed:=range db.Packages{candidate,e:=m.findSatisfying(name,"",idx);if e==nil&&(compareVersion(candidate.Version,installed.Version)>0||candidate.Revision>installed.Revision){queue=append(queue,name)}}}
	seen:=map[string]bool{}
	for len(queue)>0{
		expr:=queue[0];queue=queue[1:]
		r:=parseDependency(expr)
		key:=r.Name+"@"+r.Arch;constraints[key]=append(constraints[key],r.Op+r.Version)
		if seen[expr]{continue};seen[expr]=true
		p,e:=m.chooseWithConstraints(r, constraints[key], idx);if e!=nil{return resolvedPlan{},fmt.Errorf("resolve %q: %w",expr,e)}
		chosen[p.Name+":"+p.Architecture]=p
		for _,d:=range p.Dependencies{queue=append(queue,string(d))}
		for _,d:=range p.SharedRequires{queue=append(queue,d)}
	}
	var pkgs []model.Package
	for _,p:=range chosen{
		current,ok:=db.Packages[p.Name]
		if !ok||compareVersion(p.Version,current.Version)>0||p.Revision>current.Revision{pkgs=append(pkgs,p)}
	}
	sort.Slice(pkgs,func(i,j int)bool{return pkgs[i].Name<pkgs[j].Name})
	for _,p:=range pkgs{
		for _,c:=range p.Conflicts{
			if _,ok:=db.Packages[c];ok&&!containsName(requested,c)&&!containsString(p.Replaces,c){return resolvedPlan{},fmt.Errorf("%s conflicts with installed package %s",p.Name,c)}
			for _,q:=range pkgs{if q.Name==c{return resolvedPlan{},fmt.Errorf("%s conflicts with %s in transaction",p.Name,c)}}
		}
	}
	return resolvedPlan{Packages:pkgs,Requested:requested},nil
}

func (m *Manager) chooseWithConstraints(r dependencyRequest,constraints []string,idx model.Index)(model.Package,error){
	for _,alt:=range strings.Split(r.Name,"|"){
		alt=strings.TrimSpace(alt)
		if alt==""{continue}
		var candidates []model.Package
		for _,p:=range idx.Packages{
			if !m.packageUsable(p,firstNonEmpty(r.Arch,m.arch),idx){continue}
			if !matchesNameOrProvide(p,alt){continue}
			if !allSatisfied(p.Version,constraints){continue}
			if p.ABI!=""&&idx.ABI!=""&&p.ABI!=idx.ABI{continue}
			candidates=append(candidates,p)
		}
		if len(candidates)>0{
			sort.Slice(candidates,func(i,j int)bool{
				c:=compareVersion(candidates[i].Version,candidates[j].Version)
				if c==0{return candidates[i].Revision>candidates[j].Revision};return c>0
			})
			return candidates[0],nil
		}
	}
	return model.Package{},fmt.Errorf("no candidate satisfies %s (%s)",r.Name,strings.Join(constraints,", "))
}

func (m *Manager) findSatisfying(name,constraint string,idx model.Index)(model.Package,error){
	r:=parseDependency(name);if constraint!=""{r.Op,r.Version=parseConstraint(constraint)}
	return m.chooseWithConstraints(r,[]string{r.Op+r.Version},idx)
}

func (m *Manager) packageUsable(p model.Package,arch string,idx model.Index)bool{
	if p.OS!=""&&p.OS!="linux"{return false}
	if !repo.SupportsArchitecture(p,arch){
		if !config.ForeignArchitectures()[config.NormalizeArch(p.Architecture)]{return false}
	}
	if idx.ABI!=""&&p.ABI!=""&&p.ABI!=idx.ABI{return false}
	return true
}

func matchesNameOrProvide(p model.Package,want string)bool{
	if p.Name==want{return true}
	for _,x:=range p.Provides{if x==want{return true}}
	for _,x:=range p.SharedProvides{if x==want{return true}}
	return false
}

func parseDependency(s string)dependencyRequest{
	s=strings.TrimSpace(s);arch:=""
	if i:=strings.LastIndex(s,":");i>0&&!strings.Contains(s[i+1:],"/"){arch=config.NormalizeArch(s[i+1:]);s=s[:i]}
	op,ver:=parseConstraint(s);name:=s
	if op!=""{
		idx:=strings.Index(s,op);name=strings.TrimSpace(s[:idx]);ver=strings.TrimSpace(s[idx+len(op):])
	}
	return dependencyRequest{Name:name,Op:op,Version:ver,Arch:arch}
}
func parseConstraint(s string)(string,string){
	for _,op:=range []string{"!=",">=","<=","=","<",">"}{if i:=strings.Index(s,op);i>0{return op,strings.TrimSpace(s[i+len(op):])}}
	return "",""
}
func allSatisfied(v string,cs []string)bool{for _,c:=range cs{if op,ver:=parseConstraint("x"+c);op!=""&&!satisfies(v,op+ver){return false}};return true}
func satisfies(v,c string)bool{if c==""{return true};op,ver:=parseConstraint("x"+c);switch op{case "=":return compareVersion(v,ver)==0;case "!=":return compareVersion(v,ver)!=0;case ">":return compareVersion(v,ver)>0;case ">=":return compareVersion(v,ver)>=0;case "<":return compareVersion(v,ver)<0;case "<=":return compareVersion(v,ver)<=0};return true}

func (m *Manager) prepare(pkgs []model.Package)([]stagedPackage,error){
	if err:=os.MkdirAll(m.Paths.Cache,0o755);err!=nil{return nil,err};if err:=os.MkdirAll(m.Paths.Staging,0o755);err!=nil{return nil,err}
	sem:=make(chan struct{},4);results:=make([]stagedPackage,len(pkgs));var wg sync.WaitGroup;errCh:=make(chan error,len(pkgs))
	for i,p:=range pkgs{i,p=i,p;wg.Add(1);go func(){defer wg.Done();sem<-struct{}{};defer func(){<-sem}()
		sp:=stagedPackage{Pkg:p,Scripts:map[string]string{}};archive:=filepath.Join(m.Paths.Cache,packageFilename(p))
		if p.URL==""&&p.Kind!="meta"{errCh<-fmt.Errorf("package %s has no URL",p.Name);return}
		valid:=false;if _,err:=os.Stat(archive);err==nil&&p.SHA256!=""{valid=repo.VerifySHA256(archive,p.SHA256)==nil}
		if p.Kind!="meta"&&!valid{if err:=repo.Download(p.URL,archive);err!=nil{errCh<-fmt.Errorf("download %s: %w",p.Name,err);return};if err:=repo.VerifySHA256(archive,p.SHA256);err!=nil{_ = os.Remove(archive);errCh<-fmt.Errorf("verify %s: %w",p.Name,err);return}}
		stage,err:=os.MkdirTemp(m.Paths.Staging,p.Name+"-*");if err!=nil{errCh<-err;return};sp.Stage=stage;sp.Archive=archive
		if p.Kind!="meta"{
			if p.Format=="yspkg"||strings.HasSuffix(strings.ToLower(p.URL),".yspkg"){
				data,e:=repo.ReadPackageData(archive);if e!=nil{errCh<-e;return};sp.Scripts=data.Scripts
				sp.Manifest,e=repo.ListPackageFiles(archive);if e!=nil{errCh<-e;return}
				if err:=repo.ExtractPackage(archive,stage);err!=nil{errCh<-err;return}
			}else{
				rawStage:=stage
				if p.Kind=="appimage"||p.Format=="appimage"{
					name:=filepath.Base(p.Entry);if name==""||name=="."{name=p.Name+".AppImage"}
					if err:=copyNode(archive,filepath.Join(rawStage,name));err!=nil{errCh<-err;return}
				}else{
					if err:=repo.ExtractArchive(archive,p.Format,rawStage);err!=nil{errCh<-err;return}
				}
				entryRel:=""
				if p.Entry!=""&&! (p.Kind=="appimage"||p.Format=="appimage"){
					entry,err:=repo.FindEntry(rawStage,p.Entry);if err!=nil{errCh<-fmt.Errorf("locate executable for %s: %w",p.Name,err);return}
					entryRel,_=filepath.Rel(rawStage,entry)
				}else if p.Kind=="appimage"||p.Format=="appimage"{entryRel=filepath.Base(p.Entry);if entryRel==""||entryRel=="."{entryRel=p.Name+".AppImage"}}
				rootTree:=filepath.Join(rawStage,"root")
				if err:=os.MkdirAll(filepath.Join(rootTree,"opt","yspm","packages",p.Name,p.Version),0o755);err!=nil{errCh<-err;return}
				optDir:=filepath.Join(rootTree,"opt","yspm","packages",p.Name,p.Version)
				entries,e:=os.ReadDir(rawStage);if e!=nil{errCh<-e;return}
				for _,en:=range entries{if en.Name()=="root"{continue};if err:=os.Rename(filepath.Join(rawStage,en.Name()),filepath.Join(optDir,en.Name()));err!=nil{errCh<-err;return}}
				command:=p.Command;if command==""{command=p.Name}
				if entryRel!=""{
					link:=filepath.Join(rootTree,"usr","local","bin",command);if err:=os.MkdirAll(filepath.Dir(link),0o755);err!=nil{errCh<-err;return}
					target:=filepath.ToSlash("/opt/yspm/packages/"+p.Name+"/"+p.Version+"/"+entryRel);if err:=os.Symlink(target,link);err!=nil{errCh<-err;return}
				}
				if p.Desktop||p.Kind=="app"||p.Kind=="appimage"{
					desktopDir:=filepath.Join(rootTree,"usr","share","applications");if err:=os.MkdirAll(desktopDir,0o755);err!=nil{errCh<-err;return}
					dname:=p.DesktopName;if dname==""{dname=p.Name}
					cats:=strings.Join(p.Categories,";");if cats==""{cats="Utility;"}
					desktop:=fmt.Sprintf("[Desktop Entry]\\nType=Application\\nName=%s\\nComment=%s\\nExec=%s %%U\\nTerminal=false\\nCategories=%s\\n",escapeDesktopText(dname),escapeDesktopText(p.Description),command,cats)
					if err:=os.WriteFile(filepath.Join(desktopDir,p.Name+".desktop"),[]byte(desktop),0o644);err!=nil{errCh<-err;return}
				}
				stage=rootTree
				sp.Legacy=true;sp.Command=command;sp.InstallDir=filepath.Join(m.Paths.Root,"opt","yspm","packages",p.Name,p.Version)
			}
		}
		sp.Manifest,err=repo.Manifest(sp.Stage);if err!=nil{errCh<-err;return}
		results[i]=sp
	}()}
	wg.Wait();close(errCh);for e:=range errCh{cleanupStaged(results);return nil,e};return results,nil
}

func (m *Manager) validateConflicts(staged []stagedPackage,db model.Database)error{
	owners:=map[string]string{}
	for name,p:=range db.Packages{for _,f:=range p.Files{owners[f]=name};for _,e:=range p.Manifest{if e.Path!=""{owners[e.Path]=name}}}
	for _,sp:=range staged{
		for _,e:=range sp.Manifest{
			if e.Type=="dir"{continue};if owner,ok:=owners[e.Path];ok&&owner!=sp.Pkg.Name&&!containsString(sp.Pkg.Replaces,owner){return fmt.Errorf("file conflict: %s is owned by %s",e.Path,owner)}
		}
	}
	return nil
}

type rollbackEntry struct{target,backup string;created bool}
type transactionRollback struct{entries []rollbackEntry}
func(r *transactionRollback)rollback(){for i:=len(r.entries)-1;i>=0;i--{e:=r.entries[i];_ = os.RemoveAll(e.target);if e.backup!=""{_ = os.MkdirAll(filepath.Dir(e.target),0o755);_ = os.Rename(e.backup,e.target)}}}
func(r *transactionRollback)finalize(){for _,e:=range r.entries{if e.backup!=""{_ = os.RemoveAll(e.backup)}}}

func (m *Manager) commitPackage(sp stagedPackage,db model.Database,rb *transactionRollback)error{
	for _,e:=range sp.Manifest{
		if e.Type=="dir"{continue}
		rel:=filepath.Clean(filepath.FromSlash(e.Path));if filepath.IsAbs(rel)||rel==".."||strings.HasPrefix(rel,".."+string(os.PathSeparator)){return fmt.Errorf("unsafe package path %q",e.Path)}
		src:=filepath.Join(sp.Stage,rel);target:=filepath.Join(m.Paths.Root,rel)
		if !within(m.Paths.Root,target){return fmt.Errorf("package path escapes root: %s",e.Path)}
		if err:=os.MkdirAll(filepath.Dir(target),0o755);err!=nil{return err}
		if existing,err:=os.Lstat(target);err==nil{
			if isConfig(sp.Pkg,e.Path){
				if old,ok:=findPreviousConfigHash(db,e.Path);ok&&hashPath(target)==old{
					backup:=filepath.Join(sp.Stage,".backup",rel);if err:=os.MkdirAll(filepath.Dir(backup),0o755);err!=nil{return err};if err:=os.Rename(target,backup);err!=nil{return err};rb.entries=append(rb.entries,rollbackEntry{target:target,backup:backup})
				}else{
					dist:=target+".yspm-dist";if err:=os.RemoveAll(dist);err!=nil{return err};if err:=copyNode(src,dist);err!=nil{return err};continue
				}
			}else{
				_ = existing
				backup:=filepath.Join(sp.Stage,".backup",rel);if err:=os.MkdirAll(filepath.Dir(backup),0o755);err!=nil{return err};if err:=os.Rename(target,backup);err!=nil{return err};rb.entries=append(rb.entries,rollbackEntry{target:target,backup:backup})
			}
		}else if !os.IsNotExist(err){return err}
		if err:=os.Rename(src,target);err!=nil{
			if err:=copyNode(src,target);err!=nil{return err};_ = os.RemoveAll(src)
		}
		rb.entries=append(rb.entries,rollbackEntry{target:target})
	}
	return nil
}

func copyNode(src,dst string)error{
	info,err:=os.Lstat(src);if err!=nil{return err}
	if info.Mode()&os.ModeSymlink!=0{target,err:=os.Readlink(src);if err!=nil{return err};_ = os.RemoveAll(dst);return os.Symlink(target,dst)}
	if info.IsDir(){if err:=os.MkdirAll(dst,info.Mode().Perm());err!=nil{return err};return nil}
	in,err:=os.Open(src);if err!=nil{return err};defer in.Close()
	out,err:=os.OpenFile(dst,os.O_CREATE|os.O_TRUNC|os.O_WRONLY,info.Mode().Perm());if err!=nil{return err}
	if _,err:=io.Copy(out,in);err!=nil{_ = out.Close();return err};return out.Close()
}

func (m *Manager) installedFromStage(sp stagedPackage)model.InstalledPackage{
	hashes:=map[string]string{};configs:=map[string]string{};files:=[]string{}
	for _,e:=range sp.Manifest{if e.Type!="dir"{files=append(files,e.Path);if e.Type=="file"{hashes[e.Path]=e.SHA256;if isConfig(sp.Pkg,e.Path){configs[e.Path]=e.SHA256}}}}
	return model.InstalledPackage{Name:sp.Pkg.Name,Version:sp.Pkg.Version,Revision:sp.Pkg.Revision,Kind:sp.Pkg.Kind,Architecture:sp.Pkg.Architecture,ABI:sp.Pkg.ABI,Dependencies:append([]model.Dependency(nil),sp.Pkg.Dependencies...),InstallDir:sp.InstallDir,Command:sp.Command,Files:files,Manifest:sp.Manifest,FileHashes:hashes,ConfigHashes:configs,Checksum:sp.Pkg.SHA256,Explicit:false,Services:sp.Pkg.Services,Hooks:copyHooks(sp.Scripts),InstalledAt:time.Now()}
}

func isConfig(p model.Package,path string)bool{for _,x:=range p.ConfigFiles{if filepath.ToSlash(x)==filepath.ToSlash(path){return true}};return false}
func findPreviousConfigHash(db model.Database,path string)(string,bool){for _,p:=range db.Packages{if h,ok:=p.ConfigHashes[path];ok{return h,true}};return "",false}
func hashPath(path string)string{f,err:=os.Open(path);if err!=nil{return ""};defer f.Close();h:=sha256.New();_,_=io.Copy(h,f);return hex.EncodeToString(h.Sum(nil))}

func (m *Manager) runScript(sp stagedPackage,name string)error{
	script:=strings.TrimSpace(sp.Scripts[name]);if script==""{return nil}
	tmp:=filepath.Join(sp.Stage,".script-"+name);if err:=os.WriteFile(tmp,[]byte(script),0o700);err!=nil{return err};defer os.Remove(tmp)
	cmd:=exec.Command("/bin/sh",tmp);cmd.Env=append(os.Environ(),"YSPM_ROOT="+m.Paths.Root,"YSPM_PACKAGE="+sp.Pkg.Name,"YSPM_VERSION="+sp.Pkg.Version)
	cmd.Dir=m.Paths.Root
	out,err:=cmd.CombinedOutput();if err!=nil{return fmt.Errorf("%s hook for %s failed: %w: %s",name,sp.Pkg.Name,err,strings.TrimSpace(string(out)))};return nil
}

func (m *Manager) RemoveMany(names []string,yes,autoSnapshot bool)error{
	if err:=m.requirePrivileges("remove");err!=nil{return err}
	return withLock(m.Paths.State,func()error{
		db,err:=store.LoadDBFor(m.User);if err!=nil{return err}
		idx,err:=m.index();if err!=nil{return err}
		if db.Release!=""&&db.Release!=idx.Release{return fmt.Errorf("installed release %s differs from repository %s",db.Release,idx.Release)}
		for _,name:=range names{if _,ok:=db.Packages[name];!ok{return fmt.Errorf("package %q is not installed",name)}}
		for _,name:=range names{
			for otherName,other:=range db.Packages{if otherName==name||containsName(names,otherName){continue};if dependsOn(other.Dependencies,name){return fmt.Errorf("cannot remove %q: installed package %q depends on it",name,otherName)}}
		}
		if !yes{fmt.Printf("Remove %s? [y/N] ",strings.Join(names,", "));if !confirm(""){fmt.Println("Aborted.");return nil}}
		if autoSnapshot&&!m.User{_,_=CreateSnapshot(m)}
		tx:=m.startTransaction("remove",names,"")
		rb:=&transactionRollback{}
		for _,name:=range names{
			p:=db.Packages[name]
			if err:=m.runInstalledHook(p,"preremove");err!=nil{return m.finishFailed(tx,err)}
			if err:=RemovePackageFiles(m,p,rb);err!=nil{rb.rollback();return m.finishFailed(tx,err)}
			if err:=DisableServices(m,p.Services);err!=nil{rb.rollback();return m.finishFailed(tx,err)}
			if err:=m.runInstalledHook(p,"postremove");err!=nil{rb.rollback();return m.finishFailed(tx,err)}
			delete(db.Packages,name)
		}
		if err:=store.SaveDBFor(m.User,db);err!=nil{rb.rollback();return m.finishFailed(tx,err)}
				rb.finalize();return m.finishSuccess(tx)
	})
}

func (m *Manager) runInstalledHook(p model.InstalledPackage,name string) error {
	script := strings.TrimSpace(p.Hooks[name])
	if script == "" { return nil }
	tmpDir := filepath.Join(m.Paths.State, "hooks")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil { return err }
	f, err := os.CreateTemp(tmpDir, "hook-*")
	if err != nil { return err }
	path := f.Name()
	defer os.Remove(path)
	if _, err := f.WriteString(script); err != nil { _ = f.Close(); return err }
	if err := f.Chmod(0o700); err != nil { _ = f.Close(); return err }
	if err := f.Close(); err != nil { return err }
	cmd := exec.Command("/bin/sh", path)
	cmd.Env = append(os.Environ(), "YSPM_ROOT="+m.Paths.Root, "YSPM_PACKAGE="+p.Name, "YSPM_VERSION="+p.Version)
	cmd.Dir = m.Paths.Root
	out, err := cmd.CombinedOutput()
	if err != nil { return fmt.Errorf("%s hook for %s failed: %w: %s", name, p.Name, err, strings.TrimSpace(string(out))) }
	return nil
}
func (m *Manager) runInstalledHookByName(name,hook string) error {
	db, err := store.LoadDBFor(m.User)
	if err != nil { return err }
	p, ok := db.Packages[name]
	if !ok { return nil }
	return m.runInstalledHook(p, hook)
}
func copyHooks(in map[string]string) map[string]string {
	if len(in)==0 { return nil }
	out:=make(map[string]string,len(in))
	for k,v:=range in { out[k]=v }
	return out
}

func (m *Manager) Autoremove(yes,autoSnapshot bool)error{
	for{db,err:=store.LoadDBFor(m.User);if err!=nil{return err};var c []string
		for name,p:=range db.Packages{if p.Explicit{continue};needed:=false;for otherName,other:=range db.Packages{if otherName!=name&&dependsOn(other.Dependencies,name){needed=true;break}};if !needed{c=append(c,name)}}
		if len(c)==0{fmt.Println("No automatically installed packages can be removed.");return nil};sort.Strings(c);if err:=m.RemoveMany(c,yes,autoSnapshot);err!=nil{return err};if !yes{return nil}}
}

func (m *Manager) Clean()error{if err:=os.RemoveAll(m.Paths.Cache);err!=nil{return err};fmt.Println("Package cache cleaned.");return nil}

func (m *Manager) Check()error{
	db,err:=store.LoadDBFor(m.User);if err!=nil{return err};problems:=0
	for name,p:=range db.Packages{for _,e:=range p.Manifest{if e.Type=="dir"{continue};target:=filepath.Join(m.Paths.Root,filepath.FromSlash(e.Path));if _,err:=os.Lstat(target);err!=nil{fmt.Printf("%s: missing %s\n",name,e.Path);problems++}}}
	if problems>0{return fmt.Errorf("integrity check found %d problem(s)",problems)};fmt.Println("Package database looks consistent.");return nil
}

func (m *Manager) History()error{db,err:=store.LoadDBFor(m.User);if err!=nil{return err};for i:=len(db.Transactions)-1;i>=0;i--{t:=db.Transactions[i];fmt.Printf("%-16s %-8s %-10s %s\n",t.ID,t.Status,t.Action,t.StartedAt.Format(time.RFC3339))};return nil}
func (m *Manager) Transaction(id string)error{t,err:=store.GetTransactionFor(m.User,id);if err!=nil{return err};fmt.Printf("ID: %s\nAction: %s\nStatus: %s\nStarted: %s\n",t.ID,t.Action,t.Status,t.StartedAt.Format(time.RFC3339));if !t.FinishedAt.IsZero(){fmt.Printf("Finished: %s\n",t.FinishedAt.Format(time.RFC3339))};if t.Error!=""{fmt.Printf("Error: %s\n",t.Error)};return nil}
func (m *Manager) Release()error{db,err:=store.LoadDBFor(m.User);if err!=nil{return err};idx,err:=m.index();if err!=nil{return err};fmt.Printf("Installed release: %s\nRepository release: %s\nABI: %s\nPackages installed: %d\n",db.Release,idx.Release,valueOr(db.ABI,"none"),len(db.Packages));return nil}

func (m *Manager) UpgradeRelease(release string,yes,autoSnapshot bool)error{
	if err:=m.requirePrivileges("release upgrade");err!=nil{return err}
	return withLock(m.Paths.State,func()error{
		db,err:=store.LoadDBFor(m.User);if err!=nil{return err};if db.Release==release{return errors.New("requested release is already installed")}
		url:=config.ReleaseRepositoryURL(release);fmt.Printf("Loading release %s from %s\n",release,url)
		idx,err:=repo.FetchIndex(url);if err!=nil{return err}
		if err:=repo.ValidateStableIndex(idx);err!=nil{return err}
		if !yes{fmt.Printf("Upgrade release %s -> %s? [y/N] ",db.Release,release);if !confirm(""){return nil}}
		plan,err:=m.resolve(idx,db,nil,true);if err!=nil{return fmt.Errorf("release resolution failed: %w",err)}
		oldRepo:=m.repository;m.repository=url;defer func(){m.repository=oldRepo}()
		tx:=m.startTransaction("release-upgrade",namesFromPackages(plan.Packages),"")
		rb:=&transactionRollback{};if autoSnapshot&&!m.User{_,_=CreateSnapshot(m)}
		staged,err:=m.prepare(plan.Packages);if err!=nil{return m.finishFailed(tx,err)};defer cleanupStaged(staged)
		if err:=m.validateConflicts(staged,db);err!=nil{return m.finishFailed(tx,err)}
		for _,sp:=range staged{if err:=m.commitPackage(sp,db,rb);err!=nil{rb.rollback();return m.finishFailed(tx,err)};db.Packages[sp.Pkg.Name]=m.installedFromStage(sp)}
		db.Release,db.ABI=idx.Release,idx.ABI;if err:=store.SaveDBFor(m.User,db);err!=nil{rb.rollback();return m.finishFailed(tx,err)};rb.finalize();_=repo.CacheIndex(idx);return m.finishSuccess(tx)
	})
}

func (m *Manager) Audit()error{
	db,err:=store.LoadDBFor(m.User);if err!=nil{return err};idx,err:=m.index();if err!=nil{return err}
	found:=0
	for name,p:=range db.Packages{var meta *model.Package;for i:=range idx.Packages{if idx.Packages[i].Name==name{meta=&idx.Packages[i];break}};if meta==nil{continue};for _,v:=range meta.Vulnerabilities{if v.FixedVersion!=""&&compareVersion(p.Version,v.FixedVersion)>=0{continue};fmt.Printf("%s %s: %s %s",name,p.Version,v.ID,valueOr(v.Severity,"unknown"));if v.FixedVersion!=""{fmt.Printf(" fixed in %s",v.FixedVersion)};if v.URL!=""{fmt.Printf(" %s",v.URL)};fmt.Println();found++}}
	if found==0{fmt.Println("No known vulnerabilities in installed package metadata.")};return nil
}

func (m *Manager) Build(root,output,name,version,desc,abi,arch,license,maintainer,scripts string)(model.Package,error){
	p:=model.Package{Name:name,Version:version,Description:desc,ABI:abi,Architecture:arch,License:license,Maintainer:maintainer,Format:"yspkg",Kind:"system",OS:"linux",Revision:1}
	if p.ABI==""{p.ABI=config.DefaultSystemABI};if p.Architecture==""{p.Architecture=m.arch};if p.License==""{p.License="unknown"}
	if err:=repo.BuildPackage(root,output,p,scripts);err!=nil{return model.Package{},err}
	meta,err:=repo.ReadPackageMetadata(output);if err!=nil{return model.Package{},err};return meta,nil
}

func (m *Manager) RepoIndex(dir,output,baseURL,release,abi string)error{_,err:=repo.BuildRepository(dir,output,baseURL,release,abi);return err}
func (m *Manager) RepoSign(index,key,sig string)error{return repo.SignRepositoryIndex(index,key,sig)}
func (m *Manager) GenerateKey(pub,priv string)error{return repo.GenerateKeypair(pub,priv)}

func (m *Manager) startTransaction(action string,packages []string,snapshot string)model.Transaction{
	tx:=model.Transaction{ID:newID(),StartedAt:time.Now(),Action:action,Packages:packages,Status:"running",SnapshotBefore:snapshot};_ = store.AddTransactionFor(m.User,tx);return tx
}
func(m *Manager)finishSuccess(tx model.Transaction)error{tx.Status="success";tx.FinishedAt=time.Now();return store.UpdateTransactionFor(m.User,tx)}
func(m *Manager)finishFailed(tx model.Transaction,err error)error{tx.Status="failed";tx.Error=err.Error();tx.FinishedAt=time.Now();_=store.UpdateTransactionFor(m.User,tx);return err}

func (m *Manager) RunBackground(action string,args []string,yes,autoSnapshot bool)error{
	if !m.User{if err:=m.requirePrivileges("background "+action);err!=nil{return err}}
	tx:=m.startTransaction(action,args,"");logDir:=m.Paths.Transactions;if err:=os.MkdirAll(logDir,0o755);err!=nil{return err};logFile,err:=os.OpenFile(filepath.Join(logDir,tx.ID+".log"),os.O_CREATE|os.O_WRONLY|os.O_TRUNC,0o644);if err!=nil{return err}
	cmd:=exec.Command(os.Args[0],"__worker",action,tx.ID,"--",strings.Join(args,"\x00"));cmd.Stdout=logFile;cmd.Stderr=logFile;cmd.Env=os.Environ();cmd.Env=append(cmd.Env,"YSPM_USER="+boolText(m.User),"YSPM_ARCH="+m.arch);if err:=cmd.Start();err!=nil{_ = logFile.Close();return err};_ = logFile.Close();fmt.Printf("Transaction %s started in background.\n",tx.ID);return nil
}
func (m *Manager) Worker(action,id,packed string,yes,autoSnapshot bool)error{args:=[]string{};if packed!=""{args=strings.Split(packed,"\x00")};var err error;switch action{case"install":err=m.InstallMany(args,true,autoSnapshot);case"remove":err=m.RemoveMany(args,true,autoSnapshot);case"upgrade":err=m.Upgrade(true,autoSnapshot);default:err=fmt.Errorf("unsupported background action %q",action)};tx,e:=store.GetTransactionFor(m.User,id);if e==nil{if err==nil{tx.Status="success"}else{tx.Status="failed";tx.Error=err.Error()};tx.FinishedAt=time.Now();_=store.UpdateTransactionFor(m.User,tx)};return err}

func (m *Manager) runInstalledHookFromArchive(_ model.InstalledPackage,_ string)error{return nil}

func packageFilename(p model.Package)string{if p.Format=="yspkg"{return p.Name+"-"+p.Version+"-"+p.Architecture+".yspkg"};if p.Kind=="appimage"||p.Format=="appimage"{return p.Name+"-"+p.Version+".AppImage"};if p.Format==""{return p.Name+"-"+p.Version};return p.Name+"-"+p.Version+"."+strings.ReplaceAll(p.Format,"/","-")}
func namesFromPackages(ps []model.Package)[]string{out:=make([]string,len(ps));for i,p:=range ps{out[i]=p.Name};return out}
func cleanupStaged(xs []stagedPackage){for _,x:=range xs{if x.Stage!=""{_ = os.RemoveAll(x.Stage)}}}
func within(root,target string)bool{rel,err:=filepath.Rel(root,target);if err!=nil{return false};return rel=="."||(!strings.HasPrefix(rel,".."+string(os.PathSeparator))&&rel!="..")}
func dependsOn(ds []model.Dependency,name string)bool{for _,d:=range ds{if parseDependency(string(d)).Name==name{return true}};return false}
func containsName(xs []string,want string)bool{for _,x:=range xs{if x==want{return true}};return false}
func containsString(xs []string,want string)bool{return containsName(xs,want)}
func confirm(prompt string)bool{if prompt!=""{fmt.Print(prompt)};var s string;_,_=fmt.Scanln(&s);return strings.EqualFold(strings.TrimSpace(s),"y")}
func printInstallPlan(plan resolvedPlan,db model.Database){fmt.Printf("Transaction plan (%d package(s)):\n",len(plan.Packages));for _,p:=range plan.Packages{if old,ok:=db.Packages[p.Name];ok{fmt.Printf("  upgrade %-20s %s -> %s\n",p.Name,old.Version,p.Version)}else{fmt.Printf("  install %-20s %s\n",p.Name,p.Version)}}}
func valueOr(a,b string)string{if a==""{return b};return a}
func firstNonEmpty(a,b string)string{if a!=""{return a};return b}
func boolText(v bool)string{if v{return"1"};return"0"}
func newID()string{return fmt.Sprintf("%x",time.Now().UnixNano())}

func escapeDesktopText(s string) string { s=strings.ReplaceAll(s,"\\","\\\\"); return strings.ReplaceAll(strings.ReplaceAll(s,"\n"," "),";","\\;") }
