package main

import (
	"fmt"
	"os"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "install":
		err = cmdInstall(os.Args[2:])
	case "run":
		err = cmdRun(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "modify":
		err = cmdModify(os.Args[2:])
	case "topup":
		err = cmdTopup(os.Args[2:])
	case "config-example":
		err = cmdConfigExample(os.Args[2:])
	case "rollback":
		err = cmdRollback(os.Args[2:])
	case "doctor":
		err = cmdDoctor(os.Args[2:])
	case "uninstall":
		err = cmdUninstall(os.Args[2:])
	case "upgrade":
		err = cmdUpgrade(os.Args[2:])
	case "limit":
		err = cmdLimit(os.Args[2:])
	case "unlimit":
		err = cmdUnlimit(os.Args[2:])
	case "test-notify":
		err = cmdTestNotify(os.Args[2:])
	case "check-once":
		err = cmdCheckOnce(os.Args[2:])
	case "version", "--version", "-v":
		err = cmdVersion(os.Args[2:])
	case "help", "--help", "-h":
		err = cmdHelp(os.Args[2:])
	default:
		usage()
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
