package launchcloudweb

const component = "launchcloudweb"

// Run 对应 assets/tasks/pretasks/LaunchCloudWeb.json：启动云终末地网页外壳，等它的窗口出现后交给 MXU 连接 CloudWeb 控制器。
// 外壳已在运行时直接成功；登录与进入游戏在外壳窗口中完成。
func Run(args []string) bool {
	return run()
}
