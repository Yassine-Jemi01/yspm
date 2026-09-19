package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Yassine-Jemi01/yspm/internal/manager"
	"github.com/Yassine-Jemi01/yspm/internal/model"
)

func usage() {
	fmt.Print(`yspm - a small Linux package manager written in Go

Usage:
  yspm <command> [options] [arguments]

Commands:
  install <pkg>...        Install packages and resolve dependencies
  remove <pkg>...        Remove packages safely
  autoremove             Remove unneeded auto-installed dependencies
  search <query>         Search the repository
  info <pkg>              Show package information
  list                   List installed packages
  depends <pkg>           Show the dependency tree
  why <pkg>               Show why an installed package is needed
  explain <pkg>           Explain dependency metadata and resolution
  update                 Refresh the pinned stable repository index
  upgrade                Upgrade packages within the current release
  clean                  Remove cached package archives
  check                  Check local package database consistency
  history                Show transaction history
  transaction <id>       Show a transaction status
  release                Show the pinned stable release

Options:
  -y, --yes              Do not ask for confirmation
  --background           Run the transaction in the background
  list --upgradable       List only packages with available upgrades
  list --explicit         List explicitly installed packages

Environment:
  YSPM_REPOSITORY             Repository index URL/path
  YSPM_DATA_DIR               Package database/state directory
  YSPM_CACHE_DIR              Package cache directory
  YSPM_BIN_DIR                Executable directory
  YSPM_APPLICATIONS_DIR       Desktop entry directory
  YSPM_REQUIRE_SIGNATURES=1   Require Ed25519 repository signature verification
  YSPM_REPOSITORY_SIGNATURE   Detached repository signature URL/path
  YSPM_REPOSITORY_PUBLIC_KEY  Ed25519 public key (hex/base64)

Examples:
  yspm update
  yspm search browser
  yspm info firefox
  yspm install firefox git curl
  yspm install firefox --background
  yspm remove firefox -y
  yspm depends devkit
  yspm why ripgrep
  yspm list --upgradable
  yspm transaction 9c1a2b3c4d5e
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	mgr := manager.New()
	cmd := os.Args[1]
	args := os.Args[2:]
	if cmd == "__worker" {
		if len(args) < 3 {
			fatal("invalid worker arguments")
		}
		action, id := args[0], args[1]
		packed := ""
		if args[2] == "--" && len(args) > 3 {
			packed = args[3]
		}
		if err := mgr.Worker(action, id, packed); err != nil {
			fatal(err.Error())
		}
		return
	}
	yes, background, rest := parseFlags(args)
	if background && (cmd == "install" || cmd == "remove" || cmd == "upgrade" || cmd == "autoremove") {
		if err := mgr.RunBackground(cmd, rest); err != nil {
			fatal(err.Error())
		}
		return
	}
	switch cmd {
	case "install":
		if len(rest) == 0 {
			fatal("install requires at least one package name")
		}
		if err := mgr.InstallMany(rest, yes); err != nil {
			fatal(err.Error())
		}
	case "remove":
		if len(rest) == 0 {
			fatal("remove requires at least one package name")
		}
		if err := mgr.RemoveMany(rest, yes); err != nil {
			fatal(err.Error())
		}
	case "autoremove":
		if err := mgr.Autoremove(yes); err != nil {
			fatal(err.Error())
		}
	case "search":
		if len(rest) != 1 {
			fatal("search requires one query")
		}
		results, err := mgr.Search(rest[0])
		if err != nil {
			fatal(err.Error())
		}
		if len(results) == 0 {
			fmt.Println("No packages found.")
			return
		}
		for _, p := range results {
			fmt.Printf("%-16s %-10s %s\n", p.Name, p.Version, p.Description)
		}
	case "info":
		if len(rest) != 1 {
			fatal("info requires one package name")
		}
		p, err := mgr.Info(rest[0])
		if err != nil {
			fatal(err.Error())
		}
		printInfo(p)
	case "list":
		upgradable := contains(rest, "--upgradable")
		explicit := contains(rest, "--explicit")
		packages, err := mgr.List(upgradable, explicit)
		if err != nil {
			fatal(err.Error())
		}
		if len(packages) == 0 {
			fmt.Println("No matching packages installed.")
			return
		}
		for _, p := range packages {
			marker := ""
			if p.Explicit {
				marker = "*"
			}
			fmt.Printf("%-16s %-10s %-8s %s\n", p.Name, p.Version, p.Kind, marker)
		}
	case "depends":
		if len(rest) != 1 {
			fatal("depends requires one package name")
		}
		if err := mgr.Depends(rest[0]); err != nil {
			fatal(err.Error())
		}
	case "why":
		if len(rest) != 1 {
			fatal("why requires one package name")
		}
		if err := mgr.Why(rest[0]); err != nil {
			fatal(err.Error())
		}
	case "explain":
		if len(rest) != 1 {
			fatal("explain requires one package name")
		}
		if err := mgr.Explain(rest[0]); err != nil {
			fatal(err.Error())
		}
	case "update":
		if err := mgr.Update(); err != nil {
			fatal(err.Error())
		}
	case "upgrade":
		if err := mgr.Upgrade(yes); err != nil {
			fatal(err.Error())
		}
	case "clean":
		if err := mgr.Clean(); err != nil {
			fatal(err.Error())
		}
	case "check":
		if err := mgr.Check(); err != nil {
			fatal(err.Error())
		}
	case "history":
		if err := mgr.History(); err != nil {
			fatal(err.Error())
		}
	case "release":
		if err := mgr.Release(); err != nil {
			fatal(err.Error())
		}
	case "transaction":
		if len(rest) != 1 {
			fatal("transaction requires an ID")
		}
		if err := mgr.Transaction(rest[0]); err != nil {
			fatal(err.Error())
		}
	case "help", "--help", "-h":
		usage()
	default:
		usage()
		fatal(fmt.Sprintf("unknown command %q", cmd))
	}
}

func parseFlags(args []string) (bool, bool, []string) {
	yes, background := false, false
	var rest []string
	for _, a := range args {
		switch a {
		case "-y", "--yes":
			yes = true
		case "--background":
			background = true
		default:
			rest = append(rest, a)
		}
	}
	return yes, background, rest
}
func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
func printInfo(pkg model.Package) {
	fmt.Printf("Name:         %s\nVersion:      %s\nRevision:     %d\nDescription:  %s\nKind:         %s\nOS/Arch:      %s/%s\nLicense:      %s\n", pkg.Name, pkg.Version, pkg.Revision, pkg.Description, pkg.Kind, pkg.OS, pkg.Architecture, pkg.License)
	if pkg.Size > 0 {
		fmt.Printf("Size:         %d bytes\n", pkg.Size)
	} else {
		fmt.Println("Size:         unknown")
	}
	if pkg.SHA256 == "" {
		fmt.Println("SHA256:       not provided")
	} else {
		fmt.Printf("SHA256:       %s\n", pkg.SHA256)
	}
	if len(pkg.Dependencies) == 0 {
		fmt.Println("Dependencies: none")
	} else {
		fmt.Printf("Dependencies: %s\n", joinDeps(pkg.Dependencies))
	}
	if len(pkg.Recommends) > 0 {
		fmt.Printf("Recommends:   %s\n", joinDeps(pkg.Recommends))
	}
	if len(pkg.Suggests) > 0 {
		fmt.Printf("Suggests:     %s\n", joinDeps(pkg.Suggests))
	}
	if len(pkg.Conflicts) > 0 {
		fmt.Printf("Conflicts:    %s\n", strings.Join(pkg.Conflicts, ", "))
	}
	if len(pkg.Provides) > 0 {
		fmt.Printf("Provides:     %s\n", strings.Join(pkg.Provides, ", "))
	}
	if len(pkg.Replaces) > 0 {
		fmt.Printf("Replaces:     %s\n", strings.Join(pkg.Replaces, ", "))
	}
	if pkg.Homepage != "" {
		fmt.Printf("Homepage:     %s\n", pkg.Homepage)
	}
	if pkg.URL != "" {
		fmt.Printf("URL:          %s\n", pkg.URL)
	}
}
func joinDeps(d []model.Dependency) string {
	out := make([]string, len(d))
	for i, x := range d {
		out[i] = string(x)
	}
	return strings.Join(out, ", ")
}
func fatal(message string) { fmt.Fprintf(os.Stderr, "yspm: %s\n", message); os.Exit(1) }
