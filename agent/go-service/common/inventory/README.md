# Inventory 物品转移动作

供背包、仓库和库存转移等任务复用，以转移数量表达操作语义，不要求调用方了解平台输入方式。

## 公共接口

| 注册名 | 操作语义 | 桌面端输入 |
| --- | --- | --- |
| `InventoryTransferAllAction` | 全部转移 | Ctrl + 点击 |
| `InventoryTransferStackAction` | 转移一组 | Shift + 点击 |
| `InventoryTransferHalfAction` | 转移一半 | Alt + 点击 |

三个动作均无 `custom_action_param`。调用方通过外层 `target` / `target_offset` 指定已识别的源物品格；动作使用 Pipeline 已解析的目标框，不再次缩小目标范围。

前置条件是调用方已进入支持物品转移的界面，并完成目标物品识别。动作不负责导航、仓库切换、物品扫描、分页或快照维护。

**返回成功仅表示输入过程成功，不等于物品转移已完成。** 调用方仍须识别操作后的物品状态，再决定是否更新快照或继续任务。

## 平台实现

平台资源独立位于各资源目录的 `pipeline/Common/Private/Inventory/Action.json`，不依赖原 `common/autoalt` 的内部节点。原 Alt 点击、Alt 滑动和 Ctrl 点击能力保持独立。

每个动作使用以下内部节点，`{All|Stack|Half}` 对应三种转移模式：

1. `__InventoryTransfer{All|Stack|Half}BeginAction`：开始输入，桌面端按下对应修饰键。
2. `__InventoryTransferClickAction`：三种模式共用的执行节点，桌面端点击目标物品。
3. `__InventoryTransfer{All|Stack|Half}EndAction`：收尾输入，桌面端释放修饰键。

开始或执行失败时仍尝试收尾；收尾失败则整个动作返回失败。内部通过 `RunAction` 调用节点，不遍历节点的 `next`，也不执行节点识别。

桌面端沿用原修饰键点击时序。macOS 键码覆盖由 `pnpm generate:MacOS` 生成，不手工修改生成文件。

### ADB 触屏操作

ADB 的开始和收尾节点为 `DoNothing`，共享点击节点覆盖为内部 `InventoryTransferTouchAction`。公共 Go 动作调用该节点时传入对应的 `mode`，不依赖客户端环境变量判断平台，在一个调用内完成手势：

1. 触点 0 持续按住已识别的源物品。
2. 截图识别对应操作图标；先查左侧，未命中才查同一帧的右侧。
3. 命中后，使用触点 1 按下图标所在位置。
4. 先释放触点 1，再释放触点 0。

菜单最多等待 5 秒，两侧均未命中后间隔 100 毫秒继续截图识别，不重复按下源物品。正常结束、失败或取消都尝试释放已尝试按下的触点；释放失败则整个动作失败。清理直接调用控制器，避免任务停止后依赖 Pipeline 调度。底层输入失联时无法保证设备实际收到释放事件。

识别节点位于 ADB 资源的 `pipeline/Common/Private/Inventory/Recognition.json`。以 1280×720 为基准，左 ROI 为 `[150, 175, 100, 370]`，右 ROI 为 `[1030, 175, 100, 370]`，不搜索中间物品区域。三个图标均使用 `TemplateMatch` 的 method 5 和阈值 `0.85`。

模板从同一张左侧菜单截图 `MuMu-20260831-101834-664.png` 裁剪白色图标内部区域，不包含圆形外框：

| 模板 | 裁剪区域 `[x, y, w, h]` |
| --- | --- |
| `Common/Inventory/TransferStack.png` | `[174, 199, 37, 34]` |
| `Common/Inventory/TransferAll.png` | `[184, 337, 44, 39]` |
| `Common/Inventory/TransferHalf.png` | `[177, 482, 33, 32]` |

内部触屏动作使用 `mode: all / stack / half` 选择图标；任务应调用上述三个无参数公共动作，不直接使用内部入口。无需传入背包或仓库方向。

CloudADB 和 PlayCover 继承 ADB 资源，但其控制器的多指支持、完整转移手势和取消清理仍需实机验证；截图匹配通过不代表设备交互已验证。

## 验证

在 `agent/go-service` 目录运行：

```sh
go test ./common/inventory
```

单元测试覆盖三种动作的调用顺序、目标框透传、各阶段失败时的收尾，以及触屏操作的左右短路、同帧匹配、菜单延迟出现、超时、取消、识别异常和触点释放失败。

当前调用方的资源契约测试位于 `tools/icon_recognition/item_transfer/test_generate.py`，覆盖公共动作调用、注册与 Schema、桌面键码、macOS 映射、ADB 触点和识别参数。资源整体检查使用 `pnpm check`；真实转移结果仍需游戏内验证。
