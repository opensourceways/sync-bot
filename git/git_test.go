package git

import (
	"fmt"
	"strings"
	"testing"
)

func TestCherryPick(t *testing.T) {

	c, err := NewClient()
	if err != nil {
		t.Fatalf("New Client failed: %v", err)
	}

	owner := "open-euler"
	repo := "syncbot-example"
	r, err := c.Clone(owner, repo)
	if err != nil {
		t.Fatalf("Clone %v/%v failed: %v", owner, repo, err)
	}

	status, err := r.Status()
	if err != nil {
		t.Fatalf("Status %v/%v failed: %v", owner, repo, err)
	}
	fmt.Println("status:", status)

	if strings.Contains(status, "cherry-pick") {
		fmt.Println("Abort uncompleted cherry-pick")
		_ = r.CherryPickAbort()
	}

	pr := 41
	originalBranch := "master"
	targetBranch := "branch1"

	err = r.Checkout("origin/" + targetBranch)
	if err != nil {
		t.Fatalf("Checkout branch %v failed: %v", targetBranch, err)
	}
	tempBranch := fmt.Sprintf("sync-pr%v-%s-to-%s", pr, originalBranch, targetBranch)

	err = r.CheckoutNewBranch(tempBranch, true)
	if err != nil {
		t.Fatalf("Checkout new branch %v failed: %v", targetBranch, err)
	}

	err = r.FetchPullRequest(pr)
	if err != nil {
		t.Fatalf("Fetch pull request %v failed: %v", pr, err)
	}

	err = r.CherryPick("3d43f2fc", "43e0edbf", "ours")
	if err != nil {
		t.Fatalf("Fetch pull request %v failed: %v", pr, err)
	}

	//err = r.DeleteRemoteBranch("hello")
	//if err != nil {
	//	t.Logf("Fetch pull request %v failed: %v", pr, err)
	//}
	//
	//err = r.Push(tempBranch, true)
	//if err != nil {
	//	t.Fatalf("Fetch pull request %v failed: %v", pr, err)
	//}
}
