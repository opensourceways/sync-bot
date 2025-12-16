package hook

import (
	"fmt"
	"strings"

	"sync-bot/util"
	"sync-bot/util/rpm"

	"github.com/opensourceways/robot-framework-lib/client"
	"github.com/opensourceways/robot-framework-lib/utils"

	"github.com/sirupsen/logrus"
)

func (bot *robot) greeting(org string, repo string, number string, targetBranch string, logger *logrus.Entry) {
	branches, ok := bot.cli.GetRepoAllBranch(org, repo)
	if !ok {
		logger.Errorf("Get Branches failed. org: %s, repo: %s, number: %s, targetBranch: %s", org, repo, number, targetBranch)
		return
	}
	type branchExt struct {
		Name, Version, Release string
	}
	branchesExt := make([]branchExt, len(branches))
	for i, branch := range branches {
		if branchesExt[i].Name == targetBranch {
			branches[i].Name = fmt.Sprintf("__*__ [%s](https://gitcode.com/%s/%s/tree/%s)",
				branch.Name, org, repo, branch.Name)
		} else {
			branchesExt[i].Name = fmt.Sprintf("[%s](https://gitcode.com/%s/%s/tree/%s)",
				branch.Name, org, repo, branch.Name)
		}
		// extract Version and Release from spec file
		spec, ok := bot.cli.GetPathContent(org, repo, repo+".spec", branch.Name)
		if !ok {
			logger.Errorf("Get spec file failed. org: %s, repo: %s, number: %s, targetBranch: %s", org, repo, number, targetBranch)
			continue
		}
		s := rpm.NewSpec(utils.GetString(spec.Content))
		if s != nil {
			branchesExt[i].Version = s.Version()
			branchesExt[i].Release = s.Release()
		}
	}

	replyContent, err := executeTemplate(replySyncCheckTmpl, branchesExt)
	if err != nil {
		logger.Errorln("Execute template failed:", err)
		return
	}
	bot.cli.CreatePRComment(org, repo, number, replyContent)
}

func (bot *robot) replySync(evt *client.GenericEvent, logger *logrus.Entry) {
	owner := utils.GetString(evt.Org)
	repo := utils.GetString(evt.Repo)
	number := utils.GetString(evt.Number)
	comment := utils.GetString(evt.Comment)
	user := utils.GetString(evt.Commenter)
	url := utils.GetString(evt.HtmlURL)

	opt, err := parseSyncCommand(comment)
	if err != nil {
		logger.Errorf("Parse /sync command failed: %s", err)
		comment := fmt.Sprintf("Receive comment look like /sync command, but parseSyncCommand failed: %v", err)
		logger.Errorln(comment)
		bot.cli.CreatePRComment(owner, repo, number, comment)
		return
	}

	// retrieve all branches
	allBranches, ok := bot.cli.GetRepoAllBranch(owner, repo)
	if !ok {
		comment := fmt.Sprintf("List branches failed. org: %s, repo: %s, number: %s", owner, repo, number)
		logger.Errorln(comment)
		ok = bot.cli.CreatePRComment(owner, repo, number, comment)
		if !ok {
			logger.Errorf("Create Comment failed. org: %s, repo: %s, number: %s", owner, repo, number)
		}
		return
	}
	branchSet := make(map[string]bool)
	for _, b := range allBranches {
		branchSet[b.Name] = true
	}

	var synBranches []branchStatus
	for _, b := range opt.branches {
		if ok := branchSet[b]; ok {
			synBranches = append(synBranches, branchStatus{
				Name:   b,
				Status: branchExist,
			})
		} else {
			synBranches = append(synBranches, branchStatus{
				Name:   b,
				Status: branchNonExist,
			})
		}
	}

	data := struct {
		URL      string
		Command  string
		User     string
		Branches []branchStatus
	}{
		URL:      url,
		Command:  strings.TrimSpace(comment),
		User:     user,
		Branches: synBranches,
	}

	replyComment, err := executeTemplate(replySyncTmpl, data)
	if err != nil {
		logger.Errorln("Execute template failed:", err)
		return
	}
	ok = bot.cli.CreatePRComment(owner, repo, number, replyComment)
	if !ok {
		logger.Errorln("Create comment failed:", err)
	} else {
		logger.Infoln("Reply sync.")
	}
}

func (bot *robot) NotePullRequest(evt *client.GenericEvent, logger *logrus.Entry) {
	org := utils.GetString(evt.Org)
	repo := utils.GetString(evt.Repo)
	number := utils.GetString(evt.Number)
	comment := utils.GetString(evt.Comment)
	user := utils.GetString(evt.Commenter)
	targetBranch := utils.GetString(evt.Base)
	state := utils.GetString(evt.State)
	title := utils.GetString(evt.Title)

	if util.MatchSyncCheck(comment) {
		logger.Infoln("Receive /sync-check command")
		bot.greeting(org, repo, number, targetBranch, logger)
		return
	}

	if util.MatchSync(comment) {
		logger.Infoln("Receive /sync command")
		switch state {
		case "opened":
			logger.Infoln("Pull request is open, just replay sync.")
			bot.replySync(evt, logger)
		case "merged":
			logger.Infoln("Pull request is merge, perform sync operation.")
			_ = bot.sync(evt, user, comment, logger)
		default:
			logger.Infoln("Ignoring unhandled pull request state.")
		}
		return
	}

	if util.MatchClose(comment) {
		logger.Infoln("Receive /close command")
		if util.MatchTitle(title) {
			bot.ClosePullRequest(evt, org, repo, number, logger)
		} else {
			logger.Infoln("Pull request not created by sync-bot, ignoring /close.")
		}
		return
	}

	logger.Infoln("Ignoring unhandled comment.")
}
