package util

import (
	"regexp"
)

var (
	// title start with [sync-bot]
	titleRegex = regexp.MustCompile(`^(\[sync-bot\]|\[sync\])`)
	// just /sync-check
	syncCheckRegex = regexp.MustCompile(`^\s*/sync-check\s*$`)
	// like "/sync new_branch branch-1.0 foo/bar"
	syncRegex = regexp.MustCompile(`^\s*/sync([ \t]+[\w\./_-]+)+\s*$`)
	// /close
	closeRegex = regexp.MustCompile(`^\s*/close\s*$`)
	// sync branch name like "sync-pr103-master-to-openEuler-20.03-LTS"
	syncBranchRegex = regexp.MustCompile(`^sync-pr[\d]+-.+-to-.+$`)
	// repo url contain secret
	secretURL = regexp.MustCompile(`^([^:]+://)[^:]+:[^@]+(@.+)$`)
)

// MatchTitle match Pull Request created by sync-bot
func MatchTitle(title string) bool {
	return titleRegex.MatchString(title)
}

// MatchSync match Sync command
func MatchSync(content string) bool {
	return syncRegex.MatchString(content)
}

// MatchSyncCheck match SyncCheck command
func MatchSyncCheck(content string) bool {
	return syncCheckRegex.MatchString(content)
}

// MatchClose match close command
func MatchClose(content string) bool {
	return closeRegex.MatchString(content)
}

// MatchSyncBranch match branch name
func MatchSyncBranch(content string) bool {
	return syncBranchRegex.MatchString(content)
}

// MatchSecretURL match secret URL
func MatchSecretURL(url string) bool {
	return secretURL.MatchString(url)
}

// DeSecret hidden secret in url
func DeSecret(url string) string {
	return secretURL.ReplaceAllString(url, "$1******:******$2")
}
