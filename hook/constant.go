package hook

const (
	branchExist        = "当前 PR 合并后，将创建同步 PR"
	branchNonExist     = "目标分支不存在，忽略处理"
	createdPR          = "创建同步 PR"
	syncFailed         = "同步失败：请手动创建 PR 进行同步，我们会继续完善分支之间同步操作，尽量避免同步失败的情况"
	GetForkRepoFailed  = "获取 fork 仓库失败"
	addRemoteFailed    = "添加远程失败"
	createBranchFailed = "创建分支失败"
	checkoutFailed     = "切换分支失败"
	pullFailed         = "拉取失败"
	mergeFailed        = "合并失败"
	pushFailed         = "推送失败"
	createPRFailed     = "创建 PR 失败"
)
