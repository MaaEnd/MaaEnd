# 开发手册 - SceneManager 参考文档

## 1. 万能跳转介绍

**SceneManager** 是 MaaEnd 中的场景导航模块，提供了一套「万能跳转」机制。

### 核心概念

**万能跳转** 的含义是：**从任意游戏界面出发，都能自动导航到目标场景**。无论用户当前处于主菜单、大世界、某个子菜单，还是加载中、弹窗中等状态，只要在 `next` 中挂载对应的场景接口节点，Pipeline 就会自动处理：

- 识别并处理弹窗（确认/取消）
- 等待加载完成
- 逐层返回或进入更基础的场景
- 最终到达目标场景

### 实现原理

SceneManager 使用 MaaFramework 的 `[JumpBack]` 机制，将场景接口组织成 **有层级的跳转链**：

- 每个场景接口的 `next` 列表中，包含「直接成功」的识别节点，以及若干「回退」节点
- 当当前路径无法识别时，会 `[JumpBack]` 到更基础的场景接口，由该接口负责先进入前置场景，再重新尝试
- 最底层是 `SceneAnyEnterWorld`（进入任意大世界），它是多数场景跳转的起点

例如，`SceneEnterMenuProtocolPass`（进入通行证菜单）的 `next` 为：

- `ScenePrivateWorldEnterMenuProtocolPass`：若已在大世界，直接进入通行证
- `[JumpBack]SceneAnyEnterWorld`：若不在大世界，先进入大世界再重试

## 2. 万能跳转使用方式

### 基本用法

在 Pipeline 任务中，将「目标场景接口」作为 `[JumpBack]` 节点放在 `next` 中。当业务节点识别失败时，框架会先执行场景跳转，到达目标场景后，再回到业务逻辑继续执行。

### 示例：一键领取通行证任务

以下示例展示如何在「从任意界面进入通行证菜单并领取奖励」的任务中使用万能跳转。

```json
{
    "DailyProtocolPassStart": {
        "pre_delay": 0,
        "post_delay": 0,
        "next": [
            "DailyProtocolPassInMenu",
            "[JumpBack]SceneEnterMenuProtocolPass"
        ]
    },
    "DailyProtocolPassInMenu": {
        "desc": "在通行证界面",
        "recognition": { ... },
        "next": [ 
            "DailyProtocolMissionsEnter",
             ... 
        ]
    },
    ...
}
```

**执行流程说明**：

1. 任务入口为 `DailyProtocolPassStart`，`next` 为 `DailyProtocolPassInMenu` 和 `[JumpBack]SceneEnterMenuProtocolPass`
2. 若当前画面已是通行证界面 → 命中 `DailyProtocolPassInMenu`，进入业务逻辑
3. 若当前不在通行证界面 → 命中 `[JumpBack]SceneEnterMenuProtocolPass`，框架会执行「进入通行证菜单」的跳转链
4. `SceneEnterMenuProtocolPass` 内部会按需调用 `SceneAnyEnterWorld` 等，先进入大世界，再打开通行证菜单
5. 进入通行证后，Pipeline 会重新从入口执行，最终命中 `DailyProtocolPassInMenu`

### 示例：进入地区建设进行倒卖

```json
{
    "ResellMain": {
        "desc": "一键倒卖主入口",
        "pre_delay": 0,
        "post_delay": 500,
        "next": [
            "ResellStageCheckArea",
            "[JumpBack]SceneEnterMenuRegionalDevelopment"
        ]
    },
    "ResellStageCheckArea": {
        "desc": "检查当前地区",
        "recognition": { ... },
        "next": [ ... ]
    },
    ...
}
```

当 `ResellStageCheckArea` 识别失败（例如当前在背包、活动等其它界面）时，会通过 `SceneEnterMenuRegionalDevelopment` 自动进入地区建设菜单，再回到 `ResellMain` 重新尝试。

## 3. 万能跳转接口约定

### 只使用 SceneInterface.json 中的接口

**请仅使用 `assets/resource/pipeline/SceneInterface.json` 内定义的场景接口节点。** 这些节点名称**不以 `ScenePrivate` 开头**。

### 禁止使用 ScenePrivate 节点

`SceneManager` 文件夹（如 `SceneCommon.json`、`SceneMenu.json`、`SceneWorld.json`、`SceneMap.json` 等）中定义的 `ScenePrivate*` 节点属于 **内部实现**，用于支撑接口的实际跳转逻辑。

- **不要**在任务 Pipeline 中直接引用 `ScenePrivate*` 节点
- 这些节点的结构、名称、逻辑都可能随版本更新而变更
- 若需某个场景能力，请查看 `SceneInterface.json` 是否已有对应接口；若没有，可提出需求

### 常用接口一览

| 分类 | 接口名 | 说明 |
|------|--------|------|
| 大世界 | `SceneAnyEnterWorld` | 从任意界面进入谷地/武陵/帝江任意一个大世界 |
| 大世界 | `SceneEnterWorldDijiang` | 进入帝江号大世界 |
| 大世界 | `SceneEnterWorldValleyIVTheHub` | 进入四号谷地-枢纽区大世界 |
| 大世界 | `SceneEnterWorldFactory` | 进入大世界工厂模式 |
| 地图 | `SceneEnterMapDijiang` | 进入帝江号地图界面 |
| 地图 | `SceneEnterMapValleyIVTheHub` | 进入四号谷地-枢纽区地图界面 |
| 菜单 | `SceneEnterMenuList` | 进入菜单总列表 |
| 菜单 | `SceneEnterMenuRegionalDevelopment` | 进入地区建设菜单 |
| 菜单 | `SceneEnterMenuEvent` | 进入活动菜单 |
| 菜单 | `SceneEnterMenuProtocolPass` | 进入通行证菜单 |
| 菜单 | `SceneEnterMenuBackpack` | 进入背包界面 |
| 菜单 | `SceneEnterMenuShop` | 进入商店界面 |
| 辅助 | `SceneDialogConfirm` | 点击对话框确认按钮 |
| 辅助 | `SceneDialogCancel` | 点击对话框取消按钮 |
| 辅助 | `SceneNoticeRewardsConfirm` | 点击奖励界面确认按钮 |
| 辅助 | `SceneWaitLoadingExit` | 等待加载界面消失 |

完整接口列表及说明请直接查看 `assets/resource/pipeline/SceneInterface.json` 中各节点的 `desc` 字段。
