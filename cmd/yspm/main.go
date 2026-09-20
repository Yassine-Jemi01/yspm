package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Yassine-Jemi01/yspm/internal/manager"
	"github.com/Yassine-Jemi01/yspm/internal/model"
)

func usage() {
	fmt.Print(`yspm - a Linux system package manager written in Go

Usage:
  yspm <command> [options] [arguments]

Commands:
  install <pkg>...           Install packages and dependencies
  remove <pkg>...            Remove packages safely
  autoremove                 Remove unneeded auto-installed dependencies
  search <query>             Search the repository
  info <pkg>                 Show package information
  list                       List installed packages
  depends <pkg>              Show the dependency tree
  why <pkg>                  Show why an installed package is needed
  explain <pkg>              Explain package ABI/dependency metadata
  update                     Refresh the pinned repository index
  upgrade                    Upgrade packages within the current release
  release                    Show the pinned stable release
  release upgrade <release>  Upgrade to another stable release
  clean                      Remove cached package archives
  check                      Check local package database consistency
  audit                      Check installed packages against vulnerability metadata
  history                    Show transaction history
  transaction <id>           Show transaction status
  snapshot create            Create a read-only Btrfs system snapshot
  snapshot list              List system snapshots
  snapshot restore <id>      Restore a snapshot in a prepared/unmounted target
  build                      Build a native .yspkg package
  repo index                 Build repository index from .yspkg files
  repo sign                  Sign a repository index
  keygen                    Generate an Ed25519 repository keypair

Options:
  -y, --yes                  Do not ask for confirmation
  --user                     Install/manage packages in the user's home
  --arch <arch>              Target architecture (e.g. x86_64, aarch64)
  --snapshot                 Create a Btrfs snapshot before mutating the system
  --background               Run a transaction in the background

Environment:
  YSPM_ROOT                         Alternate system root for chroot/test environments
  YSPM_REPOSITORY                   Repository index URL/path
  YSPM_RELEASE_REPOSITORY_TEMPLATE  Release URL template, e.g. .../{release}/index.json
  YSPM_DATA_DIR / CACHE_DIR         Optional legacy overrides for tools using the old layout
  YSPM_REQUIRE_SIGNATURES=1         Require Ed25519 repository index signatures
  YSPM_REPOSITORY_SIGNATURE         Detached repository signature URL/path
  YSPM_REPOSITORY_PUBLIC_KEY        Ed25519 public key (hex/base64)
  YSPM_FOREIGN_ARCHS                Comma-separated foreign architectures
  YSPM_SNAPSHOT_ROOT                Snapshot source
  YSPM_SNAPSHOT_DIR                 Snapshot storage directory
`)
}

func main() {
	if len(os.Args) < 2 { usage(); return }
	user := has(os.Args[2:], "--user")
	arch := valueAfter(os.Args[2:], "--arch")
	if arch == "" { arch = os.Getenv("YSPM_ARCH") }
	if os.Getenv("YSPM_USER") == "1" { user = true }
	m := manager.New(user, arch)

	cmd := os.Args[1]
	args := strip(os.Args[2:], "--user", "--snapshot", "-y", "--yes", "--background")
	if cmd == "__worker" {
		if len(args) < 3 { fatal("invalid worker arguments") }
		action,id := args[0],args[1]
		packed:=""
		if args[2]=="--"&&len(args)>3{packed=args[3]}
		if err:=m.Worker(action,id,packed,true,has(os.Args[2:],"--snapshot"));err!=nil{fatal(err.Error())}
		return
	}

	yes := has(os.Args[2:], "-y") || has(os.Args[2:], "--yes")
	background := has(os.Args[2:], "--background")
	autoSnapshot := has(os.Args[2:], "--snapshot")
	if background && (cmd=="install"||cmd=="remove"||cmd=="upgrade"||cmd=="autoremove") {
		if err:=m.RunBackground(cmd,strip(args,"-y","--yes","--background"),yes,autoSnapshot);err!=nil{fatal(err.Error())}
		return
	}

	switch cmd {
	case "install":
		if len(args)==0{fatal("install requires at least one package")}
		local:=false
		for _,a:=range args{if strings.HasSuffix(strings.ToLower(a),".yspkg"){local=true;break}}
		if local {
			if err:=m.InstallLocal(args,yes,autoSnapshot);err!=nil{fatal(err.Error())}
		} else if err:=m.InstallMany(args,yes,autoSnapshot);err!=nil{fatal(err.Error())}
	case "remove":
		if len(args)==0{fatal("remove requires at least one package")}
		if err:=m.RemoveMany(args,yes,autoSnapshot);err!=nil{fatal(err.Error())}
	case "autoremove":
		if err:=m.Autoremove(yes,autoSnapshot);err!=nil{fatal(err.Error())}
	case "search":
		if len(args)!=1{fatal("search requires one query")}
		ps,err:=m.Search(args[0]);if err!=nil{fatal(err.Error())}
		for _,p:=range ps{fmt.Printf("%-20s %-12s %s\n",p.Name,p.Version,p.Description)}
	case "info":
		if len(args)!=1{fatal("info requires one package")}
		p,err:=m.Info(args[0]);if err!=nil{fatal(err.Error())};printInfo(p)
	case "list":
		up,ex:=has(args,"--upgradable"),has(args,"--explicit");ps,err:=m.List(up,ex);if err!=nil{fatal(err.Error())}
		for _,p:=range ps{mark:="";if p.Explicit{mark="*"};fmt.Printf("%-20s %-12s %-10s %s\n",p.Name,p.Version,p.Kind,mark)}
	case "depends":
		if len(args)!=1{fatal("depends requires one package")};if err:=m.Depends(args[0]);err!=nil{fatal(err.Error())}
	case "why":
		if len(args)!=1{fatal("why requires one package")};if err:=m.Why(args[0]);err!=nil{fatal(err.Error())}
	case "explain":
		if len(args)!=1{fatal("explain requires one package")};if err:=m.Explain(args[0]);err!=nil{fatal(err.Error())}
	case "update":
		if err:=m.Update();err!=nil{fatal(err.Error())}
	case "upgrade":
		if err:=m.Upgrade(yes,autoSnapshot);err!=nil{fatal(err.Error())}
	case "release":
		if len(args)==0{if err:=m.Release();err!=nil{fatal(err.Error())};return}
		if len(args)==2&&args[0]=="upgrade"{if err:=m.UpgradeRelease(args[1],yes,autoSnapshot);err!=nil{fatal(err.Error())};return}
		fatal("usage: yspm release [upgrade <release>]")
	case "clean":
		if err:=m.Clean();err!=nil{fatal(err.Error())}
	case "check":
		if err:=m.Check();err!=nil{fatal(err.Error())}
	case "audit":
		if err:=m.Audit();err!=nil{fatal(err.Error())}
	case "history":
		if err:=m.History();err!=nil{fatal(err.Error())}
	case "transaction":
		if len(args)!=1{fatal("transaction requires an ID")};if err:=m.Transaction(args[0]);err!=nil{fatal(err.Error())}
	case "snapshot":
		if err:=snapshotCommand(m,args);err!=nil{fatal(err.Error())}
	case "build":
		if err:=buildCommand(m,args);err!=nil{fatal(err.Error())}
	case "repo":
		if err:=repoCommand(m,args);err!=nil{fatal(err.Error())}
	case "keygen":
		if len(args)!=2{fatal("usage: yspm keygen <public-key> <private-key>")};if err:=m.GenerateKey(args[0],args[1]);err!=nil{fatal(err.Error())}
	case "help","--help","-h":
		usage()
	default:
		usage();fatal(fmt.Sprintf("unknown command %q",cmd))
	}
}

func buildCommand(m *manager.Manager,args []string)error{
	fs:=flag.NewFlagSet("build",flag.ContinueOnError)
	root:=fs.String("root","","directory containing the package filesystem tree")
	out:=fs.String("output","","output .yspkg path")
	name:=fs.String("name","","package name")
	version:=fs.String("version","","package version")
	desc:=fs.String("description","","package description")
	abi:=fs.String("abi","","distribution ABI identifier")
	arch:=fs.String("arch","","target architecture")
	license:=fs.String("license","unknown","package license")
	maintainer:=fs.String("maintainer","","package maintainer")
	scripts:=fs.String("scripts","","directory containing pre/post install/remove scripts")
	if err:=fs.Parse(args);err!=nil{return err}
	if *root==""||*out==""||*name==""||*version==""{return fmt.Errorf("--root, --output, --name and --version are required")}
	p,err:=m.Build(*root,*out,*name,*version,*desc,*abi,*arch,*license,*maintainer,*scripts)
	if err!=nil{return err}
	fmt.Printf("Built %s %s -> %s\n",p.Name,p.Version,*out);return nil
}

func repoCommand(m *manager.Manager,args []string)error{
	if len(args)==0{return fmt.Errorf("usage: yspm repo index|sign ...")}
	switch args[0]{
	case "index":
		fs:=flag.NewFlagSet("repo index",flag.ContinueOnError)
		dir:=fs.String("dir","","directory containing .yspkg files")
		out:=fs.String("output","index.json","output index")
		base:=fs.String("base-url","","package base URL")
		rel:=fs.String("release","1","stable release number")
		abi:=fs.String("abi","yspm-abi-1","distribution ABI")
		if err:=fs.Parse(args[1:]);err!=nil{return err}
		if *dir==""||*base==""{return fmt.Errorf("--dir and --base-url are required")}
		return m.RepoIndex(*dir,*out,*base,*rel,*abi)
	case "sign":
		if len(args)!=4{return fmt.Errorf("usage: yspm repo sign <index.json> <private-key> <signature>")}
		return m.RepoSign(args[1],args[2],args[3])
	default:return fmt.Errorf("unknown repo command %q",args[0])
	}
}

func snapshotCommand(m *manager.Manager,args []string)error{
	if len(args)==0{return fmt.Errorf("usage: yspm snapshot create|list|restore <id>")}
	switch args[0]{case "create":_,err:=manager.CreateSnapshot(m);return err
	case "list":return manager.ListSnapshots(m)
	case "restore":if len(args)!=2{return fmt.Errorf("snapshot restore requires an ID")};return manager.RestoreSnapshot(m,args[1])
	default:return fmt.Errorf("unknown snapshot command %q",args[0])}
}

func printInfo(p model.Package){
	fmt.Printf("Name:         %s\nVersion:      %s\nRevision:     %d\nDescription:  %s\nKind:         %s\nOS/Arch:      %s/%s\nABI:          %s\nLicense:      %s\n",p.Name,p.Version,p.Revision,p.Description,p.Kind,p.OS,p.Architecture,value(p.ABI),p.License)
	fmt.Printf("URL:          %s\nSHA256:       %s\n",p.URL,value(p.SHA256))
	fmt.Printf("Dependencies: ");if len(p.Dependencies)==0{fmt.Println("none")}else{d:=make([]string,len(p.Dependencies));for i,x:=range p.Dependencies{d[i]=string(x)};fmt.Println(strings.Join(d,", "))}
	if len(p.SharedRequires)>0{fmt.Printf("Shared requires: %s\n",strings.Join(p.SharedRequires,", "))}
	if len(p.SharedProvides)>0{fmt.Printf("Shared provides: %s\n",strings.Join(p.SharedProvides,", "))}
	if len(p.ConfigFiles)>0{fmt.Printf("Config files: %s\n",strings.Join(p.ConfigFiles,", "))}
	if len(p.Services)>0{fmt.Printf("Services: ");for i,s:=range p.Services{if i>0{fmt.Print(", ")};fmt.Print(s.Name)};fmt.Println()}
}

func has(xs []string,want string)bool{for _,x:=range xs{if x==want{return true}};return false}
func valueAfter(xs []string,want string)string{for i,x:=range xs{if x==want&&i+1<len(xs){return xs[i+1]};if strings.HasPrefix(x,want+"="){return strings.TrimPrefix(x,want+"=")}};return ""}
func strip(xs []string,wants ...string)[]string{out:=[]string{};skip:=false;set:=map[string]bool{};for _,w:=range wants{set[w]=true};for _,x:=range xs{if skip{skip=false;continue};if set[x]{if x=="--arch"{skip=true};continue};if strings.HasPrefix(x,"--arch="){continue};out=append(out,x)};return out}
func value(s string)string{if s==""{return "none"};return s}
func fatal(message string){fmt.Fprintf(os.Stderr,"yspm: %s\n",message);os.Exit(1)}
