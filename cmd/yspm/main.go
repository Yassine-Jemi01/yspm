package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Yassine-Jemi01/yspm/internal/manager"
)

func usage() {
	fmt.Print(`yspm - a small Linux package manager written in Go

Usage:
  yspm <command> [arguments]

Commands:
  install <package>...   Install packages and dependencies
  remove <package>...    Remove installed packages
  search <query>         Search the repository
  info <package>         Show package information
  list                   List installed packages
  update                 Refresh the package index
  upgrade                Upgrade installed packages

Environment:
  YSPM_REPOSITORY        Override the default repository index URL/path

Examples:
  yspm update
  yspm search browser
  yspm info firefox
  yspm install firefox
  yspm install devkit
  yspm remove firefox -y
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

	switch cmd {
	case "install":
		if len(args) == 0 {
			fatal("install requires at least one package name")
		}
		for _, name := range args {
			if name == "-y" || name == "--yes" {
				continue
			}
			if err := mgr.Install(name); err != nil {
				fatal(err.Error())
			}
		}
	case "remove":
		if len(args) == 0 {
			fatal("remove requires at least one package name")
		}
		yes := false
		var names []string
		for _, arg := range args {
			if arg == "-y" || arg == "--yes" {
				yes = true
				continue
			}
			names = append(names, arg)
		}
		if !yes {
			fmt.Printf("Remove %s? [y/N] ", strings.Join(names, ", "))
			var answer string
			_, _ = fmt.Scanln(&answer)
			if strings.ToLower(strings.TrimSpace(answer)) != "y" {
				fmt.Println("Aborted.")
				return
			}
		}
		for _, name := range names {
			if err := mgr.Remove(name); err != nil {
				fatal(err.Error())
			}
		}
	case "search":
		if len(args) != 1 {
			fatal("search requires one query")
		}
		results, err := mgr.Search(args[0])
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
		if len(args) != 1 {
			fatal("info requires one package name")
		}
		p, err := mgr.Info(args[0])
		if err != nil {
			fatal(err.Error())
		}
		fmt.Printf("Name:         %s\nVersion:      %s\nDescription:  %s\nKind:         %s\nOS/Arch:      %s/%s\nLicense:      %s\n", p.Name, p.Version, p.Description, p.Kind, p.OS, p.Architecture, p.License)
		if p.Size > 0 {
			fmt.Printf("Size:         %d bytes\n", p.Size)
		} else {
			fmt.Println("Size:         unknown")
		}
		if p.SHA256 == "" {
			fmt.Println("SHA256:       not provided")
		} else {
			fmt.Printf("SHA256:       %s\n", p.SHA256)
		}
		if len(p.Dependencies) == 0 {
			fmt.Println("Dependencies: none")
		} else {
			fmt.Printf("Dependencies: %s\n", strings.Join(p.Dependencies, ", "))
		}
		if p.Homepage != "" {
			fmt.Printf("Homepage:     %s\n", p.Homepage)
		}
		if p.URL != "" {
			fmt.Printf("URL:          %s\n", p.URL)
		}
	case "list":
		packages, err := mgr.List()
		if err != nil {
			fatal(err.Error())
		}
		if len(packages) == 0 {
			fmt.Println("No packages installed.")
			return
		}
		for _, p := range packages {
			fmt.Printf("%-16s %-10s %s\n", p.Name, p.Version, p.Kind)
		}
	case "update":
		if err := mgr.Update(); err != nil {
			fatal(err.Error())
		}
	case "upgrade":
		if err := mgr.Upgrade(); err != nil {
			fatal(err.Error())
		}
	case "help", "--help", "-h":
		usage()
	default:
		usage()
		fatal(fmt.Sprintf("unknown command %q", cmd))
	}
}

func fatal(message string) { fmt.Fprintf(os.Stderr, "yspm: %s\n", message); os.Exit(1) }