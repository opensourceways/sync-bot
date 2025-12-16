package hook

import (
	"flag"
	"regexp"
	"strings"
)

// Strategy strategy of sync
type Strategy int

// three types strategy
const (
	Pick Strategy = iota
	Merge
	Overwrite
)

// SyncCmdOption /sync command option
type SyncCmdOption struct {
	strategy Strategy
	branches []string
}

func parseSyncCommand(command string) (*SyncCmdOption, error) {
	f := flag.NewFlagSet("/sync", flag.ContinueOnError)
	sep := regexp.MustCompile(`[ \t]+`)
	command = strings.TrimSpace(command)
	str := sep.Split(command, -1)
	err := f.Parse(str[1:])
	if err != nil {
		return nil, err
	}
	// Todo: default is Merge now, will change to Pick
	branches := f.Args()
	return &SyncCmdOption{
		strategy: Pick,
		branches: branches,
	}, nil
}
