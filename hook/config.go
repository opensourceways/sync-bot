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
	"github.com/opensourceways/robot-framework-lib/config"
)

// Configuration holds a list of repoConfig configurations.
type Configuration struct {
	ConfigItems []repoConfig `json:"config_items,omitempty"`
	// Community name used as a request parameter to getRepoConfig sig information.
	LabelUsageDescriptionMap map[string]*LabelUsageDescription `json:"-"`
	DropBrancher             []string                          `json:"drop_brancher"`
	// Sig information url.
	SigInfoURL string `json:"sig_info_url" required:"true"`
	// Community name used as a request parameter to getRepoConfig sig information.
	CommunityName    string `json:"community_name" required:"true"`
	CommunityRobotID string `json:"community_robot_id"`
}

type LabelUsageDescription struct {
	LabelName   string `json:"label_name" required:"true"`
	Description string `json:"description" required:"true"`
}

// Validate to check the configmap data's validation, returns an error if invalid
func (c *Configuration) Validate() error {
	err := config.ValidateRequiredConfig(*c)
	if err != nil {
		return err
	}
	err = config.ValidateConfigItems(c.ConfigItems)
	if err != nil {
		return err
	}
	return nil
}

// repoConfig is a Configuration struct for a organization and repository.
// It includes a RepoFilter and a boolean value indicating if an issue can be closed only when its linking PR exists.
type repoConfig struct {
	// Repos are either in the form of org/repos or just org.
	Repos []string `json:"repos" required:"true"`
	// ExcludedRepos are in the form of org/repo.
	ExcludedRepos []string `json:"excluded_repos,omitempty"`
	// LegalOperator means who can add or remove labels legally
	LegalOperator string `json:"legal_operator"  required:"true"`
}

type freezeFile struct {
	Owner  string `json:"owner" required:"true"`
	Repo   string `json:"repo" required:"true"`
	Branch string `json:"branch" required:"true"`
	Path   string `json:"path" required:"true"`
}

type branchKeeper struct {
	Owner  string `json:"owner" required:"true"`
	Repo   string `json:"repo" required:"true"`
	Branch string `json:"branch" required:"true"`
}
