package main

import "github.com/MaaXYZ/MaaEnd/agent/go-service/mapoverview"

// beforeStart 集中启动辅助功能，在框架初始化和组件注册完成后、AgentServer 启动前调用一次。
func beforeStart() {
	// 使用在线坐标更新地图总览的点击位置。
	mapoverview.Start()
}
