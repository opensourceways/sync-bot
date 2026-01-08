package hook

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"sync-bot/git"
	"sync-bot/util"

	"github.com/opensourceways/robot-framework-lib/client"
	"github.com/opensourceways/robot-framework-lib/utils"
	"github.com/sirupsen/logrus"
)

func (bot *robot) MergePullRequest(evt *client.GenericEvent, logger *logrus.Entry) {
	org, repo, number := utils.GetString(evt.Org), utils.GetString(evt.Repo), utils.GetString(evt.Number)

	comments, ok := bot.cli.ListPullRequestComments(org, repo, number)
	if !ok {
		logger.Errorln("List PullRequest comments failed")
		return
	}
	logrus.WithFields(logrus.Fields{
		"comments": comments,
	}).Infoln("Get all comments")

	// find the last /sync command
	for _, comment := range comments {
		user := comment.Commenter
		body := comment.Body
		if util.MatchSync(body) {
			logger.Infoln("match /sync command, user: %s, body: %s", user, body)
			_ = bot.sync(evt, user, body, logger)
			return
		}
	}
	logger.WithFields(logrus.Fields{
		"comments": comments,
	}).Warnln("Not found valid /sync command in pr comments")
}

func (bot *robot) AutoMerge(evt *client.GenericEvent, org, repo, number string, logger *logrus.Entry) {
	targetBranch := utils.GetString(evt.Base)
	pr, ok := bot.cli.GetPullRequest(org, repo, number)
	prNumber, err := strconv.Atoi(number)
	if err != nil {
		logger.Errorf("Invalid pull request number: %s", number)
		return
	}
	if !ok {
		logger.Error("Get pull request failed")
		return
	}

	if !utils.GetBool(pr.MergeAble) {
		comment := "The current pull request can not be merge. "
		ok := bot.cli.CreatePRComment(org, repo, number, comment)
		if !ok {
			return
		}
		return
	}

	r, err := bot.GitClient.Clone(org, repo)
	if err != nil {
		logger.Errorf("Clone repository failed: %v", err)
		return
	}
	err = r.FetchPullRequest(prNumber)
	if err != nil {
		logger.Errorf("Fetch pull request failed: %v", err)
		return
	}
	remoteBranch := "origin/" + targetBranch
	err = r.Checkout(remoteBranch)
	if err != nil {
		logger.Errorf("Checkout %v failed: %v", remoteBranch, err)
		return
	}

	err = r.CheckoutNewBranch(targetBranch, true)
	if err != nil {
		logger.Errorf("Checkout %v failed: %v", targetBranch, err)
		return
	}
	prURL := fmt.Sprintf("origin/merge-requests/%s", number)
	err = r.Merge(prURL, git.MergeFF)
	if err != nil {
		logger.Errorf("Merge failed: %v", err)
		return
	}
	err = r.Push(targetBranch, true)
	if err != nil {
		logger.Errorf("Push %v failed: %v", targetBranch, err)
		return
	}
}

func (bot *robot) pick(org string, repo string, opt *SyncCmdOption, branchSet map[string]bool, pr client.PullRequest,
	title string, body string, firstSha string, lastSha string) ([]syncStatus, error) {
	number := utils.GetString(pr.Number)
	sourceBranch := utils.GetString(pr.Head)
	prNumber, err := strconv.Atoi(number)
	if err != nil {
		return nil, fmt.Errorf("invalid pull request number: %s", number)
	}
	var forkPath string

	r, err := bot.GitClient.Clone(org, repo)
	if err != nil {
		logrus.Errorf("Clone %s/%s failed: %v", org, repo, err)
		return nil, err
	}

	var status []syncStatus
	for _, branch := range opt.branches {
		// branch not in repository
		if ok := branchSet[branch]; !ok {
			status = append(status, syncStatus{
				Name:   branch,
				Status: branchNonExist,
			})
			continue
		}

		// pull for big repos by using upstream repos
		if org == "openEuler" && repo == "kernel" {
			bigRemote := fmt.Sprintf("%s/%s.git", "https://gitcode.com", org+"/"+repo)

			// check remote
			if hasUpstream, _ := r.ListRemote(); !hasUpstream {
				// add remote
				err = r.AddRemote(bigRemote)
				if err != nil {
					status = append(status, syncStatus{
						Name:   branch,
						Status: addRemoteFailed,
					})
					continue
				}
			}

			_ = r.Clean()

			// create branch in fork repo when it exists in origin repo but not exists in fork repo
			// get fork repo'bot branches
			forkBranches, ok := bot.cli.GetRepoAllBranch("LiYanghang00", repo)
			if !ok {
				status = append(status, syncStatus{
					Name:   branch,
					Status: GetForkRepoFailed,
				})
				continue
			}

			// create not existed branches
			forkBranchesList := make(map[string]string, len(forkBranches))
			for _, fb := range forkBranches {
				forkBranchesList[fb.Name] = fb.Name
			}

			if _, ok := forkBranchesList[branch]; !ok {
				err = r.FetchUpstream(branch)
				if err != nil {
					status = append(status, syncStatus{
						Name:   branch,
						Status: createBranchFailed,
					})
					continue
				}

				err = r.CreateBranchAndPushToOrigin(branch, fmt.Sprintf("upstream/%s", branch))
				if err != nil {
					status = append(status, syncStatus{
						Name:   branch,
						Status: err.Error(),
					})
					continue
				}
			}

			// git checkout branch
			err = r.Checkout("origin/" + branch)
			if err != nil {
				status = append(status, syncStatus{
					Name:   branch,
					Status: err.Error(),
				})
				continue
			}

			// git pull
			err = r.FetchUpstream(branch)
			if err != nil {
				status = append(status, syncStatus{
					Name:   branch,
					Status: err.Error(),
				})
				continue
			}

			err = r.MergeUpstream(branch)
			if err != nil {
				status = append(status, syncStatus{
					Name:   branch,
					Status: err.Error(),
				})
				continue
			}

			// git push
			err = r.PushUpstreamToOrigin(branch)
			if err != nil {
				status = append(status, syncStatus{
					Name:   branch,
					Status: err.Error(),
				})
				continue
			}

		} else {
			_ = r.Clean()
			err = r.Checkout("origin/" + branch)
			if err != nil {
				status = append(status, syncStatus{
					Name:   branch,
					Status: err.Error(),
				})
				continue
			}
		}

		tempBranch := fmt.Sprintf("sync-pr%v-%v-to-%v", number, sourceBranch, branch)
		err = r.CheckoutNewBranch(tempBranch, true)
		if err != nil {
			status = append(status, syncStatus{
				Name:   branch,
				Status: err.Error(),
			})
			continue
		}
		err = r.FetchPullRequest(prNumber)
		if err != nil {
			status = append(status, syncStatus{
				Name:   branch,
				Status: err.Error(),
			})
			continue
		}
		err = r.CherryPick(firstSha, lastSha, git.Theirs)
		if err != nil {
			logrus.Errorln("Cherry pick failed:", err.Error())
			status = append(status, syncStatus{
				Name:   branch,
				Status: syncFailed,
			})
			continue
		}
		err = r.Push(tempBranch, true)
		if err != nil {
			status = append(status, syncStatus{
				Name:   branch,
				Status: err.Error(),
			})
			continue
		}
		var num string
		sleepyTime := time.Second

		if org == "openEuler" && repo == "kernel" {
			tempBranch = "LiYanghang00:" + tempBranch
			forkPath = fmt.Sprintf("%s/%s", "LiYanghang00", repo)
		}

		for i := 0; i < 5; i++ {

			logrus.Infof("Create pull request: %v %v %v %v %v %v", org, repo, title, body, tempBranch, branch)

			prune := true
			newPR := client.PullRequest{
				Title:             &title,
				Body:              &body,
				Head:              &tempBranch,
				Base:              &branch,
				PruneSourceBranch: &prune,
				ForkPath:          &forkPath,
			}
			var ok bool
			num, ok = bot.cli.CreatePR(org, repo, newPR)
			if !ok {
				logrus.WithError(err).Infof("Create pull request: retrying %d times", i+1)
				time.Sleep(sleepyTime)
				sleepyTime *= 2
				continue
			}
			break
		}
		var url string
		var st string
		if err != nil {
			logrus.Errorln("Create PullRequest failed:", err)
			st = err.Error()
		} else {
			logrus.Infoln("Create PullRequest:", num)
			st = createdPR
			url = fmt.Sprintf("https://gitcode.com/%v/%v/merge_requests/%v", org, repo, num)
		}
		status = append(status, syncStatus{Name: branch, Status: st, PR: url})
	}
	return status, nil
}

func (bot *robot) merge(org string, repo string, opt *SyncCmdOption, branchSet map[string]bool, pr client.PullRequest, title string, body string) ([]syncStatus, error) {
	number := utils.GetString(pr.Number)
	ref := utils.GetString(pr.Head)

	var status []syncStatus
	for _, branch := range opt.branches {
		// branch not in repository
		if ok := branchSet[branch]; !ok {
			status = append(status, syncStatus{
				Name:   branch,
				Status: branchNonExist,
			})
			continue
		}
		// create temp branch
		tempBranch := fmt.Sprintf("sync-pr%v-to-%v", number, branch)
		ok := bot.cli.CreateRepoBranch(org, repo, tempBranch, ref)
		if !ok {
			logrus.WithFields(logrus.Fields{
				"tempBranch": tempBranch,
			}).Errorln("Create temp branch failed:")
			// TODO: check if branch exist
		} else {
			logrus.Infoln("Create temp branch:", branch)
		}
		var url string
		var st string
		var forkPath string
		prune := true
		newPR := client.PullRequest{
			Title:             &title,
			Body:              &body,
			Head:              &tempBranch,
			Base:              &branch,
			PruneSourceBranch: &prune,
			ForkPath:          &forkPath,
		}
		num, ok := bot.cli.CreatePR(org, repo, newPR)
		if !ok {
			logrus.Errorln("Create PullRequest failed")
			st = createPRFailed
		} else {
			logrus.Infoln("Create PullRequest:", num)
			st = createdPR
			url = fmt.Sprintf("https://gitcode.com/%v/%v/merge_requests/%v", org, repo, num)
		}
		status = append(status, syncStatus{Name: branch, Status: st, PR: url})
	}
	return status, nil
}

func (bot *robot) overwrite() bool {
	panic("implement me")
}

func (bot *robot) sync(evt *client.GenericEvent, user string, command string, logger *logrus.Entry) error {
	org := utils.GetString(evt.Org)
	repo := utils.GetString(evt.Repo)
	number := utils.GetString(evt.Number)

	opt, err := parseSyncCommand(command)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"opt": opt,
		}).Errorln("Parse /sync command failed:", err)
		return err
	}

	pr, ok := bot.cli.GetPullRequest(org, repo, number)
	if !ok {
		logger.Errorln("Get pull request failed")
		return errors.New("get pull request failed")
	}

	issues, ok := bot.cli.GetPRLinkedIssue(org, repo, number)
	commits, ok := bot.cli.GetPullRequestCommits(org, repo, number)
	if !ok {
		logger.Errorln("List commits failed")
		return errors.New("list commits failed")
	}
	for i := range commits {
		commits[i].Message = strings.ReplaceAll(commits[i].Message, "\n", "<br>")
	}

	// retrieve all branches
	branches, ok := bot.cli.GetRepoAllBranch(org, repo)
	if !ok {
		logger.Errorln("List branches failed")
		return errors.New("list branches failed")
	}
	branchSet := make(map[string]bool)
	for _, b := range branches {
		branchSet[b.Name] = true
	}

	title := fmt.Sprintf("[sync] PR-%v: %v", number, utils.GetString(pr.Title))

	var body string
	var data interface{}
	if org == "openEuler" && repo == "kernel" {
		data = struct {
			PR   string
			Body string
		}{
			PR:   utils.GetString(pr.URL),
			Body: utils.GetString(pr.Body),
		}

		body, err = executeTemplate(syncPRBodyTmplKernel, data)
		if err != nil {
			logger.Errorln("Execute template failed:", err)
			return err
		}
	} else {
		data = struct {
			PR      string
			Issues  []client.Issue
			Commits []client.PRCommit
		}{
			PR:      utils.GetString(pr.URL),
			Issues:  issues,
			Commits: commits,
		}

		body, err = executeTemplate(syncPRBodyTmpl, data)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"tmpl": syncPRBodyTmpl,
				"data": data,
			}).Errorln("Execute template failed:", err)
			return err
		}
	}

	var status []syncStatus
	switch opt.strategy {
	case Pick:
		firstSha := commits[len(commits)-1].SHA
		lastSha := commits[0].SHA
		status, _ = bot.pick(org, repo, opt, branchSet, pr, title, body, firstSha, lastSha)
	case Merge:
		status, _ = bot.merge(org, repo, opt, branchSet, pr, title, body)
	case Overwrite:
		bot.overwrite()
	default:
	}

	comment, err := executeTemplate(syncResultTmpl, struct {
		URL        string
		User       string
		Command    string
		SyncStatus []syncStatus
	}{
		URL:        utils.GetString(evt.HtmlURL),
		User:       user,
		Command:    strings.TrimSpace(command),
		SyncStatus: status,
	})
	if err != nil {
		logger.Errorln("Execute template failed:", err)
		return err
	}

	ok = bot.cli.CreatePRComment(org, repo, number, comment)
	if !ok {
		logger.Errorln("Create comment failed:", err)
		return err
	} else {
		logger.Infoln("Reply sync.")
	}
	return err
}

func (bot *robot) ClosePullRequest(evt *client.GenericEvent, org, repo, number string, logger *logrus.Entry) {
	sourceBranch := utils.GetString(evt.Head)

	logger.Infoln("ClosePullRequest")

	r, err := bot.GitClient.Clone(org, repo)
	if err != nil {
		logger.Errorf("Clone repo failed: %v", err)
		return
	}
	if util.MatchSyncBranch(sourceBranch) && r.RemoteBranchExists(sourceBranch) {
		err = r.DeleteRemoteBranch(sourceBranch)
		if err != nil {
			logger.Errorln("Delete source branch failed:", err)
		}
		return
	}
	logger.Warningf("Source branch %v not found.", sourceBranch)
}
