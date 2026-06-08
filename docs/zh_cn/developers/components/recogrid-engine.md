# RecoGrid 网格扫描引擎接入指南

`RecoGrid` 是 `cpp-algo` 里的通用网格识别与滚动累计扫描引擎，源码在 `agent/cpp-algo/source/RecoGrid/`。它适合“一个列表由规则格子组成，每个格子里有图标，列表需要向下滚动才能扫完”的场景。

现有生产实例是 `agent/cpp-algo/source/WeaponInventoryScan/WeaponInventoryScan.cpp`。开发新实例时，不要直接复制参数。正确流程是：先用截图确认网格能稳定检测，再调模板分类，最后接滚动累计和 Pipeline。

## 先判断能不能用

适合使用 `RecoGridEngine`：

- 目标区域是规则行列网格。
- 每个有效格子的主体图标位置比较稳定。
- 可以准备每种物品的模板图。
- 滚动后仍然能通过截图判断当前页内容，而不是只能靠固定滑动次数。

不适合使用：

- 格子边界不明显，行列投影找不到稳定 segment。
- 同一类物品在 cell 内位置、缩放、遮挡变化很大。
- 需要 C++ 负责点击、跳界面、失败重试等业务流程。流程应由 Pipeline 管，RecoGrid 只做识别与累计。

开始写代码前至少要准备：

- 720p 基准截图，分辨率按 `1280x720` 标注。
- 网格区域 `roi`。
- 一次滑动前后的连续截图，最好包含首屏、中间页、末尾页。
- 模板图片目录。
- 目标界面的进入方式、滑动方式、稳定等待区域。

缺这些信息时不要硬写 Pipeline。RecoGrid 对 `roi`、格子边界和模板质量很敏感，凭猜出来的参数基本不可维护。

## Engine 接入方式

`RecoGridEngine` 是 C++ 内部 API，不能直接在 Pipeline 调。新业务需要写自己的 C++ wrapper，在 wrapper 中调用 engine，再把结果写给 Maa。

`RecoGridEngine` 负责完整扫描：

- 加载一个模板目录。
- 检测当前页网格。
- 过滤空格子。
- 对格子做多模板分类。
- 用 `sessionId` 累计多页结果。
- 判断滚动是否到末尾。

## 核心 API 输入输出

业务 wrapper 最终只需要围绕这个调用组织代码：

```cpp
GridScanResult Scan(
    const std::string& sessionId,
    const cv::Mat& image,
    const GridScanOptions& options = {});
```

输入：

| 输入 | 怎么来 | 注意点 |
| --- | --- | --- |
| `sessionId` | 业务自定义字符串，例如 `"WeaponInventoryScan"` | 同一个滚动列表必须保持一致；新任务开始要 reset；不同业务不要共用 |
| `image` | Maa callback 里的 `MaaImageBuffer` 转成 `cv::Mat` | 空图会返回失败；坐标会按 `normalizedSize` 归一化处理 |
| `options` | 业务默认值 + Pipeline 覆盖参数 | 先让默认值能跑通，再暴露少量参数给 Pipeline |

输出 `GridScanResult`：

| 字段 | 含义 | 调试时怎么看 |
| --- | --- | --- |
| `success` / `message` | 本帧是否扫描成功，以及失败原因 | 失败先看空图、模板目录、ROI |
| `rows` / `cols` | 当前可见页检测到的行列 | 判断网格检测是否稳定 |
| `totalCells` | 当前页有效格子数，不是 `rows * cols` 的裸乘 | 受空格子过滤影响 |
| `sessionRows` / `sessionCols` | 累计 session 的行列 | 判断累计列表形状 |
| `sessionTotalCells` | 累计有效格子数 | 业务通常最关心这个 |
| `knownCells` / `unknownCells` | 已分类 / 未分类累计数量 | 判断模板分类质量 |
| `rowOffset` | 当前页相对上一状态推进行数 | 判断滑动是否稳定 |
| `deltaReliable` | 当前页和历史页的 pHash 对齐是否可靠 | 滚动累计不对时优先看它 |
| `hasProgress` | 本帧是否带来新格子 | 中间页通常应为 true |
| `reachedEnd` | 是否判断已经到末尾 | 用来决定继续滑动还是结束 |
| `pendingStored` / `pendingResolved` | pending / beam 状态 | 滚动未稳定时会出现 pending |
| `matchRatio` / `averageDistance` | 页面重叠匹配质量 | 调滚动参数时看这两个 |
| `newCellIndices` | 当前页新增 cell index | 可用于判断本帧新增多少内容 |
| `cells` | 已排序的累计 cell 列表 | 默认不建议完整写入 `out_detail` |

`cells` 中每个 `GridScanCell` 的常用字段：

| 字段 | 含义 |
| --- | --- |
| `row` / `col` | session 全局行列 |
| `cellIndex` | 当前可见页内的 cell index |
| `screenCell` | 原始截图坐标下的 cell 矩形 |
| `templateId` | 分类 id，来自模板文件名 |
| `matched` | 是否成功分类 |
| `visible` | 本帧是否可见 |
| `score` / `templateScore` / `hueScore` | 分类评分 |
| `phashDistance` | 与模板的 pHash 距离 |

## 推荐开发顺序

不要一上来就接完整 Pipeline。推荐按下面顺序做：

1. 截图并定 `roi`
2. 调到当前页网格行列稳定
3. 准备模板目录并确认模板 id
4. 调占用过滤，避免空格子进入分类
5. 调模板分类，降低 unknown 和误分类
6. 调滚动 delta，让 `rowOffset` 稳定
7. 调末尾判断，让 `reachedEnd` 准确
8. 写业务 wrapper
9. 接 Pipeline 的识别、滑动、freeze wait
10. 看 `out_detail` 和日志复测首屏、中间页、末尾页

下面按这个顺序展开。

## 第一步：定 ROI 和网格检测参数

网格检测只看 `recognition.detect`。内部流程是：

1. 把截图 resize 到 `normalizedSize`。
2. 裁剪 `roi`。
3. 灰度化并 Otsu 二值化。
4. 分别对行、列做投影。
5. 用阈值找 row / col segment。
6. 过滤过小 segment。
7. row segment 和 col segment 交叉生成 cell。

最重要的初始参数：

```cpp
options.recognition.detect.normalizedSize = { 1280, 720 };
options.recognition.detect.roi = { x, y, width, height };
options.recognition.detect.rowThresholdRatio = 0.3;
options.recognition.detect.colThresholdRatio = 0.4;
options.recognition.detect.minRawSegmentLength = 10;
options.recognition.detect.minKeptSegmentRatio = 0.8;
```

参数怎么设：

| 参数 | 先怎么填 | 看什么现象 | 怎么调 |
| --- | --- | --- | --- |
| `normalizedSize` | 通常固定 `{1280, 720}` | 坐标整体偏移 | 确认截图标注是否按 720p 基准 |
| `roi` | 框住完整网格，少带无关 UI | 行列数不对、误识别标题/按钮 | 收紧到格子区域；不要切掉完整格子主体 |
| `rowThresholdRatio` | `0.2` 到 `0.4` | 行太多：噪声被当成行；行太少：格子被漏掉 | 行太多升高；行太少降低 |
| `colThresholdRatio` | `0.3` 到 `0.5` | 列太多或太少 | 列太多升高；列太少降低 |
| `minRawSegmentLength` | `8` 到 `12` | 小碎片很多 | 增大；如果细格子被漏掉则减小 |
| `minKeptSegmentRatio` | 滚动列表建议 `0.8` 到 `0.9` | 顶部/底部半截格被当完整行 | 增大；如果有效行被过滤则减小 |

判断网格检测是否合格：

- 首屏、中间页、末尾页的 `page_cols` 应稳定。
- 正常滚动时 `page_rows` 不应频繁在两个数之间跳。
- `page_grid = page_rows * page_cols` 应符合肉眼看到的可见格子数。
- 如果 `page_rows` 经常错，先调 `roi` 和 segment 参数，不要先改滚动累计参数。

`WeaponInventoryScan` 当前参数只是实例参考：

```cpp
options.recognition.detect.roi = { 20, 70, 960, 600 };
options.recognition.detect.rowThresholdRatio = 0.2;
options.recognition.detect.colThresholdRatio = 0.4;
options.recognition.detect.minRawSegmentLength = 10;
options.recognition.detect.minKeptSegmentRatio = 0.9;
```

## 第二步：设置 mask

mask 用来忽略 cell 内不适合参与识别的区域，例如左上角等级、右上角角标、底部文字条、稀有度边框。它会影响：

- pHash。
- 模板分类。
- 空格子占用判断。

字段是 cell 尺寸比例，不是像素：

```cpp
options.recognition.mask.leftHeaderWidth = 0.0;
options.recognition.mask.leftHeaderHeight = 0.0;
options.recognition.mask.rightHeaderWidth = 0.0;
options.recognition.mask.rightHeaderHeight = 0.0;
options.recognition.mask.bottomHeight = 0.0;
```

含义：

| 字段 | 忽略区域 |
| --- | --- |
| `leftHeaderWidth` + `leftHeaderHeight` | 左上角矩形 |
| `rightHeaderWidth` + `rightHeaderHeight` | 右上角矩形 |
| `bottomHeight` | 底部整条 |

例如 cell 大约是 `96x96`，想忽略左上 `20x20`、右上 `30x30`、底部 `20px`：

```cpp
options.recognition.mask.leftHeaderWidth = 20.0 / 96.0;
options.recognition.mask.leftHeaderHeight = 20.0 / 96.0;
options.recognition.mask.rightHeaderWidth = 30.0 / 96.0;
options.recognition.mask.rightHeaderHeight = 30.0 / 96.0;
options.recognition.mask.bottomHeight = 20.0 / 96.0;
```

调参建议：

- 如果同一物品因为角标、等级、数量不同而匹配不稳定，扩大对应 mask。
- 如果不同物品主体被 mask 遮掉导致混淆，缩小 mask。
- mask 应和模板裁剪逻辑一致。模板里保留的有效主体越干净，分类越稳定。

## 第三步：准备模板目录

`RecoGridEngine` 必须先加载模板才能 `Scan()`：

```cpp
g_engine.LoadTemplatesFromDirectory("assets/data/YourIcon");
```

模板规则：

- 支持 `.png`、`.jpg`、`.jpeg`、`.webp`、`.bmp`。
- 默认只读取目录第一层。
- 文件名 stem 是分类结果的 `templateId`，例如 `ak47.png` 输出 `ak47`。
- id 不能为空，不能重复。
- 图片必须能正常读取。

需要递归读取：

```cpp
recogrid::TemplateLoadOptions loadOptions;
loadOptions.recursive = true;
g_engine.LoadTemplatesFromDirectory("assets/data/YourIcon", loadOptions);
```

也可以手动设置模板：

```cpp
std::vector<recogrid::GridClassifyTemplate> templates;
templates.push_back({ "template_id", imageMat });
g_engine.SetTemplates(std::move(templates));
```

模板建议：

- 尽量裁成和实际 cell 主体一致的图标，不要带多余背景。
- 同一套模板的风格、分辨率、裁剪范围要统一。
- 如果 cell 里有固定 UI 噪声，优先用 mask 忽略，不要让模板硬吃噪声。
- 不要把“unknown”当模板放进去；未匹配时 engine 会用 `unknownTemplateId`。

## 第四步：调空格子过滤

engine 不会对所有 cell 都分类，会先判断 cell 是否“看起来有内容”。相关参数：

```cpp
options.occupiedBrightThreshold = 70;
options.minOccupiedMean = 55.0;
options.minOccupiedBrightRatio = 0.20;
```

内部判断逻辑：

- 先应用 mask。
- 计算保留区域的平均灰度 `mean`。
- 统计高于 `occupiedBrightThreshold` 的亮像素比例 `brightRatio`。
- 只有 `mean >= minOccupiedMean` 且 `brightRatio >= minOccupiedBrightRatio` 才认为 cell 被占用。

调参表：

| 现象 | 优先调整 |
| --- | --- |
| 空格子被当成物品，`page_grid` 偏大 | 提高 `minOccupiedMean` 或 `minOccupiedBrightRatio` |
| 暗色物品被漏掉，`page_grid` 偏小 | 降低 `minOccupiedMean` 或 `minOccupiedBrightRatio` |
| 亮边框/角标让空格子误判 | 设置 mask，或提高 `minOccupiedBrightRatio` |
| 深色背景上亮图标很少 | 降低 `minOccupiedBrightRatio`，不要只降 `occupiedBrightThreshold` |

建议先让 `page_grid` 接近肉眼看到的“有内容格子数”，再调分类。否则分类参数会被空格子噪声拖偏。

## 第五步：调模板分类

分类分两段：

1. pHash 初筛：只保留 Hamming distance 不超过 `maxPhashDistance` 的模板。
2. 精排：把模板缩放到 cell 大小，用灰度模板匹配和可选 hue 评分算最终 `score`。

主要参数：

```cpp
options.recognition.maxPhashDistance = 10;
options.recognition.minScore = 0.6;
options.recognition.hueWeight = 0.4;
options.recognition.maxRankedCandidates = 0;
```

但注意：在多模板分类里，`maxRankedCandidates = 0` 不是真正无限。源码会用默认每个 cell 最多精排 5 个 pHash 最近模板。设置大于 0 时，表示每个 cell 精排的模板数。

调参表：

| 参数 | 作用 | 调大 | 调小 |
| --- | --- | --- | --- |
| `maxPhashDistance` | pHash 初筛距离 | 候选更多，减少 unknown，但更慢、更容易误分类 | 候选更少，速度更快，但可能漏匹配 |
| `minScore` | 最终接受阈值 | 减少误分类，unknown 增多 | unknown 减少，误分类风险增加 |
| `hueWeight` | 色相评分权重 | 更重视颜色，适合彩色图标区分 | 更重视形状/亮度，适合颜色易受环境影响 |
| `maxRankedCandidates` | 每个 cell 进入精排的模板数 | 误筛风险降低，但更慢 | 更快，但 pHash 排名靠后的正确模板可能进不来 |

推荐调法：

1. 先用 `maxPhashDistance = 10`、`minScore = 0.6`、`hueWeight = 0.3~0.4`。
2. 如果很多有效格子是 unknown，看 `phashDistance` 和 `score`：
   - `phashDistance` 经常略高于阈值：提高 `maxPhashDistance`。
   - `score` 经常略低于阈值：降低 `minScore` 或检查 mask/模板裁剪。
3. 如果误分类多：
   - 提高 `minScore`。
   - 降低 `maxPhashDistance`。
   - 检查是否模板太相似，必要时提高 `hueWeight`。
4. 如果颜色相近但形状不同，降低 `hueWeight`。
5. 如果形状相近但颜色不同，提高 `hueWeight`。

不要靠无限降低 `minScore` 来消灭 unknown。unknown 是有价值的信号，说明模板、mask、截图或阈值还需要检查。

## 第六步：调滚动累计

滚动累计由 `sessionId` 维持。每次调用：

```cpp
const recogrid::GridScanResult result = g_engine.Scan(kSessionId, imageMat, options);
```

同一个 `sessionId` 会累计到同一个 session。新任务开始时必须重置：

```cpp
g_engine.ResetSession(kSessionId);
```

滚动相关参数：

```cpp
options.incremental = true;
options.matchDistanceThreshold = 12;
options.minMatchRatio = 0.5;
options.weakMinMatchRatio = 0.3;
options.endMinMatchRatio = 0.95;
```

这些参数看的是“相邻页面之间有多少 cell 的 pHash 能对上”。

| 参数 | 作用 | 调大 | 调小 |
| --- | --- | --- | --- |
| `incremental` | 是否启用 session 累计 | 通常保持 `true` | `false` 只扫当前页 |
| `matchDistanceThreshold` | 两个 cell 算匹配的最大 pHash 距离 | 更容易认为同一格匹配，delta 更容易可靠，但误对齐风险变高 | 更严格，误对齐少，但滚动中轻微变化可能对不上 |
| `minMatchRatio` | delta 可靠所需匹配比例 | 更保守，减少错位累计 | 更激进，减少卡住，但可能错位 |
| `weakMinMatchRatio` | pending / beam 接受弱进展的比例 | 更保守 | 更容易推进 |
| `endMinMatchRatio` | 判断到末尾的重复页比例 | 不容易提前结束，但可能多滑几次 | 容易结束，但可能提前停 |

观察 `out_detail`：

| 字段 | 该怎么看 |
| --- | --- |
| `row_offset` | 一次滑动大约推进几行，应相对稳定 |
| `delta_reliable` | 正常中间页应经常为 `true` |
| `match_ratio` | 越高说明页面重叠越明显 |
| `new_cells` | 中间页应大于 0；重复页或末尾可能为 0 |
| `pending_stored` | 当前候选先存起来等下一帧确认 |
| `pending_resolved` | 上一帧候选被确认 |
| `has_progress` | 是否真的增加了新内容 |
| `reached_end` | 是否判断列表结束 |

常见问题：

| 现象 | 先查什么 | 调整方向 |
| --- | --- | --- |
| 滚动后累计不增长 | `row_offset <= 0`、`delta_reliable = false` | 降低 `minMatchRatio`，或提高 `matchDistanceThreshold`；同时检查滑动后是否真的换页 |
| 累计跳行或重复 | `row_offset` 忽大忽小 | 先检查 `page_rows/page_cols` 是否稳定，再提高 `minMatchRatio` |
| 很快提前结束 | `reached_end = true` 但肉眼未到底 | 提高 `endMinMatchRatio`，或增大单次滑动距离避免重复帧 |
| 到底后不停滑 | 到底时 `match_ratio` 低 | 降低 `endMinMatchRatio`，或让滑动后等待区域稳定 |
| 中间页经常 pending 但不 resolve | 连续截图变化太大或等待不稳 | Pipeline 增加 `post_wait_freezes`，不要加硬延迟 |

先保证滑动后截图稳定，再调这些参数。滚动未结束的动画帧会让 pHash delta 看起来随机，参数再怎么调也不可靠。

## 第七步：写业务 wrapper

业务 wrapper 的职责：

- 持有一个 `recogrid::RecoGridEngine`。
- 加载模板目录。
- 设置本业务默认参数。
- 新任务开始时 reset session。
- 调 `Scan()`。
- 把 `GridScanResult` 写成 `out_detail`。
- 根据 `reachedEnd` 覆盖下一 Pipeline 节点。

最小结构：

```cpp
#include "../RecoGrid/RecoGridEngine.h"
#include "../utils.h"

namespace yourscan
{
namespace
{

constexpr const char* kSessionId = "YourScan";
recogrid::RecoGridEngine g_engine;
bool g_loaded = false;
MaaTaskId g_lastTaskId = MaaInvalidId;

void EnsureLoaded()
{
    if (!g_loaded) {
        g_engine.LoadTemplatesFromDirectory("assets/data/YourIcon");
        g_loaded = true;
    }
}

void ResetSessionForNewTask(MaaTaskId taskId)
{
    if (taskId == MaaInvalidId || taskId == g_lastTaskId) {
        return;
    }
    g_engine.ResetSession(kSessionId);
    g_lastTaskId = taskId;
}

void ApplyScanDefaults(recogrid::GridScanOptions& options)
{
    options.recognition.detect.normalizedSize = { 1280, 720 };
    options.recognition.detect.roi = { 20, 70, 960, 600 };
    options.recognition.detect.rowThresholdRatio = 0.3;
    options.recognition.detect.colThresholdRatio = 0.4;
    options.recognition.detect.minRawSegmentLength = 10;
    options.recognition.detect.minKeptSegmentRatio = 0.85;

    options.recognition.maxPhashDistance = 10;
    options.recognition.minScore = 0.6;
    options.recognition.hueWeight = 0.4;
    options.recognition.maxRankedCandidates = 0;

    options.incremental = true;
    options.matchDistanceThreshold = 12;
    options.minMatchRatio = 0.5;
    options.weakMinMatchRatio = 0.3;
    options.endMinMatchRatio = 0.95;
}

} // namespace
} // namespace yourscan
```

callback 中的关键调用：

```cpp
EnsureLoaded();
ResetSessionForNewTask(task_id);

recogrid::GridScanOptions options;
ApplyScanDefaults(options);

const recogrid::GridScanResult result = g_engine.Scan(kSessionId, to_mat(image), options);
```

注册到 `main.cpp`：

```cpp
MaaAgentServerRegisterCustomRecognition("YourScanRecognition", yourscan::YourScanRecognitionRun, nullptr);
```

`agent/cpp-algo/source/CMakeLists.txt` 当前用 `GLOB_RECURSE` 收集源码，新增 `.cpp` / `.h` 通常会自动进入构建。

## 第八步：设计 out_detail

建议 wrapper 默认输出紧凑摘要，不要把完整 `cells` 全塞进去。完整列表可能很大，会拖慢日志和 Maa detail。

推荐字段：

```json
{
    "success": true,
    "page_grid": 30,
    "cumulative_grid": 84,
    "known": 82,
    "unknown": 2,
    "page_rows": 5,
    "page_cols": 6,
    "rows": 14,
    "cols": 6,
    "new_cells": 12,
    "row_offset": 2,
    "delta_reliable": true,
    "pending_stored": false,
    "pending_resolved": true,
    "has_progress": true,
    "reached_end": false,
    "matched_cells": 24,
    "compared_cells": 30,
    "match_ratio": 0.8,
    "average_distance": 4.2,
    "delta_score": 221.5
}
```

字段含义：

| 字段 | 来源 | 用途 |
| --- | --- | --- |
| `page_grid` | `result.totalCells` | 当前页有效格子数 |
| `cumulative_grid` | `result.sessionTotalCells` | 累计格子数 |
| `known` | `result.knownCells` | 已分类数量 |
| `unknown` | `result.unknownCells` | 未分类数量 |
| `page_rows/page_cols` | `result.rows/cols` | 当前页检测行列 |
| `rows/cols` | `result.sessionRows/sessionCols` | 累计 session 行列 |
| `new_cells` | `result.newCellIndices.size()` | 本帧新增 cell |
| `row_offset` | `result.rowOffset` | 当前页相对上一状态推进行数 |
| `delta_reliable` | `result.deltaReliable` | 对齐是否可靠 |
| `reached_end` | `result.reachedEnd` | 是否到列表末尾 |
| `match_ratio` | `result.matchRatio` | 页面重叠匹配比例 |

需要导出完整结果时，可以加一个业务开关，例如 `return_cells`，只在调试或确实有消费方时输出：

```json
{
    "cells": [
        {
            "row": 0,
            "col": 0,
            "template_id": "ak47",
            "matched": true,
            "score": 0.91
        }
    ]
}
```

## 第九步：Pipeline 怎么接

Pipeline 仍然要遵守“识别 -> 操作 -> 再识别”。RecoGrid 不负责进界面，也不负责滑动动作。

推荐流程：

1. 进入目标界面。
2. 识别标题、Tab 或其他稳定元素，确认已在目标列表。
3. `pre_wait_freezes` 等网格区域稳定。
4. 调业务 `Custom Recognition` 扫当前页。
5. C++ wrapper 根据 `reachedEnd` 覆盖下一节点：
   - 未到底：`YourScanSwipeNext`
   - 到底：`YourScanFinish`
6. 滑动。
7. 移开鼠标/触点，避免 hover 或手指遮挡。
8. `post_wait_freezes` 等网格区域稳定。
9. 回到扫描节点。

示例骨架：

```json
{
    "YourScanRecognizePage": {
        "recognition": {
            "type": "Custom",
            "param": {
                "custom_recognition": "YourScanRecognition",
                "custom_recognition_param": {
                    "roi": [
                        20,
                        70,
                        960,
                        600
                    ],
                    "normalized_size": [
                        1280,
                        720
                    ],
                    "incremental": true
                }
            }
        },
        "pre_wait_freezes": {
            "time": 100,
            "target": [
                20,
                70,
                960,
                600
            ]
        },
        "action": "DoNothing",
        "next": [
            "YourScanFinish"
        ]
    },
    "YourScanSwipeNext": {
        "recognition": "DirectHit",
        "action": {
            "type": "Swipe",
            "param": {
                "begin": [
                    500,
                    540
                ],
                "end": [
                    500,
                    380
                ],
                "end_hold": 400,
                "duration": 200
            }
        },
        "next": [
            "YourScanMoveCursorAway"
        ]
    },
    "YourScanMoveCursorAway": {
        "recognition": "DirectHit",
        "action": "TouchMove",
        "target": [
            0,
            0,
            1,
            1
        ],
        "post_wait_freezes": {
            "time": 100,
            "target": [
                20,
                70,
                960,
                600
            ]
        },
        "next": [
            "YourScanRecognizePage"
        ]
    },
    "YourScanFinish": {
        "recognition": "DirectHit",
        "action": "StopTask"
    }
}
```

示例里的 ROI 和滑动坐标只是结构示例，必须按实际 720p 截图重测。不要为了“稳定”加硬延迟；优先用 freeze wait 和中间识别节点确认页面状态。

## custom_recognition_param 怎么覆盖默认值

业务 wrapper 可以像 `WeaponInventoryScan` 一样，先设置 C++ 默认值，再解析 `custom_recognition_param` 覆盖。

常见可暴露参数：

```json
{
    "roi": [
        20,
        70,
        960,
        600
    ],
    "normalized_size": [
        1280,
        720
    ],
    "row_threshold_ratio": 0.2,
    "col_threshold_ratio": 0.4,
    "min_raw_segment_length": 10,
    "min_kept_segment_ratio": 0.9,
    "mask": {
        "left_header_width": 0.2,
        "left_header_height": 0.2,
        "right_header_width": 0.3,
        "right_header_height": 0.3,
        "bottom_height": 0.2
    },
    "max_phash_distance": 10,
    "max_ranked_candidates": 0,
    "min_score": 0.6,
    "hue_weight": 0.4,
    "incremental": true,
    "end_min_match_ratio": 0.95
}
```

`GridRecognitionRequest::from_json()` 原生支持这些识别字段：

- `roi`
- `normalized_size`
- `row_threshold_ratio`
- `col_threshold_ratio`
- `min_raw_segment_length`
- `min_kept_segment_ratio`
- `mask` / `mask_ratios`
- `max_phash_distance`
- `min_score`
- `hue_weight`
- `max_ranked_candidates`
- `return_cells`
- `max_returned_cells`
- `max_returned_matches`
- `template_path`
- `template_paths`

注意：`template_path` / `template_paths` 不用于 `RecoGridEngine` 的多模板分类。业务 wrapper 应通过 `LoadTemplatesFromDirectory()` 或 `SetTemplates()` 加载模板目录。

scan 独有字段，例如 `incremental`、`end_min_match_ratio`、占用过滤阈值，不会由 `GridRecognitionRequest` 自动处理。业务 wrapper 需要自己读取。

## 快速排错表

| 问题 | 优先看 | 常见修法 |
| --- | --- | --- |
| 当前页完全识别失败 | `success/message`、`roi` | 检查 ROI 是否在截图内，截图是否为空 |
| 行列数不稳定 | `page_rows/page_cols` | 调 `roi`、`rowThresholdRatio`、`colThresholdRatio`、`minKeptSegmentRatio` |
| 空格子很多 | `page_grid` 偏大 | 调占用过滤和 mask |
| 物品被漏掉 | `page_grid` 偏小 | 降低占用过滤阈值，检查 mask 是否遮掉主体 |
| unknown 多 | `unknown`、分类分数 | 调模板、mask、`maxPhashDistance`、`minScore` |
| 误分类多 | `score/templateScore/hueScore` | 提高 `minScore`，降低 `maxPhashDistance`，调整 `hueWeight` |
| 滚动后不增长 | `row_offset`、`delta_reliable`、`new_cells` | 等稳定后再扫；调 `matchDistanceThreshold`、`minMatchRatio` |
| 提前结束 | `reached_end`、`match_ratio` | 提高 `endMinMatchRatio`，检查滑动距离 |
| 到底不停 | 末尾页 `match_ratio` | 降低 `endMinMatchRatio`，确保末尾重复页稳定 |

## 构建与检查

修改 C++ 后构建：

```powershell
cmake --build agent\cpp-algo\build --config RelWithDebInfo --target cpp-algo
```

需要安装到运行目录：

```powershell
cmake --install agent\cpp-algo\build --config RelWithDebInfo
```

提交前建议运行：

```powershell
pnpm format
pnpm format:go
pnpm check
pnpm test
```
