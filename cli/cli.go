package cli

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/mirror-media/yt-relay/config"
)

type Command struct {
	// Flags to look up in the global table
	Flags []string
	// Main runs the command, args are the arguments after flags
	Main func(args []string, c Conf) error
}

func Start(cmds map[string]*Command) error {

	var ytrelayFlagSet = flag.NewFlagSet("ytrelay", flag.ExitOnError)
	var c Conf

	registerFlags(&c, ytrelayFlagSet)

	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "No command is given.\n")
		flag.Usage()
		return errors.New("no command was given")
	}

	// Get the command and arguments
	cmdName := flag.Arg(0)
	args := flag.Args()[1:]
	cmd, found := cmds[cmdName]
	if !found {
		fmt.Fprintf(os.Stderr, "Command %s is not defined.\n", cmdName)
		return errors.New("undefined command")
	}

	// Parse the flag from the remaining arguments
	_ = ytrelayFlagSet.Parse(args)
	args = ytrelayFlagSet.Args()

	cfg, err := config.Load(c.ConfigFile)
	if err != nil {
		log.Printf("Failed to load config: %v", err)
		return errors.New("failed to load config")
	}
	cfg.Address = c.Address
	cfg.Port = c.Port
	c.CFG = cfg

	if err := cmd.Main(args, c); err != nil {
		log.Print(err)
		return err
	}

	return nil
}
