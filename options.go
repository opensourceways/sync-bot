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
package main

import (
	"flag"

	"github.com/opensourceways/robot-framework-lib/client"
	"github.com/opensourceways/robot-framework-lib/config"
	"github.com/opensourceways/robot-framework-lib/framework"
	"github.com/sirupsen/logrus"
	"sync-bot/hook"
)

type robotOptions struct {
	service config.FrameworkOptions
}

// gatherOptions gather the necessary arguments from command line for project startup.
// It save the configuration, the token etc. It will to be used for subsequent processes.
func (o *robotOptions) gatherOptions(fs *flag.FlagSet, logger *logrus.Entry, args ...string) {
	o.service.AddFlags(fs)
	_ = fs.Parse(args)
	cnf := new(hook.Configuration)
	o.service.ValidateComposite(cnf, logger)
	if o.service.Interrupt {
		return
	}

	client.SetSigInfoBaseURL(cnf.SigInfoURL)
	client.SetCommunityName(cnf.CommunityName)
	framework.SetRobotUserID(cnf.CommunityRobotID)
}
