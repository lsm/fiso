package main

import (
	"fmt"
	"os"

	"github.com/lsm/fiso/internal/cli"
)

const usage = `fiso - Fiso development toolkit

Usage:
  fiso <command> [arguments]

Commands:
  init [project-name]   Create a new Fiso project
  dev                   Start local development environment
  validate [path]       Validate flow and link configuration files
  export [path]         Export configs as Kubernetes CRDs
  doctor                Check environment and project health

Run 'fiso <command> -h' for help on a specific command.`

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		fmt.Println(usage)
		return nil
	}

	switch os.Args[1] {
	case "init":
		return cli.RunInit(os.Args[2:], nil)
	case "dev":
		return cli.RunDev(os.Args[2:])
	case "validate":
		return cli.RunValidate(os.Args[2:])
	case "export":
		return cli.RunExport(os.Args[2:], nil)
	case "doctor":
		return cli.RunDoctor(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Println(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q\nRun 'fiso help' for usage", os.Args[1])
	}
}
