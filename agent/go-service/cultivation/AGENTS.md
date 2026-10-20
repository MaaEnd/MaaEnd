# 培养工具原生试作约定

用户于 2026-10-20 授权原生试作，并要求先完整阅读上游编码规范。当前上游基线为 `v2.35.0-beta.1`，提交 `01d31cf39ef00a8bb5f3efecdfbd309f6648a418`。本文件补充根 `AGENTS.md`，不替代上游规范。

## 动笔前必读

- 根 `AGENTS.md` 和 `docs/zh_cn/developers/coding-standards.md` 全文。
- `.agents/skills/go-service-guide/SKILL.md`、`.agents/skills/pipeline-guide/SKILL.md`、`.agents/skills/docs-guide/SKILL.md`。
- `docs/zh_cn/developers/components-guide.md`、`go-service-pkg.md`、`node-testing.md`；修改相关组件时继续读对应组件文档和实际代码。
- 升级上游基线后重新核对这些文件，不能只沿用本摘要。

## 必须落实

1. 任务和界面流程放 `assets/tasks`、Pipeline；Go 负责解析、图像算法、需求计算和持久化。SRC 的 Python、PyWebIO、AzurLaneConfig 不作为运行依赖，不把整套设备流程搬进 Go 回调。
2. 新 Pipeline 必须有真实截图、1280×720 ROI/模板及界面跳转依据。现有 2026-10-16 截图只能证明旧布局；新 beta 必须另验，不凭旧样本宣布新版本通过。
3. 每步操作前后确认界面；优先中间识别和必要的 freezes。不得靠 sleep、无界循环、重复点击或任意 max_hit 掩盖失败。不需要延迟时显式设置零值，避免协议默认等待。
4. 优先复用上游场景公共接口、IMS、SupplyPlan 和 `pkg/`，禁止引用 `__ScenePrivate*`。数值、And 解包、资源定位、输出文件分别核对 `ocrnum`、`recogtarget`、`resource`、`fsutil`；确需不同的严格语义时说明差异，不直接复制通用实现。
5. 新 Custom 同步本包 `register.go`、根 `registerAll()`、对应 Custom Schema 注册名与参数规则；类型定义处有编译期接口断言。无参数组件不创建空 Schema。
6. 回调句柄仅本回调有效；controller/resource 每回调只取一次并判空，向辅助函数传参，禁止存入跨回调状态。Job 紧接 Wait；识别未命中返回 `nil, false`。
7. zerolog 链式字段记录上下文，不拼入 Msg；导出符号写注释，保持代码可读，不堆成黑盒或分号密集的一行函数。
8. 新任务同步任务文件、interface 导入、全部语言文案；OCR expected 默认完整文本。确有证据需截断时按上游要求加 i18n-skip 与完整原文。不要自行增加 option description / Pipeline focus；用户要求可见预览时，用现有原生提示能力并明确用途。
9. 资源目录不用下划线开头；JSON/YAML 按仓库 Prettier，Go 按 gofmt；Markdown 按 docs-guide / markdownlint，中英文用户或开发文档同步。
10. 真实识别节点配正负样本；普通 maa-tools 节点测试不会启动 Agent，不能据此宣称自定义识别或完整流程已验证。变更 tests/** 时依上游运行 pnpm test，同时改 assets/schema 时运行 pnpm check；其他情况不无理由跑全套。Go 纯逻辑用定向测试，原始输出/源码绑定留档。

## 本试作契约

- 先支持简中、单账号、CloudADB；用户在游戏中选定干员和培养期望。布局未确认、材料身份/数量不明、双遍不一致或扫描不完整时保留旧计划。
- 稳定物品 ID；材料与箱子分开；同物品需求累加，共享库存只计一次。SupplyPlan 填总目标库存，经验按物品面值换算；16 项完整覆盖，未选项目显式排除，不能继承非零默认值。
- 计划保存、清空修订与库存缓存各自有明确语义；清空不能让在途旧扫描复活。运行参数应用不等于 MXU 表单已经更新和持久化。
- 本次为用户授权的独立原生试作。保留旧 SRC 和安装目录，不恢复 DS/guard，不自行启动设备/游戏或解除既有 unknown。构建、离线回放、节点测试、设备验证分别报告，正式复核状态如实记录。
- 2026-10-20用户追加调用Luna做模拟器测试，允许本次协调后的只读扫描；不扩展为消费或生产启用。prototype.1设备证据与prototype.2离线模块预览分别绑定。
- 培养完成区分材料备齐、用户确认完成与自动核验完成。手动确认必须明确标来源、干员/期望/修订/时间，不能把模块结束当核验。新扫描替换后不继承旧完成。
- 采集0为无限；未选路线明确关闭，共享路线可对应两项。培养目标模式库存同步失败应停止，不能沿用普通采集的无门控回退。参数桥接不是实际派发，当前各模块保持executable=false；禁止仅改标记启用。
