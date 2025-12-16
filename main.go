package main

import (
	"flag"
	"os"

	"github.com/opensourceways/robot-framework-lib/framework"
	"sync-bot/hook"
)

const component = "robot-sync-bot"

func main() {
	logger := framework.NewLogger().WithField("component", component)
	opt := new(robotOptions)
	// Gather the necessary arguments from command line for project startup
	opt.gatherOptions(flag.NewFlagSet(os.Args[0], flag.ExitOnError), logger, os.Args[1:]...)
	if opt.service.Interrupt {
		return
	}

	cnf := opt.service.ConfigmapAgentValue.GetConfigmap().(*hook.Configuration)
	bot := hook.NewRobot(cnf, opt.service.TokenValue, logger)
	if bot == nil {
		return
	}
	framework.StartupServer(framework.NewServer(bot, opt.service), opt.service)
}
