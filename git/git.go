// Package git provides a client to do git operations.
package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"sync-bot/util"
)

// StrategyOption strategy option for cherry-pick
type StrategyOption string

// StrategyOption enum
const (
	Ours   StrategyOption = "ours"
	Theirs StrategyOption = "theirs"
)

// MergeOption merge option
type MergeOption string

// MergeOption enum
const (
	MergeFF MergeOption = "--ff"
)

const repoPath = "repos"
const gitcode = "gitcode.com"

var largeRepos = map[string]bool{
	"LiYanghang00/kernel": true,
}

// Client can clone repos. It keeps a local cache, so successive clones of the
// same repo should be quick. Create with NewClient. Be sure to clean it up.
type Client struct {
	credLock sync.RWMutex
	// user is used when pushing or pulling code if specified.
	user string

	// needed to generate the token.
	tokenGenerator []byte

	// dir is the location of the git cache.
	dir string
	// git is the path to the git binary.
	git string
	// base is the base path for git clone calls.
	base string
	// host is the git host.
	host string

	// rlm protects repoLocks which protect individual repos
	// Lock with Client.lockRepo, unlock with Client.unlockRepo.
	rlm       sync.Mutex
	repoLocks map[string]*sync.Mutex
}

// NewClient returns a client
func NewClient() (*Client, error) {
	return NewClientWithHost(gitcode)
}

// NewClientWithHost creates a client with specified host.
func NewClientWithHost(host string) (*Client, error) {
	g, err := exec.LookPath("git")
	if err != nil {
		return nil, err
	}

	return &Client{
		tokenGenerator: nil,
		dir:            repoPath,
		git:            g,
		base:           fmt.Sprintf("https://%s", host),
		host:           host,
		repoLocks:      make(map[string]*sync.Mutex),
	}, nil
}

// PrewarmLargeRepos performs a full clone for known large repositories at startup if missing
func (c *Client) PrewarmLargeRepos() error {
	for fullName := range largeRepos {
		dir := filepath.Join(c.dir, fullName)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			base := c.base
			user, pass := c.getCredentials()
			if user != "" && pass != "" {
				base = fmt.Sprintf("https://%s:%s@%s", user, pass, c.host)
			}
			remote := fmt.Sprintf("%s/%s.git", base, fullName)
			logrus.WithFields(logrus.Fields{
				"remote": util.DeSecret(remote),
				"dir":    dir,
			}).Infof("Prewarm full clone (init+fetch) for large repo %s", fullName)

			if err2 := os.MkdirAll(dir, os.ModePerm); err2 != nil && !os.IsExist(err2) {
				return fmt.Errorf("mkdir for prewarm failed: %v", err2)
			}

			if _, errInit := retryCmd(dir, c.git, "init"); errInit != nil {
				return fmt.Errorf("git init failed: %v", errInit)
			}

			_, _ = retryCmd(dir, c.git, "config", "http.postBuffer", "524288000")
			_, _ = retryCmd(dir, c.git, "config", "core.compression", "0")
			_, _ = retryCmd(dir, c.git, "config", "gc.auto", "0")

			if _, errRemote := retryCmd(dir, c.git, "remote", "add", "origin", remote); errRemote != nil {
				logrus.Debugf("remote add failed (might exist): %v", errRemote)
			}

			r := &Repo{dir: dir, git: c.git, host: c.host, base: c.base, owner: strings.Split(fullName, "/")[0], repo: strings.Split(fullName, "/")[1], user: user, pass: pass}
			if errFetch := r.FetchRemoteRobust("origin"); errFetch != nil {
				logrus.WithFields(logrus.Fields{
					"remote": util.DeSecret(remote),
					"dir":    dir,
				}).Errorf("Prewarm full fetch failed: %v", errFetch)
				// fallback: if LiYanghang00/kernel fails, try openFuyao-test/kernel as mirror to populate objects
				if fullName == "LiYanghang00/kernel" {
					fallbackFull := "openEuler/kernel"
					fallbackRemote := fmt.Sprintf("%s/%s.git", base, fallbackFull)
					logrus.WithFields(logrus.Fields{
						"mirror": util.DeSecret(fallbackRemote),
						"dir":    dir,
					}).Warnf("Attempt mirror fallback for %s", fullName)
					_, _ = retryCmd(dir, c.git, "remote", "add", "mirror", fallbackRemote)
					refspec := "+refs/heads/*:refs/remotes/mirror/*"
					// fetch heads from mirror to populate objects
					if ferr := r.fetchRefspecRobust("mirror", refspec); ferr != nil {
						logrus.WithFields(logrus.Fields{
							"mirror":  util.DeSecret(fallbackRemote),
							"dir":     dir,
							"refspec": refspec,
						}).Errorf("Mirror fetch failed: %v", ferr)
						return fmt.Errorf("prewarm full fetch %s failed: %v", fullName, errFetch)
					}
					// cleanup mirror remote, keep origin
					_, _ = retryCmd(dir, c.git, "remote", "remove", "mirror")
				} else {
					return fmt.Errorf("prewarm full fetch %s failed: %v", fullName, errFetch)
				}
			}

			_, _ = retryCmd(dir, c.git, "remote", "set-head", "origin", "--auto")
			if b, errCo := retryCmd(dir, c.git, "checkout", "-f", "origin/HEAD"); errCo != nil {
				logrus.Warnf("Prewarm checkout failed (non-fatal): %v, output: %s", errCo, string(b))
			}

			_ = r.DisablePartialClone()
			logrus.Infof("repo: %s is finshed", r.repo)
		}
	}
	return nil
}

// SetCredentials sets credentials in the client to be used for pushing to
// or pulling from remote repositories.
func (c *Client) SetCredentials(user string, tokenGenerator []byte) {
	c.credLock.Lock()
	defer c.credLock.Unlock()
	c.user = user
	c.tokenGenerator = tokenGenerator
}

func (c *Client) getCredentials() (string, string) {
	c.credLock.RLock()
	defer c.credLock.RUnlock()
	return c.user, string(c.tokenGenerator)
}

func (c *Client) lockRepo(repo string) {
	c.rlm.Lock()
	if _, ok := c.repoLocks[repo]; !ok {
		c.repoLocks[repo] = &sync.Mutex{}
	}
	m := c.repoLocks[repo]
	c.rlm.Unlock()
	m.Lock()
}

func (c *Client) unlockRepo(repo string) {
	c.rlm.Lock()
	defer c.rlm.Unlock()
	c.repoLocks[repo].Unlock()
}

// Clone clones a repository.
func (c *Client) Clone(owner, repo string) (*Repo, error) {
	fullName := owner + "/" + repo
	c.lockRepo(fullName)
	defer c.unlockRepo(fullName)
	base := c.base
	user, pass := c.getCredentials()
	if user != "" && pass != "" {
		base = fmt.Sprintf("https://%s:%s@%s", user, pass, c.host)
	}
	dir := filepath.Join(c.dir, fullName)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		logrus.Infof("Cloning %s.", fullName)
		if err2 := os.MkdirAll(filepath.Dir(dir), os.ModePerm); err2 != nil && !os.IsExist(err2) {
			return nil, err2
		}

		// special for big size repos
		if owner == "openEuler" && repo == "kernel" {
			fullName = "Liyanghang00" + "/" + repo
		}

		remote := fmt.Sprintf("%s/%s.git", base, fullName)
		if b, err2 := retryCmd("", c.git, "clone", remote, dir); err2 != nil {
			return nil, fmt.Errorf("git dir clone error: %v. output: %s", err2, string(b))
		}
	} else if err != nil {
		return nil, err
	} else {

		if owner == "openEuler" && repo == "kernel" {
			return &Repo{
				dir:   dir,
				git:   c.git,
				host:  c.host,
				base:  base,
				owner: owner,
				repo:  repo,
				user:  user,
				pass:  pass,
			}, nil
		}

		// Cache hit. Do a git fetch to keep updated.
		logrus.Infof("Fetching %s.", fullName)
		if b, err := retryCmd(dir, c.git, "fetch"); err != nil {
			return nil, fmt.Errorf("git fetch error: %v. output: %s", err, string(b))
		}
	}

	return &Repo{
		dir:   dir,
		git:   c.git,
		host:  c.host,
		base:  base,
		owner: owner,
		repo:  repo,
		user:  user,
		pass:  pass,
	}, nil
}

// Repo is a clone of a git repository. Create with Client.Clone.
type Repo struct {
	// dir is the location of the git repo.
	dir string
	// git is the path to the git binary.
	git string
	// host is the git host.
	host string
	// base is the base path for remote git fetch calls.
	base string
	// owner is the organization name: "owner" in "owner/repo".
	owner string
	// repo is the repository name: "repo" in "owner/repo".
	repo string
	// user is used for pushing to the remote repo.
	user string
	// pass is used for pushing to the remote repo.
	pass string
}

// Directory exposes the location of the git repo
func (r *Repo) Directory() string {
	return r.dir
}

// Destroy deletes the repo. It is unusable after calling.
func (r *Repo) Destroy() error {
	return os.RemoveAll(r.dir)
}

func (r *Repo) gitCommand(arg ...string) *exec.Cmd {
	cmd := exec.Command(r.git, arg...)
	cmd.Dir = r.dir

	// hide secret in command arguments
	for i, a := range arg {
		arg[i] = util.DeSecret(a)
	}
	logrus.WithField("args", arg).WithField("dir", cmd.Dir).Debug("Constructed git command")
	return cmd
}

// PullUpstream git pull upstream branch
func (r *Repo) PullUpstream(branch string) error {
	co := r.gitCommand("pull", "upstream", branch)
	b, err := co.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull upstream failed, output: %s, err: %v", string(b), err)
	}

	return nil
}

// ListRemote list git branch
func (r *Repo) ListRemote() (bool, error) {
	co := r.gitCommand("remote", "-v")
	b, err := co.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("git remote failed, output: %s, err: %v", string(b), err)
	}

	if strings.Contains(string(b), "upstream") {
		return true, nil
	} else {
		return false, nil
	}
}

// AddRemote add a upstream repo
func (r *Repo) AddRemote(remotePath string) error {
	logrus.Infof("Add remote %s", remotePath)
	co := r.gitCommand("remote", "add", "upstream", remotePath)
	b, err := co.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git remote failed, output: %s, err: %v", string(b), err)
	}

	return nil
}

// FetchUpstream fetch the upstream branch to local
func (r *Repo) FetchUpstream(branch string) error {
	logrus.Infof("fetch upstream branch %s", branch)
	co := r.gitCommand("fetch", "upstream", branch)
	b, err := co.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch %s failed, output: %s, err: %v", branch, string(b), err)
	}

	return nil
}

// MergeUpstream merge the upstream branch to local
func (r *Repo) MergeUpstream(branch string) error {
	logrus.Infof("merge upstream branch %s", branch)
	co := r.gitCommand("merge", fmt.Sprintf("upstream/%s", branch))
	b, err := co.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git merge %s failed, output: %s, err: %v", branch, string(b), err)
	}

	return nil
}

// PushUpstreamToOrigin push the upstream changes to origin
func (r *Repo) PushUpstreamToOrigin(branch string) error {
	var co *exec.Cmd
	co = r.gitCommand("push", "origin", fmt.Sprintf("HEAD:%s", branch))

	out, err := co.CombinedOutput()
	if err != nil {
		logrus.Errorf("Pushing failed with error: %v and output: %q", err, string(out))
		return fmt.Errorf("pushing failed, output: %q, error: %v", string(out), err)
	}
	return nil
}

// CreateBranchAndPushToOrigin create a branch by upstream/xx
func (r *Repo) CreateBranchAndPushToOrigin(branch, upstream string) error {
	logrus.Infof("Create new branch from upstream")
	co := r.gitCommand("checkout", "-b", branch, upstream)
	b, err := co.CombinedOutput()
	if err != nil {
		return fmt.Errorf("create branch by upstream failed, output: %s, err: %v", string(b), err)
	}

	po := r.gitCommand("push", "-u", "origin", branch)
	p, err := po.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git push branch to origin failed, output: %s, err: %v", string(p), err)
	}

	return nil
}

// Status show the working tree status
func (r *Repo) Status() (string, error) {
	logrus.Infof("Workspace status")
	co := r.gitCommand("status")
	b, err := co.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("error status %v. output: %s", err, string(b))
	}
	return string(b), nil
}

// Clean clean the repo.
func (r *Repo) Clean() error {
	logrus.Infof("cancel possible intermediate state of cherry-pick")
	co := r.gitCommand("cherry-pick", "--abort")
	out, err := co.CombinedOutput()
	if err != nil {
		logrus.Warningln("cherry-pick --abort failed:", err)
	}
	logrus.Infof("reset checkout clean")
	co = r.gitCommand("reset", "--")
	out, err = co.CombinedOutput()
	if err != nil {
		return fmt.Errorf("reset failed, output: %q, error: %v", string(out), err)
	}
	co = r.gitCommand("checkout", "--", ".")
	out, err = co.CombinedOutput()
	if err != nil {
		return fmt.Errorf("checkout failed, output: %q, error: %v", string(out), err)
	}
	co = r.gitCommand("clean", "-df")
	out, err = co.CombinedOutput()
	if err != nil {
		return fmt.Errorf("clean failed, output: %q, error: %v", string(out), err)
	}
	return nil
}

// Checkout runs git checkout.
func (r *Repo) Checkout(commitLike string) error {
	logrus.Infof("Checkout %s.", commitLike)
	co := r.gitCommand("checkout", commitLike)
	if b, err := co.CombinedOutput(); err != nil {
		return fmt.Errorf("error checking out %s: %v. output: %s", commitLike, err, string(b))
	}
	wd, _ := os.Getwd()
	fmt.Println("wd:", wd)
	return nil
}

// RemoteBranchExists returns true if branch exists in heads.
func (r *Repo) RemoteBranchExists(branch string) bool {
	heads := "origin"
	logrus.Infof("Checking if branch %s exists in %s.", branch, heads)
	co := r.gitCommand("ls-remote", "--exit-code", "--heads", heads, branch)
	return co.Run() == nil
}

// CheckoutNewBranch creates a new branch and checks it out.
func (r *Repo) CheckoutNewBranch(branch string, force bool) error {
	if force {
		_ = r.DeleteBranch(branch, true)
	}
	logrus.Infof("Create and checkout %s.", branch)
	co := r.gitCommand("checkout", "-b", branch)
	if b, err := co.CombinedOutput(); err != nil {
		return fmt.Errorf("error checking out %s: %v. output: %s", branch, err, string(b))
	}
	return nil
}

// CherryPick cherry-pick from commits with strategyOption
func (r *Repo) CherryPick(first, last string, strategyOption StrategyOption) error {
	if err := r.ensureIdentity(); err != nil {
		return fmt.Errorf("git identity setup failed before cherry-pick: %v", err)
	}
	logrus.Infof("Cherry Pick from %s to %s.", first, last)
	co := r.gitCommand("cherry-pick", "-x", fmt.Sprintf("%s^..%s", first, last))
	out, err := co.CombinedOutput()
	if err != nil {
		logrus.Errorf("Cherry pick failed with error: %v and output: %q", err, string(out))
		return fmt.Errorf("cherry pick failed, output: %q, error: %v", string(out), err)
	}
	return nil
}

// CherryPickAbort abort cherry-pick
func (r *Repo) CherryPickAbort() error {
	logrus.Infof("Cherry pick abort.")
	co := r.gitCommand("cherry-pick", "--abort")
	if b, err := co.CombinedOutput(); err != nil {
		return fmt.Errorf("error cherry-pick abort: %v. output: %s", err, string(b))
	}
	return nil
}

// Push pushes over https to the provided owner/repo#branch using a password for basic auth.
func (r *Repo) Push(branch string, force bool) error {
	if r.user == "" || r.pass == "" {
		return errors.New("cannot push without credentials - configure your git client")
	}
	logrus.Infof("Pushing to '%s/%s (branch: %s)'.", r.owner, r.repo, branch)
	remote := fmt.Sprintf("https://%s:%s@%s/%s/%s", r.user, r.pass, r.host, r.owner, r.repo)

	// check if repo is one of the big repos
	if r.owner == "openEuler" && r.repo == "kernel" {
		remote = fmt.Sprintf("https://%s:%s@%s/%s/%s", r.user, r.pass, r.host, "LiYanghang00", r.repo)
	}

	var co *exec.Cmd
	if force {
		co = r.gitCommand("push", "--force", remote, branch)
	} else {
		co = r.gitCommand("push", remote, branch)
	}
	out, err := co.CombinedOutput()
	if err != nil {
		logrus.Errorf("Pushing failed with error: %v and output: %q", err, string(out))
		return fmt.Errorf("pushing failed, output: %q, error: %v", string(out), err)
	}
	return nil
}

// DeleteBranch delete branch
func (r *Repo) DeleteBranch(branch string, force bool) error {
	var co *exec.Cmd
	if force {
		co = r.gitCommand("branch", "--delete", "--force", branch)
	} else {
		co = r.gitCommand("branch", "--delete", branch)
	}
	out, err := co.CombinedOutput()
	if err != nil {
		return fmt.Errorf("delete branch %s failed, output: %q, error: %v", branch, string(out), err)
	}
	return nil
}

// DeleteRemoteBranch delete remote branch
func (r *Repo) DeleteRemoteBranch(branch string) error {
	if r.user == "" || r.pass == "" {
		return errors.New("cannot push without credentials - configure your git client")
	}
	logrus.Infof("Delete remote branch '%s/%s (branch: %s)'.", r.owner, r.repo, branch)
	remote := fmt.Sprintf("https://%s:%s@%s/%s/%s", r.user, r.pass, r.host, r.owner, r.repo)
	co := r.gitCommand("push", remote, "--delete", branch)
	out, err := co.CombinedOutput()
	if err != nil {
		logrus.Errorf("Delete remote branch %s failed with error: %v and output: %q", branch, err, string(out))
		return fmt.Errorf("delete remote branch %s failed, output: %q, error: %v", branch, string(out), err)
	}
	return nil
}

// FetchPullRequest just fetch
func (r *Repo) FetchPullRequest(number int) error {
	logrus.Infof("Fetching %s/%s#%d.", r.owner, r.repo, number)
	if b, err := retryCmd(r.dir, r.git, "fetch", r.base+"/"+r.owner+"/"+r.repo,
		fmt.Sprintf("+refs/merge-requests/%d/head:refs/remotes/origin/merge-requests/%d", number, number)); err != nil {
		return fmt.Errorf("git fetch failed for PR %d: %v. output: %s", number, err, string(b))
	}
	return nil
}

// Config runs git config.
func (r *Repo) Config(key, value string) error {
	logrus.Infof("Running git config %s %s", key, value)
	if b, err := r.gitCommand("config", key, value).CombinedOutput(); err != nil {
		return fmt.Errorf("git config %s %s failed: %v. output: %s", key, value, err, string(b))
	}
	return nil
}

// Merge incorporates changes from other branch
func (r *Repo) Merge(ref string, option MergeOption) error {
	logrus.Infof("Running git merge %v %s", option, ref)
	if b, err := r.gitCommand("merge", string(option), ref).CombinedOutput(); err != nil {
		return fmt.Errorf("git merge %s %s failed: %v. output: %s", option, ref, err, string(b))
	}
	return nil
}

// retryCmd will retry the command a few times with backoff. Use this for any
// commands that will be talking to GitHub, such as clones or fetches.
func retryCmd(dir, cmd string, arg ...string) ([]byte, error) {
	var b []byte
	var err error
	sleepyTime := time.Second
	for i := 0; i < 3; i++ {
		c := exec.Command(cmd, arg...)
		c.Dir = dir
		b, err = c.CombinedOutput()
		if err != nil {
			err = fmt.Errorf("running %q %v returned error %w with output %q", cmd, arg, err, string(b))
			logrus.WithError(err).Debugf("Retrying #%d, if this is not the 3rd try then this will be retried", i+1)
			time.Sleep(sleepyTime)
			sleepyTime *= 2
			continue
		}
		break
	}
	return b, err
}

func (r *Repo) DisablePartialClone() error {
	co := r.gitCommand("config", "--local", "remote.origin.promisor", "false")
	if b, err := co.CombinedOutput(); err != nil {
		return fmt.Errorf("disable promisor failed: %v. output: %s", err, string(b))
	}
	co = r.gitCommand("config", "--local", "--unset-all", "remote.origin.promisor")
	_, _ = co.CombinedOutput()
	co = r.gitCommand("config", "--unset-all", "remote.origin.partialclonefilter")
	_, _ = co.CombinedOutput()
	co = r.gitCommand("config", "--local", "--unset-all", "extensions.partialclone")
	_, _ = co.CombinedOutput()
	co = r.gitCommand("config", "--local", "remote.upstream.promisor", "false")
	_, _ = co.CombinedOutput()
	co = r.gitCommand("config", "--local", "--unset-all", "remote.upstream.promisor")
	_, _ = co.CombinedOutput()
	co = r.gitCommand("config", "--unset-all", "remote.upstream.partialclonefilter")
	_, _ = co.CombinedOutput()
	return nil
}

func (r *Repo) FetchRemoteRobust(remote string) error {
	if fb, ferr := retryCmd(r.dir, r.git, "fetch", "--no-tags", remote); ferr != nil {
		out := string(fb)
		if strings.Contains(out, "RPC failed") || strings.Contains(out, "expected 'packfile'") || strings.Contains(out, "504") ||
			strings.Contains(out, "invalid index-pack output") || strings.Contains(out, "promisor remote") || strings.Contains(out, "could not fetch") {
			if _, ferr2 := retryCmd(r.dir, r.git, "fetch", "--no-tags", "--depth", "1", remote); ferr2 != nil {
				_ = r.DisablePartialClone()
				if fb3, ferr3 := retryCmd(r.dir, r.git, "fetch", "--no-tags", remote); ferr3 != nil {
					logrus.WithFields(logrus.Fields{
						"dir":    r.dir,
						"remote": remote,
					}).Errorf("fetch remote robust failed: %v, output: %s", ferr3, string(fb3))
					return fmt.Errorf("fetch %s failed: %v. output: %s", remote, ferr3, string(fb3))
				}
			}
		} else {
			logrus.WithFields(logrus.Fields{
				"dir":    r.dir,
				"remote": remote,
			}).Errorf("fetch remote failed: %v, output: %s", ferr, out)
			return fmt.Errorf("fetch %s failed: %v. output: %s", remote, ferr, out)
		}
	}
	return nil
}

func (r *Repo) fetchRefspecRobust(remote, refspec string) error {
	if fb, ferr := retryCmd(r.dir, r.git, "fetch", "--no-tags", "--filter=blob:none", remote, refspec); ferr != nil {
		out := string(fb)
		if strings.Contains(out, "RPC failed") || strings.Contains(out, "expected 'packfile'") || strings.Contains(out, "504") ||
			strings.Contains(out, "invalid index-pack output") || strings.Contains(out, "promisor remote") || strings.Contains(out, "could not fetch") {
			if _, ferr2 := retryCmd(r.dir, r.git, "fetch", "--no-tags", "--depth", "1", remote, refspec); ferr2 != nil {
				_ = r.DisablePartialClone()
				if fb3, ferr3 := retryCmd(r.dir, r.git, "fetch", "--no-tags", remote, refspec); ferr3 != nil {
					logrus.WithFields(logrus.Fields{
						"dir":     r.dir,
						"remote":  remote,
						"refspec": refspec,
					}).Errorf("fetch refspec robust failed: %v, output: %s", ferr3, string(fb3))
					return fmt.Errorf("fetch %s with %s failed: %v. output: %s", remote, refspec, ferr3, string(fb3))
				}
			}
		} else {
			logrus.WithFields(logrus.Fields{
				"dir":     r.dir,
				"remote":  remote,
				"refspec": refspec,
			}).Errorf("fetch refspec failed: %v, output: %s", ferr, out)
			return fmt.Errorf("fetch %s with %s failed: %v. output: %s", remote, refspec, ferr, out)
		}
	}
	return nil
}

func (r *Repo) ensureIdentity() error {
	getName := r.gitCommand("config", "--get", "user.name")
	n, ne := getName.CombinedOutput()
	name := strings.TrimSpace(string(n))
	if ne != nil || name == "" {
		if err := r.Config("user.name", "sync-bot"); err != nil {
			return err
		}
	}
	if err := r.Config("user.email", "infra@openeuler.sh"); err != nil {
		return err
	}
	return nil
}
