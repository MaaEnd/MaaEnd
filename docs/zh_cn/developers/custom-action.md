<!-- markdownlint-disable MD060 -->

# 开发手册 - Custom 自定义动作参考

`Custom` 是 Pipeline 中用于调用 **自定义动作** 的通用节点类型。  
具体逻辑由项目侧通过 `MaaResourceRegisterCustomAction` 注册（如 `agent/go-service` 中的实现），Pipeline 仅负责 **传参与调度**。

与普通点击、识别节点不同，`Custom` 不限定具体行为——  
只要在资源加载阶段完成注册，就可以在任意 Pipeline 中以统一的方式调用，例如：

- 执行一次截图并保存到本地。
- 进行复杂的多步交互（长按、拖拽、组合键等）。
- 做一些统计、日志或埋点上报。

---

<!-- markdownlint-enable MD060 -->
