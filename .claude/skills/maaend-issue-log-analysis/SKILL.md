---
name: maaend-issue-log-analysis
description: 分析 MaaEnd 上游仓库公开 Issue（`https://github.com/MaaEnd/MaaEnd/issues/...` 或 `#1234`）。自动抓取 issue 正文和评论中的 `MaaEnd-logs-*.zip` 附件，下载解压后从 `maa.log`、`maa.bak.log`、`go-service.log`、`mxu-tauri.log`、`mxu-web-*.log`、`mxu-agent*.log`、`config/*`、`on_error/` 中筛选关键证据，并结合 MaaEnd、MaaFramework、MXU 的代码和文档判断根因、给出修复方案，供用户在让你分析 MaaEnd issue、日志包、识别失败、任务卡死、控制器差异、Pipeline/Agent/MXU 问题时使用。
---

# MaaEnd Issue Log Analysis

## Scope

- 仅用于上游公开仓库 `https://github.com/MaaEnd/MaaEnd`。
- 输入可以是完整 issue URL，或 `#1234` 形式的 issue 编号。
- 只分析公开 issue 中可直接访问的附件。
- 如果 issue 没有 `MaaEnd-logs-*.zip`，要先明确说明证据不足，再尽量基于 issue 文本、截图、代码和文档给出初步判断。

## Workflow

1. 规范化输入。

    - `#1234` 视为 `https://github.com/MaaEnd/MaaEnd/issues/1234`
    - 如果不是 `MaaEnd/MaaEnd`，停止并说明此 skill 不适用。

2. 获取 issue 内容。

    - 优先读取 issue 页面正文和评论。
    - 提取这些信息：版本、控制器类型、任务名、预期行为、实际行为、复现步骤、维护者评论。
    - 如果维护者已经给出结论，不要直接照抄；仍要用日志和代码自行验证，再把维护者结论作为补强证据。

3. 提取日志附件链接。

    - 关注 `MaaEnd-logs-*.zip`。
    - 如果同一个 issue 有多个日志包，先看最新一次复现；如果 issue 在对比不同版本或不同控制器，再补看前面的包。

4. 下载并解压日志包。

    - 二进制 zip 不能用网页抓取工具直接读取，应使用终端下载。
    - 用 `curl -L` 或等价方式下载到仓库内临时目录，例如 `.cache/issue-logs/issue-<number>/`。
    - 解压后用文件工具读取，不要把整份大日志完整塞进回复。
    - 先列一遍解压目录，不要假定结构固定。日志包可能包含：
        - 多份 `mxu-agent-<index>-<pid>.log`
        - 多天的 `mxu-web-YYYY-MM-DD.log`
        - `maa.bak.log`
        - `config/` 目录
        - 没有 `on_error/`，或只有部分现场图

5. 建立时间线。

    - 先从 issue 文本判断“用户觉得出问题的时刻”。
    - 再把 `mxu-web-*`、`mxu-tauri.log`、`go-service.log`、`maa.log`、`mxu-agent*.log` 串成一条时间线。
    - 先用 `mxu-tauri.log` 或 `maa.log` 找本次提交的 `task_id`，因为一个日志包里经常混有很多历史运行。
    - 如果有 `on_error/` 截图，用它校验当时实际停留画面；如果没有，要检查是否是未触发 `on_error`，还是日志导出因体积限制把图片截断了。

6. 回溯到代码和文档。
    - 任务入口、节点名、控制器限制先看 MaaEnd 仓库。
    - Pipeline 运行语义不确定时查 MaaFramework 文档。
    - MXU 行为或日志分层不确定时，先查 MXU README / 文档；只有文档不足或证据已指向实现层时才看源码。
    - 输出给用户时，如果提到任务入口、任务说明、选项名、提示文案，先到 `assets/misc/locales/zh_cn.json` 查中文文案，不要直接把 `task id` / `option id` 当成最终展示文本。

7. 只有在满足条件时才下钻第三方仓库。

    - 先用 issue、日志、MaaEnd 仓库和 MaaFramework 文档做初步归因。
    - 如果怀疑问题在 MXU、MaaFramework 或 binding 实现层，且现有证据不足以确认，再按需查看对应上游仓库源码。
    - 常见对应关系：
        - `MXU`：`https://github.com/MistEO/MXU`，前端配置、Tauri 后端编排、实例/任务/agent 生命周期、`mxu-tauri.log` / `mxu-web-*`
        - `MaaFramework`：`https://github.com/MaaXYZ/MaaFramework`，Pipeline 运行时、控制器、资源加载、任务调度、`maa.log`
        - `maa-framework-rs`：`https://github.com/MaaXYZ/maa-framework-rs`，Rust binding / FFI / `maa_ffi` 回调桥接
        - `maa-framework-go`：`https://github.com/MaaXYZ/maa-framework-go`，Go binding / Go 与 MaaFramework 的桥接
    - 只看真正相关的仓库；本地没有时再 clone 到临时目录，例如 `.cache/upstream-src/<repo>/`。

## Log Map

### `maa.log`

- 模块归属：`MaaFramework` 核心运行时。
- 主要内容：资源加载、控制器连接、任务启动、节点识别、动作执行、超时、回调细节、C++ 源文件和函数名。
- 最适合看：
    - Pipeline 是否按预期推进
    - `next` / `on_error` 是否命中
    - 识别算法细节（模板分数、OCR 结果、动作成功失败）
    - 控制器、截图、资源加载层面的异常
- 这是分析 Pipeline/识别/动作问题时的主证据之一。

### `maa.bak.log`

- 模块归属：`MaaFramework` 旧滚动日志。
- 主要内容：和 `maa.log` 同类，但通常是更早一批运行。
- 最适合看：
    - 当前 `maa.log` 不够长，复现发生在更早时间
    - 用户在 issue 里对比“前一次失败 / 后一次成功”
- 不要把 `maa.bak.log` 里的旧结论误判成最新一次复现。

### `go-service.log`

- 模块归属：`agent/go-service`。
- 主要内容：Go 侧结构化日志，包含 agent 启动、注册组件、HDR 检查、分辨率检查、自定义识别器/动作/业务扩展日志。
- 最适合看：
    - Go 自定义逻辑是否触发
    - 环境检查是否报错或预警
    - 哪个 Go 包在输出异常
- 对 Go 扩展逻辑问题，这是主证据。

### `mxu-tauri.log`

- 模块归属：MXU 的 Tauri/Rust 后端。
- 主要内容：实例创建、控制器连接、资源加载、任务开始/停止、agent 生命周期、`maa_ffi` 回调、后端命令调用。
- 最适合看：
    - MXU 是否正确把 UI 操作转成 MaaFramework/Agent 调用
    - 任务有没有被用户手动停止
    - agent 是否成功启动、断开、退出
    - 回调层是否出现重复循环或状态错乱
- 对 UI 编排、实例状态、agent 生命周期问题，这是主证据。

### `mxu-web-YYYY-MM-DD.log`

- 模块归属：MXU 的 React/TypeScript 前端。
- 主要内容：`interface.json` 加载、配置读写、任务列表解析、控制器选择、启动参数、高层 UI 流程日志。
- 最适合看：
    - UI 是否正确加载项目配置
    - 用户点击后前端提交了什么任务和配置
    - 配置持久化或界面状态问题
- 注意：
    - 日志包里可能有多天前端日志，要优先看 issue 当天、复现前后的日期。
- 对“界面选项没生效”“前端显示和实际行为不一致”这类问题很有用。

### `mxu-agent*.log`

- 模块归属：MXU 捕获到的 agent 子进程标准输出/标准错误。
- 主要内容：子进程控制台输出、MaaFramework 输出到 stdout 的内容、以及面向 MXU 运行日志面板的 HTML / 纯文本消息。
- 最适合看：
    - 用户在 MXU 运行日志面板里实际看到了什么
    - 子进程 stdout/stderr 有没有明显报错
    - 自定义逻辑是否打印了辅助信息
- 注意：
    - 新版日志包里可能不是单个 `mxu-agent.log`，而是多份 `mxu-agent-<index>-<pid>.log`。
    - 要结合 issue 时间、agent 启动时间、`mxu-tauri.log` 中的实例/agent 生命周期来判断哪一份是本次复现。
- 注意：它很有用，但不是最权威的根因日志。涉及具体运行细节时，优先以 `maa.log` 和 `go-service.log` 为准。

### `config/*`

- 模块归属：MXU 配置快照。
- 常见文件：
    - `config/mxu-MaaEnd.json`
    - `config/maa_option.json`
- 最适合看：
    - 实际启用了哪些任务和选项
    - 是否开启 `save_on_error`
    - 用户 issue 中说的配置，是否真和本次日志一致
- 当“用户口述的任务配置”和日志行为对不上时，这一层很关键。

### `on_error/`

- 模块归属：MaaFramework 错误现场截图。
- 主要内容：任务出错或进入 `on_error` 时保存的现场图。
- 最适合看：
    - 实际停留界面
    - 模板/OCR 是否根本识别错了画面
    - 是不是被弹窗、加载态、遮罩、分辨率、HDR 等环境因素干扰
- 当日志和 issue 文字描述冲突时，优先相信现场截图。
- 但要注意：
    - 没有 `on_error/` 不一定代表没出错，也可能是本次没有走到 `on_error`
    - 也可能是导出日志时图片被体积限制截断了，需要去 `mxu-tauri.log` 或同层日志里找“图片已截断”之类提示

## How To Filter Evidence

1. 先从 issue 文本拿到这几个锚点：

    - 版本
    - 控制器类型
    - 任务名 / 入口名
    - 用户说“卡住 / 点错 / 识别失败”的画面
    - 如果日志流程和当前主线代码不一致，先确认用户版本，必要时切到对应 tag（例如 `git checkout vXXX`）复核旧逻辑

2. 再从日志里找这几类高价值信号：

    - `Tasker.Task.Starting` / `Succeeded` / `Failed`
    - `Node.Recognition.Failed` 连续重复
    - `Node.Action.Failed`
    - `timeout`
    - `Warn` / `Error` / `Fatal`
    - agent 启动失败、断连、被停止

3. 先锁定“这一次复现”的任务实例，再看细节：

    - 从 `mxu-tauri.log` 找 `post_task returned task_id`
    - 再到 `maa.log` 用这个 `task_id` 跟完整个任务
    - 如果 issue 文本说“失败”，但目标 `task_id` 实际 `Tasker.Task.Succeeded`，要明确写出“本日志未复现用户描述的失败”

4. 对 Pipeline 问题，重点看：

    - `maa.log`
    - `mxu-tauri.log` 里的 `maa_ffi` 回调

5. 对 Go 扩展问题，重点看：

    - `go-service.log`
    - `mxu-agent*.log`

6. 对 UI / 配置 / 编排问题，重点看：

    - `mxu-web-*`
    - `mxu-tauri.log`
    - `config/*`

7. 回答时只保留关键片段。
    - 只摘足够支撑结论的几十行，不要倾倒整份日志。

## Common Patterns

- `next` 列表中的识别连续失败直到超时：

    - 常见于当前画面不在预期分支中、模板/OCR 失配、漏了中间节点、被弹窗打断、控制器资源分支不对。

- 某个“兜底返回/退出”节点连续成功，但流程没有前进：

    - 常见于 Pipeline 对当前状态判断错了，或者退回动作本身就是错误行为。
    - 也可能是某控制器上该任务根本不支持，导致一直落入通用回退路径。

- issue 文字说“卡死/误点”，但对应 `task_id` 最终 `Tasker.Task.Succeeded`：

    - 先明确“本次日志没有复现出用户描述的问题”。
    - 再区分两种情况：
        - 日志只是一次成功样本，不能证明 issue 不存在
        - 代码流程里仍可能存在脆弱点，需要作为“潜在设计风险”单独说明

- 用户日志里的任务流程与当前主线代码明显不一致，且当前代码看起来已经修掉了该问题：

    - 先确认用户版本，必要时切到对应 tag（例如 `git checkout vXXX`）核对旧逻辑。
    - 不要用当前分支否定旧日志；旧版本问题可能真实存在。
    - 如果主线已修复，再看修复 commit 是否已进入 tag / release：已发版建议升级，未发版建议等待 release。

- 奖励弹窗相关节点看起来“识别成功并点击成功”，但父流程没有再次验证弹窗真的消失：

    - 这是很常见的脆弱点。
    - 例如某个 `Scene*Confirm` 节点只做“识别确认按钮 -> 点击 -> 返回父节点”，没有后续场景确认。
    - 这种情况下，如果本次日志没有失败，不要直接说“已证实这里就是根因”；应写成“与 issue 描述一致的潜在风险点”。

- `go-service.log` 只有 HDR / 分辨率告警，没有明确错误：

    - 这些更像环境风险提示，不能自动当成根因。
    - 需要和 `maa.log` 里的识别结果、`on_error` 截图一起判断。

- `mxu-agent*.log` 里有 HTML 提示，但 `go-service.log` 没有对应错误：

    - 说明这可能是面向用户展示的提示，不等于流程失败点本身。

- `maa.bak.log` 里有同一入口、同类参数的历史成功样本，而本次 `maa.log` 失败：
    - 这是判断“行为回归”或“某次改动引入脆弱性”的高价值证据。
    - 回答时要明确：这是同配置历史成功对比，还是只是相似场景的旁证。

## Correlating With Code

### MaaEnd

- 任务入口、控制器限制：

    - `assets/tasks/*.json`
    - `assets/interface.json`

- Pipeline 节点：

    - `assets/resource/pipeline/**/*.json`
    - `assets/resource_adb/pipeline/**/*.json`

- Go 扩展逻辑：
    - `agent/go-service/**`

### MaaFramework

- Pipeline 执行语义、`next` / `on_error` / `timeout` / 动作语义：

    - `https://github.com/MaaXYZ/MaaFramework/raw/refs/heads/main/docs/en_us/3.1-PipelineProtocol.md`

- `interface.json` / agent / controller 语义：
    - `https://github.com/MaaXYZ/MaaFramework/raw/refs/heads/main/docs/en_us/3.3-ProjectInterfaceV2.md`

### MXU

- 日志分层、GUI 角色、Tauri + React 架构：
    - `https://raw.githubusercontent.com/MistEO/MXU/main/README.md`

## Localized Copy

- 总结任务、选项、界面提示时，优先使用 `assets/misc/locales/zh_cn.json` 中的中文文案。
- 常见查找顺序：
    - 任务名：`task.<TaskId>.label`
    - 任务描述：`task.<TaskId>.description`
    - 通用选项名：`option.<OptionId>.label`
    - 通用选项描述：`option.<OptionId>.description`
    - 任务内选项：`task.<TaskId>.option.<OptionId>.label` / `.description`
    - 任务运行提示或状态文案：优先按完整 key 查，例如 `task.<TaskId>.*`
- 输出时优先写中文，必要时在括号里补原始 id，例如 `📅日常奖励领取（DailyRewards）`。
- 如果 `zh_cn.json` 没有对应 key，再退回原始 id 或代码里的英文字符串，并明确说明“未在 `zh_cn.json` 找到对应文案”。

## Linking Code Evidence

- 如果要指向具体代码行，不要写本地路径加行号，也不要写绝对路径。
- 统一给出对应仓库的远端 GitHub `blob` 行号链接。
- MaaEnd 仓库链接格式：
    - `https://github.com/MaaEnd/MaaEnd/blob/<commit>/<path>#L14-L20`
- `<commit>` 必须是本次分析实际依据的代码版本：
    - 默认使用当前检出的 `HEAD`
    - 如果为了复核旧 issue 切到了某个 tag / commit，就使用那个版本解析后的 SHA
- 例子：
    - `https://github.com/MaaEnd/MaaEnd/blob/f2de4c61367ad03d4d8a13ce823139c6237f2a55/assets/resource/pipeline/Common/Button.json#L14-L20`
- 如果引用的是 `MXU`、`MaaFramework` 或其他上游仓库，也用对应仓库的远端 `blob` 链接，而不是本地文件行号。

## Example Heuristic

如果 issue 指向“拜访好友在 ADB 上卡死”，而日志里同时出现：

- 控制器是 `ADB`
- 任务入口是 `FriendVisitMain`
- 流程反复命中类似 `ScenePrivateAnyExit` 的回退节点
- 代码里 `assets/tasks/VisitFriends.json` 明确只允许 `Win32-*`
- `assets/resource_adb/pipeline/VisitFriends.json` 开头直接写着“该 adb 流水线逻辑无效，请勿使用”

那么根因更可能是“ADB 路径不受支持 / 旧逻辑无效”，而不是某一张模板图单点失效。

## Output Format

最终回答用这个结构：

```markdown
## Issue 概要

- issue：`#1234`
- 版本 / 控制器 / 任务：优先写 `zh_cn` 中文任务名，必要时补 task id
- 任务描述 / 相关选项：优先写 `assets/misc/locales/zh_cn.json` 中的中文 `label` / `description`
- 用户现象：

## 关键证据

- `maa.log`：...
- `go-service.log`：...
- `mxu-tauri.log`：...
- `mxu-web-*.log`：...
- `mxu-agent.log` / `on_error`：...
- 代码依据：如需指向具体实现，直接附远端 GitHub 行号链接

## 根因判断

- 直接结论：
- 证据链：

## 修复方案

1. 代码 / Pipeline / 配置层修复
2. 需要补充的测试或日志
3. 如问题本身属于不支持场景，给出应如何限制入口或改进提示

## 给用户的建议

- 用户现在可以直接尝试的动作：
- 是否建议升级 / 重下完整包 / 同步资源 / 重置配置：
- 是否需要等待开发者修复：
- 是否有临时绕过方案：

## 给 AI 的建议（可复制）

给修复 AI 的指令，要求可直接执行。

~~~text
已确认事实：
- ...

已确认根因：
- ...

请按下面要求修复：
1. 优先修改这些文件：...
2. 目标改动：...
3. 不要采用这些修法：...
4. 回归验证：...
5. 如果暂时无法彻底修复，至少补上：...
~~~

## 置信度

- 高 / 中 / 低
- 还缺什么证据
```

## Reminders

- 不要只看一个日志文件下结论。
- 不要把“维护者评论”当成唯一证据。
- 不要把环境告警自动等同于根因。
- 如果 issue 版本很旧，要明确区分“当时的根因”和“当前分支是否已修复”。
- 如果用户日志与当前代码不一致，先按用户版本 tag 复核；若确认已修，再看修复是否已进入 tag / release：已发版建议升级，未发版建议等待 release。
- 如果结论是“功能不支持”，必须给出代码级依据，例如任务控制器白名单、无效的 ADB pipeline、缺失的控制器分支或文档限制。
- 如果回答里出现任务名、任务描述、选项名、提示文案，优先使用 `assets/misc/locales/zh_cn.json` 的中文文案；必要时才在括号里补原始 id。
- 如果回答里引用了具体代码行，直接给远端 GitHub `blob` 行号链接，不要给本地路径加行号。
- 如果日志和 issue 文字描述不一致，必须显式说明“证据未复现”还是“证据已复现但用户表述不精确”。
- 如果证据表明问题已在新版本修复，明确建议用户升级；如果怀疑安装包、资源文件或配置损坏，明确建议重新下载或重建；如果判断为真实代码缺陷且暂无 workaround，明确建议等待开发者修复。
