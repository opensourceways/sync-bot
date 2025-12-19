// Package git provides a client to do git operations.
package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// MergeFF MergeOption enum
const (
	MergeFF MergeOption = "--ff"
)

const repoPath = "repos"
const gitcode = "gitcode.com"

var largeRepos = map[string]bool{
	"openFuyao-test/kernel": true,
	"LiYanghang00/kernel":   true,
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
		if owner == "openFuyao-test" && repo == "kernel" {
			fullName = "Liyanghang00" + "/" + repo
		}

		remote := fmt.Sprintf("%s/%s.git", base, fullName)
		if largeRepos[owner+"/"+repo] {
			if b, err2 := retryCmd("", c.git, "clone", "--no-tags", "--single-branch", "--depth", "50", remote, dir); err2 != nil {
				out := string(b)
				if strings.Contains(out, "destination path") && strings.Contains(out, "already exists") {
					if _, e := os.Stat(filepath.Join(dir, ".git")); e == nil {
						logrus.Infof("Path exists for %s, switching to fetch.", fullName)
						if bf, ef := retryCmd(dir, c.git, "fetch"); ef != nil {
							return nil, fmt.Errorf("git fetch error: %v. output: %s", ef, string(bf))
						}
						if hb, _ := retryCmd(dir, c.git, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); len(hb) > 0 {
							h := strings.TrimSpace(string(hb))
							h = strings.TrimPrefix(h, "origin/")
							if h != "" {
								_, _ = retryCmd(dir, c.git, "checkout", "-B", h, "origin/"+h)
							}
						}
					} else {
						args := []string{"clone", "--no-tags", "--single-branch", "--depth", "50", util.DeSecret(remote), dir}
						logrus.WithFields(logrus.Fields{
							"remote": util.DeSecret(remote),
							"dir":    dir,
							"args":   args,
							"type":   "destination_exists",
						}).Errorf("Clone failed: %v, output: %s", err2, out)
						return nil, fmt.Errorf("git clone failed (destination exists), err: %v, output: %s", err2, out)
					}
				} else if strings.Contains(out, "RPC failed") || strings.Contains(out, "expected 'packfile'") {
					if b2, err3 := retryCmd("", c.git, "clone", "--no-tags", "--single-branch", "--depth", "1", remote, dir); err3 != nil {
						if _, e := os.Stat(dir); e != nil {
							_ = os.MkdirAll(dir, os.ModePerm)
						}
						if _, e := retryCmd(dir, c.git, "init"); e != nil {
							args := []string{"clone", "--no-tags", "--single-branch", "--depth", "50", util.DeSecret(remote), dir}
							logrus.WithFields(logrus.Fields{
								"remote": util.DeSecret(remote),
								"dir":    dir,
								"args":   args,
								"type":   "rpc_failed_init_error",
							}).Errorf("Clone failed: %v, output: %s", err3, string(b2))
							return nil, fmt.Errorf("git clone failed (rpc), init error: %v, output: %s", err3, string(b2))
						}
						if _, e := retryCmd(dir, c.git, "remote", "add", "origin", remote); e != nil {
							logrus.WithFields(logrus.Fields{
								"remote": util.DeSecret(remote),
								"dir":    dir,
								"type":   "rpc_failed_add_remote_error",
							}).Errorf("Add remote failed: %v", e)
							return nil, fmt.Errorf("git clone failed (rpc), add remote error: %v", e)
						}
						if bf, ef := retryCmd(dir, c.git, "fetch", "--no-tags", "--depth", "1", "origin"); ef != nil {
							logrus.WithFields(logrus.Fields{
								"remote": util.DeSecret(remote),
								"dir":    dir,
								"type":   "rpc_failed_fetch_error",
							}).Errorf("Fetch failed: %v, output: %s", ef, string(bf))
							return nil, fmt.Errorf("git fetch failed (rpc), err: %v, output: %s", ef, string(bf))
						}
					}
				} else {
					args := []string{"clone", "--no-tags", "--single-branch", "--depth", "50", util.DeSecret(remote), dir}
					logrus.WithFields(logrus.Fields{
						"remote": util.DeSecret(remote),
						"dir":    dir,
						"args":   args,
						"type":   "unknown_clone_error",
					}).Errorf("Clone failed: %v, output: %s", err2, out)
					return nil, fmt.Errorf("git clone failed, err: %v, output: %s", err2, out)
				}
			}
		} else if b, err2 := retryCmd("", c.git, "clone", "--no-tags", "--single-branch", remote, dir); err2 != nil {
			out := string(b)
			if strings.Contains(out, "destination path") && strings.Contains(out, "already exists") {
				if _, e := os.Stat(filepath.Join(dir, ".git")); e == nil {
					logrus.Infof("Path exists for %s, switching to fetch.", fullName)
					if bf, ef := retryCmd(dir, c.git, "fetch"); ef != nil {
						return nil, fmt.Errorf("git fetch error: %v. output: %s", ef, string(bf))
					}
					if hb, _ := retryCmd(dir, c.git, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); len(hb) > 0 {
						h := strings.TrimSpace(string(hb))
						h = strings.TrimPrefix(h, "origin/")
						if h != "" {
							_, _ = retryCmd(dir, c.git, "checkout", "-B", h, "origin/"+h)
						}
					}
				} else {
					_ = os.RemoveAll(dir)
					if b2, err3 := retryCmd("", c.git, "clone", "--no-tags", "--single-branch", remote, dir); err3 != nil {
						args := []string{"clone", "--no-tags", "--single-branch", util.DeSecret(remote), dir}
						logrus.WithFields(logrus.Fields{
							"remote": util.DeSecret(remote),
							"dir":    dir,
							"args":   args,
							"type":   "destination_exists_reclone_failed",
						}).Errorf("Reclone failed: %v, output: %s", err3, string(b2))
						return nil, fmt.Errorf("git clone failed, err: %v, output: %s", err3, string(b2))
					}
				}
			} else if strings.Contains(out, "RPC failed") || strings.Contains(out, "expected 'packfile'") {
				if b2, err3 := retryCmd("", c.git, "clone", "--no-tags", "--single-branch", "--depth", "1", remote, dir); err3 != nil {
					if _, e := os.Stat(dir); e != nil {
						_ = os.MkdirAll(dir, os.ModePerm)
					}
					if _, e := retryCmd(dir, c.git, "init"); e != nil {
						args := []string{"clone", "--no-tags", "--single-branch", util.DeSecret(remote), dir}
						logrus.WithFields(logrus.Fields{
							"remote": util.DeSecret(remote),
							"dir":    dir,
							"args":   args,
							"type":   "rpc_failed_init_error",
						}).Errorf("Clone failed: %v, output: %s", err3, string(b2))
						return nil, fmt.Errorf("git clone failed (rpc), init error: %v, output: %s", err3, string(b2))
					}
					if _, e := retryCmd(dir, c.git, "remote", "add", "origin", remote); e != nil {
						logrus.WithFields(logrus.Fields{
							"remote": util.DeSecret(remote),
							"dir":    dir,
							"type":   "rpc_failed_add_remote_error",
						}).Errorf("Add remote failed: %v", e)
						return nil, fmt.Errorf("git clone failed (rpc), add remote error: %v", e)
					}
					if bf, ef := retryCmd(dir, c.git, "fetch", "--no-tags", "--depth", "1", "origin"); ef != nil {
						logrus.WithFields(logrus.Fields{
							"remote": util.DeSecret(remote),
							"dir":    dir,
							"type":   "rpc_failed_fetch_error",
						}).Errorf("Fetch failed: %v, output: %s", ef, string(bf))
						return nil, fmt.Errorf("git fetch failed (rpc), err: %v, output: %s", ef, string(bf))
					}
				}
			} else {
				args := []string{"clone", "--no-tags", "--single-branch", util.DeSecret(remote), dir}
				logrus.WithFields(logrus.Fields{
					"remote": util.DeSecret(remote),
					"dir":    dir,
					"args":   args,
					"type":   "unknown_clone_error",
				}).Errorf("Clone failed: %v, output: %s", err2, out)
				return nil, fmt.Errorf("git clone failed, err: %v, output: %s", err2, out)
			}
		}
	} else if err != nil {
		return nil, err
	} else {

		if owner == "openFuyao-test" && repo == "kernel" {
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
	refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/upstream/%s", branch, branch)
	if ferr := r.fetchRefspecRobust("upstream", refspec); ferr != nil {
		logrus.WithFields(logrus.Fields{
			"dir":     r.dir,
			"branch":  branch,
			"refspec": refspec,
		}).Errorf("git fetch upstream failed: %v", ferr)
		return fmt.Errorf("git fetch upstream %s failed: %v", branch, ferr)
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
	co = r.gitCommand("push", "origin", fmt.Sprintf("refs/remotes/upstream/%s:refs/heads/%s", branch, branch))

	out, err := co.CombinedOutput()
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"dir":    r.dir,
			"branch": branch,
			"remote": "origin",
		}).Errorf("push upstream to origin failed: %v, output: %q", err, string(out))
		return fmt.Errorf("pushing failed, output: %q, error: %v", string(out), err)
	}
	return nil
}

// CreateBranchAndPushToOrigin create a branch by upstream/xx
func (r *Repo) CreateBranchAndPushToOrigin(branch, upstream string) error {
	logrus.Infof("Create new branch %s from upstream %s", branch, upstream)
	co := r.gitCommand("checkout", "-B", branch, upstream)
	b, err := co.CombinedOutput()
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"dir":      r.dir,
			"branch":   branch,
			"upstream": upstream,
		}).Warnf("initial checkout -B failed: %v, output: %s", err, string(b))
		if strings.Contains(string(b), "invalid path") {
			rev := r.gitCommand("rev-parse", upstream)
			if shaBytes, shaErr := rev.CombinedOutput(); shaErr == nil {
				sha := strings.TrimSpace(string(shaBytes))
				logrus.WithFields(logrus.Fields{
					"dir":      r.dir,
					"branch":   branch,
					"upstream": upstream,
					"sha":      sha,
				}).Warnf("checkout blocked by invalid path, pushing commit id to origin branch directly")
				po := r.gitCommand("push", "-u", "origin", fmt.Sprintf("%s:refs/heads/%s", sha, branch))
				p, perr := po.CombinedOutput()
				if perr != nil {
					logrus.WithFields(logrus.Fields{
						"dir":    r.dir,
						"branch": branch,
						"sha":    sha,
					}).Errorf("push by sha failed: %v, output: %s", perr, string(p))
					return fmt.Errorf("git push by sha to origin failed, output: %s, err: %v", string(p), perr)
				}
				return nil
			}
		}
		if !r.RemoteBranchExistsIn(strings.Split(upstream, "/")[0], branch) {
			logrus.WithFields(logrus.Fields{
				"dir":      r.dir,
				"branch":   branch,
				"upstream": upstream,
			}).Errorf("upstream branch not found")
			return fmt.Errorf("upstream branch %s not found", branch)
		}
		srcRemote := strings.Split(upstream, "/")[0]
		refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", branch, srcRemote, branch)
		if ferr := r.fetchRefspecRobust(srcRemote, refspec); ferr != nil {
			logrus.WithFields(logrus.Fields{
				"dir":      r.dir,
				"branch":   branch,
				"upstream": upstream,
			}).Errorf("fetch upstream before checkout failed: %v", ferr)
			return fmt.Errorf("create branch by upstream failed after fetch, output: %s, err: %v", string(b), ferr)
		}
		rev := r.gitCommand("rev-parse", upstream)
		if shaBytes, shaErr := rev.CombinedOutput(); shaErr == nil {
			sha := strings.TrimSpace(string(shaBytes))
			co2 := r.gitCommand("checkout", "-B", branch, sha)
			if b2, err2 := co2.CombinedOutput(); err2 != nil {
				logrus.WithFields(logrus.Fields{
					"dir":      r.dir,
					"branch":   branch,
					"upstream": upstream,
					"sha":      sha,
				}).Errorf("checkout -B with sha failed: %v, output: %s", err2, string(b2))
				return fmt.Errorf("create branch by upstream failed, output: %s, err: %v", string(b2), err2)
			}
		} else {
			logrus.WithFields(logrus.Fields{
				"dir":      r.dir,
				"branch":   branch,
				"upstream": upstream,
			}).Warnf("rev-parse upstream/%s failed: %v", branch, shaErr)
			co2 := r.gitCommand("checkout", "-B", branch, upstream)
			if b2, err2 := co2.CombinedOutput(); err2 != nil {
				if r.RemoteBranchExistsIn("origin", branch) {
					co3 := r.gitCommand("checkout", "-B", branch, "origin/"+branch)
					if b3, err3 := co3.CombinedOutput(); err3 != nil {
						logrus.WithFields(logrus.Fields{
							"dir":    r.dir,
							"branch": branch,
						}).Errorf("checkout -B from origin failed: %v, output: %s", err3, string(b3))
						return fmt.Errorf("create branch failed from origin, output: %s, err: %v", string(b3), err3)
					}
				} else {
					logrus.WithFields(logrus.Fields{
						"dir":      r.dir,
						"branch":   branch,
						"upstream": upstream,
					}).Errorf("checkout -B from upstream failed: %v, output: %s", err2, string(b2))
					return fmt.Errorf("create branch by upstream failed, output: %s, err: %v", string(b2), err2)
				}
			}
		}
	}

	po := r.gitCommand("push", "-u", "origin", branch)
	p, err := po.CombinedOutput()
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"dir":    r.dir,
			"branch": branch,
		}).Errorf("push new branch to origin failed: %v, output: %s", err, string(p))
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
	parts := strings.SplitN(commitLike, "/", 2)
	if len(parts) == 2 && (parts[0] == "origin" || parts[0] == "upstream") {
		remote := parts[0]
		branch := parts[1]
		logrus.Infof("Checkout remote branch %s/%s.", remote, branch)
		// verify remote branch exists
		if !r.RemoteBranchExistsIn(remote, branch) {
			if remote == "upstream" && r.RemoteBranchExistsIn("origin", branch) {
				remote = "origin"
			} else {
				logrus.WithFields(logrus.Fields{
					"dir":    r.dir,
					"remote": remote,
					"branch": branch,
				}).Errorf("remote branch not found before checkout")
				return fmt.Errorf("remote branch %s/%s not found", remote, branch)
			}
		}
		_ = r.DisablePartialClone()
		// explicit refspec to ensure remote-tracking reference exists locally
		refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", branch, remote, branch)
		if ferr := r.fetchRefspecRobust(remote, refspec); ferr != nil {
			if remote == "origin" && r.RemoteBranchExistsIn("upstream", branch) {
				refspec2 := fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", branch, "upstream", branch)
				if ferr2 := r.fetchRefspecRobust("upstream", refspec2); ferr2 != nil {
					logrus.WithFields(logrus.Fields{
						"dir":     r.dir,
						"remote":  remote,
						"branch":  branch,
						"refspec": refspec,
					}).Errorf("fetch remote branch before checkout failed: %v", ferr)
					return fmt.Errorf("fetch %s/%s failed: %v", remote, branch, ferr)
				}
				co := r.gitCommand("checkout", "-B", branch, "upstream/"+branch)
				if b, err := co.CombinedOutput(); err != nil {
					return fmt.Errorf("error checking out %s: %v. output: %s", commitLike, err, string(b))
				}
				return nil
			}
			logrus.WithFields(logrus.Fields{
				"dir":     r.dir,
				"remote":  remote,
				"branch":  branch,
				"refspec": refspec,
			}).Errorf("fetch remote branch before checkout failed: %v", ferr)
			return fmt.Errorf("fetch %s/%s failed: %v", remote, branch, ferr)
		}
		co := r.gitCommand("checkout", "-B", branch, remote+"/"+branch)
		if b, err := co.CombinedOutput(); err != nil {
			out := string(b)
			if strings.Contains(out, "would be overwritten by checkout") {
				logrus.WithFields(logrus.Fields{
					"dir":    r.dir,
					"remote": remote,
					"branch": branch,
				}).Warnf("checkout would overwrite local changes, attempting clean. output: %s", out)
				if err2 := r.Clean(); err2 != nil {
					logrus.WithFields(logrus.Fields{
						"dir":    r.dir,
						"remote": remote,
						"branch": branch,
					}).Errorf("clean before retry failed: %v", err2)
					return fmt.Errorf("error checking out %s: %v. output: %s", commitLike, err, out)
				}
				co2 := r.gitCommand("checkout", "-B", branch, remote+"/"+branch)
				if b2, err3 := co2.CombinedOutput(); err3 != nil {
					logrus.WithFields(logrus.Fields{
						"dir":    r.dir,
						"remote": remote,
						"branch": branch,
					}).Errorf("checkout after clean failed: %v, output: %s", err3, string(b2))
					return fmt.Errorf("error checking out %s after clean: %v. output: %s", commitLike, err3, string(b2))
				}
			} else {
				logrus.WithFields(logrus.Fields{
					"dir":    r.dir,
					"remote": remote,
					"branch": branch,
				}).Errorf("checkout remote branch failed: %v, output: %s", err, out)
				return fmt.Errorf("error checking out %s: %v. output: %s", commitLike, err, out)
			}
		}
	} else {
		co := r.gitCommand("checkout", commitLike)
		if b, err := co.CombinedOutput(); err != nil {
			out := string(b)
			if strings.Contains(out, "would be overwritten by checkout") {
				logrus.WithFields(logrus.Fields{
					"dir":        r.dir,
					"commitLike": commitLike,
				}).Warnf("checkout would overwrite local changes, attempting clean. output: %s", out)
				if err2 := r.Clean(); err2 != nil {
					logrus.WithFields(logrus.Fields{
						"dir":        r.dir,
						"commitLike": commitLike,
					}).Errorf("clean before retry failed: %v", err2)
					return fmt.Errorf("error checking out %s: %v. output: %s", commitLike, err, out)
				}
				co2 := r.gitCommand("checkout", commitLike)
				if b2, err3 := co2.CombinedOutput(); err3 != nil {
					logrus.WithFields(logrus.Fields{
						"dir":        r.dir,
						"commitLike": commitLike,
					}).Errorf("checkout after clean failed: %v, output: %s", err3, string(b2))
					return fmt.Errorf("error checking out %s after clean: %v. output: %s", commitLike, err3, string(b2))
				}
			} else {
				logrus.WithFields(logrus.Fields{
					"dir":        r.dir,
					"commitLike": commitLike,
				}).Errorf("checkout failed: %v, output: %s", err, out)
				return fmt.Errorf("error checking out %s: %v. output: %s", commitLike, err, out)
			}
		}
	}
	return nil
}

// RemoteBranchExists returns true if branch exists in heads.
func (r *Repo) RemoteBranchExists(branch string) bool {
	heads := "origin"
	logrus.Infof("Checking if branch %s exists in %s.", branch, heads)
	co := r.gitCommand("ls-remote", "--exit-code", "--heads", heads, branch)
	return co.Run() == nil
}

func (r *Repo) RemoteBranchExistsIn(remote, branch string) bool {
	logrus.Infof("Checking if branch %s exists in %s.", branch, remote)
	co := r.gitCommand("ls-remote", "--exit-code", "--heads", remote, branch)
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
		out := string(b)
		if strings.Contains(out, "invalid path") || strings.Contains(out, "used by worktree") || strings.Contains(out, "is checked out") {
			logrus.WithFields(logrus.Fields{
				"dir":    r.dir,
				"branch": branch,
			}).Warnf("checkout -b failed: %v, output: %s; falling back to branch -f from HEAD", err, out)
			rev := r.gitCommand("rev-parse", "HEAD")
			hb, he := rev.CombinedOutput()
			if he != nil {
				return fmt.Errorf("error checking out %s: %v. output: %s", branch, err, out)
			}
			sha := strings.TrimSpace(string(hb))
			br := r.gitCommand("branch", "-f", branch, sha)
			if bb, be := br.CombinedOutput(); be != nil {
				return fmt.Errorf("error creating branch %s by branch -f: %v. output: %s", branch, be, string(bb))
			}
			return nil
		}
		return fmt.Errorf("error checking out %s: %v. output: %s", branch, err, out)
	}
	return nil
}

// CherryPick cherry-pick from commits with strategyOption
func (r *Repo) CherryPick(first, last string, strategyOption StrategyOption) error {
	if err := r.ensureIdentity(); err != nil {
		return fmt.Errorf("git identity setup failed before cherry-pick: %v", err)
	}
	logrus.Infof("Cherry Pick from %s to %s.", first, last)
	rangeArg := fmt.Sprintf("%s^..%s", first, last)
	args := []string{"cherry-pick", "-x"}
	if strategyOption == Ours || strategyOption == Theirs {
		args = append(args, "-X", string(strategyOption))
	}
	args = append(args, rangeArg)
	co := r.gitCommand(args...)
	out, err := co.CombinedOutput()
	if err == nil {
		return nil
	}
	// handle conflicts: fallback to opposite strategy after abort
	outStr := string(out)
	logrus.WithFields(logrus.Fields{
		"dir":      r.dir,
		"range":    rangeArg,
		"strategy": string(strategyOption),
	}).Errorf("Cherry pick failed: %v, output: %q", err, outStr)
	// handle dirty working tree
	if strings.Contains(outStr, "would be overwritten by cherry-pick") {
		_ = r.gitCommand("cherry-pick", "--abort").Run()
		// attempt clean and retry
		if cerr := r.Clean(); cerr != nil {
			logrus.WithFields(logrus.Fields{
				"dir":   r.dir,
				"range": rangeArg,
			}).Errorf("Clean before cherry-pick retry failed: %v", cerr)
			return fmt.Errorf("cherry pick failed, output: %q, error: %v", outStr, err)
		}
		co3 := r.gitCommand(args...)
		out3, err3 := co3.CombinedOutput()
		if err3 == nil {
			return nil
		}
		outStr3 := string(out3)
		logrus.WithFields(logrus.Fields{
			"dir":      r.dir,
			"range":    rangeArg,
			"strategy": string(strategyOption),
		}).Errorf("Cherry pick retry after clean failed: %v, output: %q", err3, outStr3)
		_ = r.gitCommand("cherry-pick", "--abort").Run()
	}
	if strings.Contains(outStr, "CONFLICT") {
		_ = r.gitCommand("cherry-pick", "--abort").Run()
		var fallback StrategyOption
		if strategyOption == Ours {
			fallback = Theirs
		} else {
			fallback = Ours
		}
		logrus.WithFields(logrus.Fields{
			"dir":      r.dir,
			"range":    rangeArg,
			"strategy": string(fallback),
		}).Warnf("Retry cherry-pick with fallback strategy")
		args2 := []string{"cherry-pick", "-x", "-X", string(fallback), rangeArg}
		co2 := r.gitCommand(args2...)
		out2, err2 := co2.CombinedOutput()
		if err2 == nil {
			return nil
		}
		logrus.WithFields(logrus.Fields{
			"dir":      r.dir,
			"range":    rangeArg,
			"strategy": string(fallback),
		}).Errorf("Cherry pick retry failed: %v, output: %q", err2, string(out2))
		_ = r.gitCommand("cherry-pick", "--abort").Run()
		return fmt.Errorf("cherry pick failed after fallback, output: %q, error: %v", string(out2), err2)
	}
	return fmt.Errorf("cherry pick failed, output: %q, error: %v", outStr, err)
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
	if r.owner == "openFuyao-test" && r.repo == "kernel" {
		remote = fmt.Sprintf("https://%s:%s@%s/%s/%s", r.user, r.pass, r.host, "LiYanghang00", r.repo)
	}

	var args []string
	if force {
		args = []string{"push", "--force", remote, branch}
	} else {
		args = []string{"push", remote, branch}
	}
	out, err := retryCmd(r.dir, r.git, args...)
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
	out, err := retryCmd(r.dir, r.git, "push", remote, "--delete", branch)
	if err != nil {
		logrus.Errorf("Delete remote branch %s failed with error: %v and output: %q", branch, err, string(out))
		return fmt.Errorf("delete remote branch %s failed, output: %q, error: %v", branch, string(out), err)
	}
	return nil
}

// FetchPullRequest just fetch
func (r *Repo) FetchPullRequest(number string) error {
	logrus.Infof("Fetching %s/%s#%s.", r.owner, r.repo, number)
	refspecPull := fmt.Sprintf("+refs/pull/%s/head:refs/remotes/origin/pull/%s", number, number)
	remote := r.base + "/" + r.owner + "/" + r.repo
	if r.user != "" && r.pass != "" {
		remote = fmt.Sprintf("https://%s:%s@%s/%s/%s", r.user, r.pass, r.host, r.owner, r.repo)
	}
	if err := r.fetchRefspecRobust(remote, refspecPull); err == nil {
		return nil
	}
	logrus.Infof("Trying refs/merge-requests for PR %s.", number)

	refspecMerge := fmt.Sprintf("+refs/merge-requests/%s/head:refs/remotes/origin/merge-requests/%s", number, number)
	if err := r.fetchRefspecRobust(remote, refspecMerge); err != nil {
		return fmt.Errorf("git fetch failed for PR %s with both refs/pull and refs/merge-requests: %v", number, err)
	}
	return nil
}

func (r *Repo) Deepen(n int) error {
	co := r.gitCommand("fetch", "--deepen", strconv.Itoa(n))
	b, err := co.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git deepen %d failed, output: %s, err: %v", n, string(b), err)
	}
	return nil
}

func (r *Repo) FetchBranchDepth(branch string, depth int) error {
	co := r.gitCommand("fetch", "--no-tags", "--filter=blob:none", "--depth", strconv.Itoa(depth), "origin", branch)
	b, err := co.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git fetch branch %s depth %d failed, output: %s, err: %v", branch, depth, string(b), err)
	}
	return nil
}

func (r *Repo) Sparse(paths []string) error {
	co := r.gitCommand("sparse-checkout", "init", "--cone")
	b, err := co.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git sparse init failed, output: %s, err: %v", string(b), err)
	}
	args := append([]string{"sparse-checkout", "set"}, paths...)
	co = r.gitCommand(args...)
	b, err = co.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git sparse set failed, output: %s, err: %v", string(b), err)
	}
	return nil
}

func (r *Repo) SparseForRange(first, last string) error {
	rangeArg := fmt.Sprintf("%s^..%s", first, last)
	co := r.gitCommand("diff", "--name-only", rangeArg)
	b, err := co.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git diff --name-only %s failed: %v, output: %s", rangeArg, err, string(b))
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	pathsSet := make(map[string]struct{})
	for _, p := range lines {
		if p == "" {
			continue
		}
		d := filepath.Dir(p)
		if d == "." {
			pathsSet["."] = struct{}{}
		} else {
			pathsSet[d] = struct{}{}
		}
	}
	var paths []string
	for k := range pathsSet {
		paths = append(paths, k)
	}
	if len(paths) == 0 {
		return nil
	}
	return r.Sparse(paths)
}

// Config runs git config.
func (r *Repo) Config(key, value string) error {
	logrus.Infof("Running git config %s %s", key, value)
	if b, err := r.gitCommand("config", key, value).CombinedOutput(); err != nil {
		return fmt.Errorf("git config %s %s failed: %v. output: %s", key, value, err, string(b))
	}
	return nil
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
	getEmail := r.gitCommand("config", "--get", "user.email")
	e, ee := getEmail.CombinedOutput()
	email := strings.TrimSpace(string(e))
	if ee != nil || email == "" {
		if err := r.Config("user.email", "sync-bot@local"); err != nil {
			return err
		}
	}
	return nil
}

// Merge incorporates changes from other branch
func (r *Repo) Merge(ref string, option MergeOption) error {
	if err := r.ensureIdentity(); err != nil {
		return fmt.Errorf("git identity setup failed before merge: %v", err)
	}
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
	for i := 0; i < 5; i++ {
		prefix := []string{"-c", "http.lowSpeedLimit=1000", "-c", "http.lowSpeedTime=60", "-c", "protocol.version=2"}
		args := append(prefix, arg...)
		c := exec.Command(cmd, args...)
		c.Dir = dir
		b, err = c.CombinedOutput()
		if err != nil {
			sanitized := make([]string, len(arg))
			for i2, a := range arg {
				sanitized[i2] = util.DeSecret(a)
			}
			err = fmt.Errorf("running %q %v returned error %w with output %q", cmd, sanitized, err, string(b))
			logrus.WithError(err).Debugf("Retrying #%d, if this is not the 3rd try then this will be retried", i+1)
			time.Sleep(sleepyTime)
			sleepyTime *= 2
			continue
		}
		break
	}
	return b, err
}
