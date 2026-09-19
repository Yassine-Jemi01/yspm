package manager

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Yassine-Jemi01/yspm/internal/model"
)

func DetectServiceManager(m *Manager) string {
	if m.User { return "none" }
	for _, name := range []string{"systemctl","rc-service","sv"} {
		if _, err := exec.LookPath(name); err == nil {
			switch name {
			case "systemctl": return "systemd"
			case "rc-service": return "openrc"
			case "sv": return "runit"
			}
		}
	}
	for _, p := range []string{filepath.Join(m.Paths.Root,"usr","bin","initctl"), filepath.Join(m.Paths.Root,"sbin","initctl"), filepath.Join(m.Paths.Root,"usr","sbin","initctl")} {
		if _, err := os.Stat(p); err == nil { return "hardcore" }
	}
	return "unknown"
}

func ApplyServices(m *Manager, p model.Package) error {
	if m.User || len(p.Services)==0 { return nil }
	manager := DetectServiceManager(m)
	if manager=="unknown" { return fmt.Errorf("package %s declares services but no supported service manager was detected",p.Name) }
	for _,svc:=range p.Services{
		if svc.Name=="" { continue }
		if err:=serviceOne(m,manager,svc);err!=nil{return err}
	}
	return nil
}

func serviceOne(m *Manager, manager string, svc model.Service) error {
	switch manager {
	case "systemd":
		if err:=runInRoot(m, "systemctl", "daemon-reload");err!=nil{return err}
		if svc.Enable{if err:=runInRoot(m,"systemctl","enable",svc.Name);err!=nil{return err}}
		if svc.Start{if err:=runInRoot(m,"systemctl","start",svc.Name);err!=nil{return err}}
	case "openrc":
		if svc.Enable{if err:=runInRoot(m,"rc-update","add",svc.Name,"default");err!=nil{return err}}
		if svc.Start{if err:=runInRoot(m,"rc-service",svc.Name,"start");err!=nil{return err}}
	case "runit":
		if svc.Enable {
			src:=filepath.Join(m.Paths.Root,"etc","sv",svc.Name)
			dst:=filepath.Join(m.Paths.Root,"var","service",svc.Name)
			if _,err:=os.Stat(src);err!=nil{return fmt.Errorf("runit service %s not found at %s",svc.Name,src)}
			_ = os.Remove(dst)
			if err:=os.MkdirAll(filepath.Dir(dst),0o755);err!=nil{return err}
			if err:=os.Symlink(src,dst);err!=nil{return err}
		}
	case "hardcore":
		initctl:=findHardcoreInitctl(m)
		if initctl==""{return fmt.Errorf("HardcoreLinux initctl was not found")}
		if svc.Enable{if err:=runCommand(initctl,"enable",svc.Name);err!=nil{return err}}
		if svc.Start{if err:=runCommand(initctl,"start",svc.Name);err!=nil{return err}}
	default:
		return fmt.Errorf("unsupported service manager %s",manager)
	}
	return nil
}

func DisableServices(m *Manager, services []model.Service) error {
	if m.User || len(services)==0{return nil}
	manager:=DetectServiceManager(m)
	for _,svc:=range services{
		if svc.Name==""{continue}
		switch manager{
		case "systemd":
			_ = runInRoot(m,"systemctl","stop",svc.Name)
			_ = runInRoot(m,"systemctl","disable",svc.Name)
			_ = runInRoot(m,"systemctl","daemon-reload")
		case "openrc":
			if svc.Start{_ = runInRoot(m,"rc-service",svc.Name,"stop")}
			if svc.Enable{_ = runInRoot(m,"rc-update","del",svc.Name,"default")}
		case "runit":
			dst:=filepath.Join(m.Paths.Root,"var","service",svc.Name)
			_ = os.Remove(dst)
		case "hardcore":
			initctl:=findHardcoreInitctl(m)
			if initctl!=""{
				if svc.Start{_ = runCommand(initctl,"stop",svc.Name)}
				if svc.Enable{_ = runCommand(initctl,"disable",svc.Name)}
			}
		}
	}
	return nil
}

func runInRoot(m *Manager, name string, args ...string) error {
	if m.Paths.Root=="/" { return runCommand(name,args...) }
	// For chroot test roots, require the service manager binary inside the root.
	bin:=filepath.Join(m.Paths.Root,"usr","bin",name)
	if _,err:=os.Stat(bin);err!=nil{bin=filepath.Join(m.Paths.Root,"bin",name)}
	if _,err:=os.Stat(bin);err!=nil{return fmt.Errorf("%s is not present in target root",name)}
	return runCommand("chroot",append([]string{m.Paths.Root,bin},args...)...)
}

func runCommand(name string,args ...string) error {
	cmd:=exec.Command(name,args...)
	out,err:=cmd.CombinedOutput()
	if err!=nil{return fmt.Errorf("%s %s failed: %w: %s",name,strings.Join(args," "),err,strings.TrimSpace(string(out)))}
	return nil
}

func findHardcoreInitctl(m *Manager) string {
	for _,p:=range []string{filepath.Join(m.Paths.Root,"usr","bin","initctl"),filepath.Join(m.Paths.Root,"sbin","initctl"),filepath.Join(m.Paths.Root,"usr","sbin","initctl")}{
		if _,err:=os.Stat(p);err==nil{return p}
	}
	return ""
}

func ApplyTriggers(m *Manager, p model.Package) error {
	if m.User || len(p.Triggers)==0 { return nil }
	for _, trigger := range p.Triggers {
		switch trigger {
		case "ldconfig":
			if m.Paths.Root=="/" {
				if err:=runCommand("ldconfig"); err!=nil{return err}
			} else {
				if err:=runInRoot(m,"ldconfig","-r",m.Paths.Root);err!=nil{return err}
			}
		case "desktop-database":
			dir:=filepath.Join(m.Paths.Root,"usr","share","applications")
			if _,err:=os.Stat(dir);os.IsNotExist(err){continue}
			if m.Paths.Root=="/" {
				if err:=runCommand("update-desktop-database",dir);err!=nil{return err}
			} else {
				if err:=runInRoot(m,"update-desktop-database","/usr/share/applications");err!=nil{return err}
			}
		case "font-cache":
			if m.Paths.Root=="/" {
				if _,err:=exec.LookPath("fc-cache");err==nil{if err:=runCommand("fc-cache","-f");err!=nil{return err}}
			} else if _,err:=os.Stat(filepath.Join(m.Paths.Root,"usr","bin","fc-cache"));err==nil {
				if err:=runInRoot(m,"fc-cache","-f");err!=nil{return err}
			}
		case "icon-cache":
			if m.Paths.Root=="/" {
				if _,err:=exec.LookPath("gtk-update-icon-cache");err==nil{continue}
			}
		default:
			return fmt.Errorf("unknown yspm trigger %q",trigger)
		}
	}
	return nil
}
