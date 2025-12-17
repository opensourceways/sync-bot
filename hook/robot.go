// Copyright (c) Huawei Technologies Co., Ltd. 2024. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package hook

import (
	"reflect"
	"slices"

	"sync-bot/git"
	"sync-bot/util"

	"github.com/opensourceways/robot-framework-lib/client"
	"github.com/opensourceways/robot-framework-lib/config"
	"github.com/opensourceways/robot-framework-lib/framework"
	"github.com/opensourceways/robot-framework-lib/utils"
	"github.com/sirupsen/logrus"
)

// iClient is an interface that defines methods for client-side interactions
type iClient interface {
	// GetPullRequest gets a pull request in a specified organization and repository
	GetPullRequest(org, repo, number string) (result client.PullRequest, success bool)
	GetPathContent(org, repo, path, branch string) (result client.RepoContent, success bool)
	GetRepoAllBranch(org, repo string) (result []client.Branch, success bool)
	ListPullRequestComments(org, repo, number string) (result []client.PRComment, success bool)
	GetPRLinkedIssue(org, repo, number string) (result []client.Issue, success bool)
	CreatePRComment(org, repo, number, comment string) (success bool)
	CreateIssueComment(org, repo, number, comment string) (success bool)
	AddIssueLabels(org, repo, number string, labels []string) (success bool)
	RemoveIssueLabels(org, repo, number string, labels []string) (success bool)
	AddPRLabels(org, repo, number string, labels []string) (success bool)
	RemovePRLabels(org, repo, number string, labels []string) (success bool)
	GetPullRequestCommits(org, repo, number string) (result []client.PRCommit, success bool)
	GetPullRequestLabels(org, repo, number string) (result []string, success bool)
	GetIssueLabels(org, issueID string) (result []string, success bool)
	GetRepoIssueLabels(org, repo string) (result []string, success bool)
	CheckPermissionWithBranch(org, repo, username, branch string) (pass, success bool)
	GetPullRequestChanges(org, repo, number string) (result []client.CommitFile, success bool)

	CreatePR(org, repo string, prContent client.PullRequest) (number string, success bool)
	CreateRepoBranch(org, repo, createFrom, branch string) (success bool)
	CheckIfPRCreateEvent(evt *client.GenericEvent) (yes bool)
	CheckIfPRReopenEvent(evt *client.GenericEvent) (yes bool)
	CheckIfPRMergeEvent(evt *client.GenericEvent) (yes bool)
	CheckIfPRCloseEvent(evt *client.GenericEvent) (yes bool)
	CheckIfPRSourceCodeUpdateEvent(evt *client.GenericEvent) (yes bool)
}

type robot struct {
	cli       iClient
	cnf       *Configuration
	log       *logrus.Entry
	GitClient *git.Client
}

func (bot *robot) GetConfigmap() config.Configmap {
	return bot.cnf
}

func NewRobot(c *Configuration, token []byte, logger *logrus.Entry) *robot {
	cli := client.NewClient(token, logger)
	if cli == nil {
		return nil
	}
	gitClient, err := git.NewClient()
	if err != nil {
		logrus.WithError(err).Fatalf("New git client failed: %v", err)
	}
	gitClient.SetCredentials("LiYanghang00", token)
	return &robot{cli: cli, cnf: c, log: logger, GitClient: gitClient}
}

func (bot *robot) NewConfig() config.Configmap {
	return &Configuration{}
}

func (bot *robot) RegisterEventHandler(p framework.HandlerRegister) {
	p.RegisterPullRequestHandler(bot.handlePREvent)
	p.RegisterPullRequestCommentHandler(bot.handlePullRequestCommentEvent)
}

func (bot *robot) GetLogger() *logrus.Entry {
	return bot.log
}

func (bot *robot) handlePREvent(evt *client.GenericEvent, repoCnfPtr any, logger *logrus.Entry) {
	if bot == nil || bot.cli == nil {
		logger.Errorln("robot or client not initialized")
		return
	}
	org, repo, number := utils.GetString(evt.Org), utils.GetString(evt.Repo), utils.GetString(evt.Number)
	// repoCnf := reflect.ValueOf(repoCnfPtr).Interface().(*repoConfig)
	targetBranch := utils.GetString(evt.Base)
	title := utils.GetString(evt.Title)

	if bot.cnf == nil {
		logger.Infoln("Configuration is nil, ignore it.")
		return
	}
	if bot.cli.CheckIfPRCreateEvent(evt) || bot.cli.CheckIfPRReopenEvent(evt) {
		if util.MatchTitle(title) {
			logger.Infoln("Merge Pull Request which created by sync-bot, ignore it.")
		} else if util.MatchSyncBranch(utils.GetString(evt.Base)) {
			bot.AutoMerge(evt, org, repo, number, logger)
		} else {
			bot.greeting(org, repo, number, targetBranch, logger)
		}
	} else if bot.cli.CheckIfPRMergeEvent(evt) {
		if util.MatchTitle(title) {
			logger.Infoln("Merge Pull Request which created by sync-bot, ignore it.")
		} else if util.MatchSyncBranch(targetBranch) {
			logger.Infoln("Merge Pull Request to sync branch, ignore it.")
		} else {
			bot.MergePullRequest(evt, logger)
		}
	} else if bot.cli.CheckIfPRSourceCodeUpdateEvent(evt) {
		if util.MatchSyncBranch(targetBranch) {
			bot.AutoMerge(evt, org, repo, number, logger)
		} else {
			logger.Infoln("Ignoring unhandled action:", evt.Action)
		}
	} else if bot.cli.CheckIfPRCloseEvent(evt) {
		if util.MatchTitle(title) {
			bot.ClosePullRequest(evt, org, repo, number, logger)
		} else {
			logger.Infoln("Pull request not create by sync-bot, ignoring it.")
		}
	}
}

func (bot *robot) handlePullRequestCommentEvent(evt *client.GenericEvent, repoCnfPtr any, logger *logrus.Entry) {
	org, repo := utils.GetString(evt.Org), utils.GetString(evt.Repo)
	repoCnf := reflect.ValueOf(repoCnfPtr).Interface().(*repoConfig)

	if !slices.Contains(repoCnf.Repos, repo) && !slices.Contains(repoCnf.Repos, org) {
		logger.Infoln("Ignoring event for repo:", repo)
		return
	}

	bot.NotePullRequest(evt, logger)
}
