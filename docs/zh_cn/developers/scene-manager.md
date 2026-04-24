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
- 在当前路径无法识别时，会 `[JumpBack]` 到更基础的场景接口，由该接口负责先进入前置场景，再重新尝试
- 最底层是 `SceneAnyEnterWorld`（进入任意大世界），它是多数场景跳转的起点

例如，`SceneEnterMenuProtocolPass`（进入通行证菜单）的 `next` 为：

- `__ScenePrivateWorldEnterMenuProtocolPass`：若已在大世界，直接进入通行证
- `[JumpBack]SceneAnyEnterWorld`：若不在大世界，先进入大世界再重试

## 2. 万能跳转使用方式

### 基本用法

在 Pipeline 任务中，将「目标场景接口」作为 `[JumpBack]` 节点放在 `next` 中。当业务节点识别失败时，框架会先执行场景跳转，到达目标场景后，再回到业务逻辑继续执行。

### 示例

具体用法示例请参考 `assets/resource/pipeline/Interface/Example/Scene.json`，其中包含普通场景接口与传送接口的完整示例节点。

## 3. 万能跳转接口约定

### 只使用 Interface 中的场景接口

**请仅使用 `assets/resource/pipeline/Interface` 目录下各 `SceneXXX.json` 内定义的场景接口节点。** 这些节点名称**不以 `__ScenePrivate` 开头**。

### 禁止使用 \_\_ScenePrivate 节点

`SceneManager` 文件夹（如 `SceneCommon.json`、`SceneMenu.json`、`SceneWorld.json`、`SceneMap.json` 等）中定义的 `__ScenePrivate*` 节点属于 **内部实现**，用于支撑接口的实际跳转逻辑。

- **不要**在任务 Pipeline 中直接引用 `__ScenePrivate*` 节点
- 这些节点的结构、名称、逻辑都可能随版本更新而变更
- 若需某个场景能力，请查看 `Interface` 目录下各 `SceneXXX.json` 是否已有对应接口；若没有，可提出需求

### 常用接口一览

| 分类   | 接口名                              | 说明                                       |
| ------ | ----------------------------------- | ------------------------------------------ |
| 大世界 | `SceneAnyEnterWorld`                | 从任意界面进入谷地/武陵/帝江任意一个大世界 |
| 大世界 | `SceneEnterWorldDijiang`            | 进入帝江号大世界                           |
| 大世界 | `SceneEnterWorldValleyIVTheHub`     | 进入四号谷地-枢纽区大世界                  |
| 大世界 | `SceneEnterWorldFactory`            | 进入大世界工厂模式                         |
| 地图   | `SceneEnterMapDijiang`              | 进入帝江号地图界面                         |
| 地图   | `SceneEnterMapValleyIVTheHub`       | 进入四号谷地-枢纽区地图界面                |
| 菜单   | `SceneEnterMenuList`                | 进入菜单总列表                             |
| 菜单   | `SceneEnterMenuRegionalDevelopment` | 进入地区建设菜单                           |
| 菜单   | `SceneEnterMenuEvent`               | 进入活动菜单                               |
| 菜单   | `SceneEnterMenuProtocolPass`        | 进入通行证菜单                             |
| 菜单   | `SceneEnterMenuBackpack`            | 进入背包界面                               |
| 菜单   | `SceneEnterMenuShop`                | 进入商店界面                               |
| 辅助   | `SceneDialogConfirm`                | 点击对话框确认按钮                         |
| 辅助   | `SceneDialogCancel`                 | 点击对话框取消按钮                         |
| 辅助   | `SceneNoticeRewardsConfirm`         | 点击奖励界面确认按钮                       |
| 辅助   | `SceneWaitLoadingExit`              | 等待加载界面消失                           |

## 协议传送点接口

### 四号谷地

| 接口名                                             | 说明                                                 |
| -------------------------------------------------- | ---------------------------------------------------- |
| `SceneEnterMapValleyIVTheHub`                      | 从任意界面进入四号谷地-枢纽区地图界面                |
| `SceneEnterMapValleyIVValleyPass`                  | 从任意界面进入四号谷地-谷地通道地图界面              |
| `SceneEnterMapValleyIVOriginiumSciencePark`        | 从任意界面进入四号谷地-源石研究园地图界面            |
| `SceneEnterMapValleyIVAburreyQuarry`               | 从任意界面进入四号谷地-阿伯莉采石场地图界面          |
| `SceneEnterMapValleyIVOriginLodespring`            | 从任意界面进入四号谷地-矿脉源区地图界面              |
| `SceneEnterMapValleyIVPowerPlateau`                | 从任意界面进入四号谷地-供能高地地图界面              |
| `SceneEnterWorldValleyIVTheHub`                    | 从任意界面进入四号谷地-枢纽区大世界                  |
| `SceneEnterWorldValleyIVTheHub1`                   | 从任意界面进入四号谷地-枢纽区-旧供水设施东侧大世界   |
| `SceneEnterWorldValleyIVTheHub2`                   | 从任意界面进入四号谷地-枢纽区-岩丘通道大世界         |
| `SceneEnterWorldValleyIVOriginiumSciencePark0`     | 从任意界面进入四号谷地-源石研究园-矿脉源区入口大世界 |
| `SceneEnterWorldValleyIVOriginiumSciencePark1`     | 从任意界面进入四号谷地-源石研究园-源石实验园区大世界 |
| `SceneEnterWorldValleyIVOriginiumSciencePark2`     | 从任意界面进入四号谷地-源石研究园-山道缓坡大世界     |
| `SceneEnterWorldValleyIVOriginiumSciencePark3`     | 从任意界面进入四号谷地-源石研究园-五号公路大世界     |
| `SceneEnterWorldValleyIVOriginiumSciencePark4`     | 从任意界面进入四号谷地-源石研究园-醇化合物工厂大世界 |
| `SceneEnterWorldValleyIVOriginiumScienceParkInfra` | 从任意界面进入四号谷地-源石研究园-基建前站大世界     |
| `SceneEnterWorldValleyIVOriginLodespring0`         | 从任意界面进入四号谷地-矿脉源区-源石隧道大世界       |
| `SceneEnterWorldValleyIVOriginLodespring1`         | 从任意界面进入四号谷地-矿脉源区-运输区大世界         |
| `SceneEnterWorldValleyIVOriginLodespring2`         | 从任意界面进入四号谷地-矿脉源区-旧矿区大世界         |
| `SceneEnterWorldValleyIVOriginLodespring3`         | 从任意界面进入四号谷地-矿脉源区-矿区医疗站大世界     |
| `SceneEnterWorldValleyIVOriginLodespring4`         | 从任意界面进入四号谷地-矿脉源区-裂地者营地大世界     |
| `SceneEnterWorldValleyIVPowerPlateau0`             | 从任意界面进入四号谷地-供能高地-电站通信站大世界     |
| `SceneEnterWorldValleyIVPowerPlateau1`             | 从任意界面进入四号谷地-供能高地-高地主路大世界       |
| `SceneEnterWorldValleyIVPowerPlateau2`             | 从任意界面进入四号谷地-供能高地-碾骨战营大世界       |
| `SceneEnterWorldValleyIVPowerPlateau3`             | 从任意界面进入四号谷地-供能高地-疏散区大世界         |

### 武陵

| 接口名                                 | 说明                                       |
| -------------------------------------- | ------------------------------------------ |
| `SceneEnterMapWulingWulingCity`        | 从任意界面进入武陵-武陵城地图界面          |
| `SceneEnterMapWulingJingyuValley`      | 从任意界面进入武陵-景玉谷地图界面          |
| `SceneEnterMapWulingQingboStockade`    | 从任意界面进入武陵-清波寨地图界面          |
| `SceneEnterWorldWulingJingyuValley0`   | 从任意界面进入武陵-景玉谷-生态实验站大世界 |
| `SceneEnterWorldWulingJingyuValley1`   | 从任意界面进入武陵-景玉谷-踩道大世界       |
| `SceneEnterWorldWulingJingyuValley2`   | 从任意界面进入武陵-景玉谷-聚宝窟外大世界   |
| `SceneEnterWorldWulingJingyuValley3`   | 从任意界面进入武陵-景玉谷-清波寨外寨大世界 |
| `SceneEnterWorldWulingJingyuValley4`   | 从任意界面进入武陵-景玉谷-聚宝窟内大世界   |
| `SceneEnterWorldWulingJingyuValley5`   | 从任意界面进入武陵-景玉谷-天王坪大世界     |
| `SceneEnterWorldWulingJingyuValley6`   | 从任意界面进入武陵-景玉谷-驮鼻山大世界     |
| `SceneEnterWorldWulingJingyuValley7`   | 从任意界面进入武陵-景玉谷-迷踪林大世界     |
| `SceneEnterWorldWulingJingyuValley8`   | 从任意界面进入武陵-景玉谷-摘菱屿大世界     |
| `SceneEnterWorldWulingJingyuValley9`   | 从任意界面进入武陵-景玉谷-南山大世界       |
| `SceneEnterWorldWulingJingyuValley10`  | 从任意界面进入武陵-景玉谷-水泽涧大世界     |
| `SceneEnterWorldWulingWulingCityCore`  | 从任意界面进入武陵-武陵城核心区大世界      |
| `SceneEnterWorldWulingWulingCity0`     | 从任意界面进入武陵-武陵城-观测站大世界     |
| `SceneEnterWorldWulingWulingCity1`     | 从任意界面进入武陵-武陵城-界石坪大世界     |
| `SceneEnterWorldWulingWulingCity2`     | 从任意界面进入武陵-武陵城-待修缮区大世界   |
| `SceneEnterWorldWulingWulingCity3`     | 从任意界面进入武陵-武陵城-方兴衢大世界     |
| `SceneEnterWorldWulingWulingCity4`     | 从任意界面进入武陵-武陵城-天师府学院大世界 |
| `SceneEnterWorldWulingWulingCity5`     | 从任意界面进入武陵-武陵城-天井院大世界     |
| `SceneEnterWorldWulingWulingCity6`     | 从任意界面进入武陵-武陵城-储备站左上大世界 |
| `SceneEnterWorldWulingWulingCity7`     | 从任意界面进入武陵-武陵城-三窟岗大世界     |
| `SceneEnterWorldWulingWulingCity8`     | 从任意界面进入武陵-武陵城-储备站右下大世界 |
| `SceneEnterWorldWulingQingboStockade0` | 从任意界面进入武陵-清波寨-顶天梁大世界     |
| `SceneEnterWorldWulingQingboStockade1` | 从任意界面进入武陵-清波寨-栈桥道大世界     |
| `SceneEnterWorldWulingQingboStockade2` | 从任意界面进入武陵-清波寨-主寨西南大世界   |
| `SceneEnterWorldWulingQingboStockade3` | 从任意界面进入武陵-清波寨-祖泉大世界       |
| `SceneEnterWorldWulingQingboStockade4` | 从任意界面进入武陵-清波寨-主寨东南大世界   |

完整接口列表及说明请直接查看 `assets/resource/pipeline/Interface` 目录下各 `SceneXXX.json` 中各节点的 `desc` 字段。
