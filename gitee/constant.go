package gitee

import (
	"bufio"
	"os"
)

func GetDroppedBranches() map[string]bool {
	file, err := os.Open("drop_branches.config")

	if err != nil {
		return make(map[string]bool)
	}
	defer file.Close()

	drop_branches := make(map[string]bool)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		drop_branches[scanner.Text()] = true
	}

	if err := scanner.Err(); err != nil {
		return make(map[string]bool)
	}
	return drop_branches
}
