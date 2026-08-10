# 内部架构与维护边界

本文档面向维护 `IconRecognition` 实现的开发者，只说明模块职责、数据流和修改边界。Pipeline、Go Service 和 C++ 调用所需的参数、参考 ROI、返回结构和错误码统一见[开发者使用指南](/docs/zh_cn/developers/components/icon-recognition.md)，内部文档不再复制外部契约。

## 数据流

一次 Custom Recognition 调用按以下层次处理：

1. `IconRecognitionRecognition.cpp` 把 MaaFramework 回调参数转换为 `RecognitionRequest`，调用核心识别器，并把 `RecognitionResult` 写回 Maa detail；
2. `IconRecognizer.cpp` 校验请求、加载候选模板、调用网格检测、逐格匹配并汇总结果；
3. `detail/GridDetector.cpp` 根据界面类型返回 `GridDetection`，不参与 catalog 过滤和图标分类；
4. `detail/TemplateCatalog.cpp`、`RarityCandidates.cpp` 和 `IconMatcher.cpp` 负责候选准备、稀有度缩减与模板评分；
5. `RecognitionDiagnostics.cpp` 和 `DebugCapture.cpp` 只记录内部诊断，不改变识别结果。

直接调用 C++ API 时会跳过第一层，其余数据流与 Custom 入口相同。

## 公开类型与内部类型

`IconRecognitionTypes.h` 定义跨入口共用的请求和结果类型：

- `RecognitionRequest` 是核心识别器唯一接受的请求对象；
- `RecognitionResult` 是 C++ 返回值，也是 Custom detail 的序列化来源；
- `ItemMatchLess` 固定公开结果的排序规则；
- `DeduplicateMatches` 在排序后按物品 ID 去重。

`detail/GridTypes.h` 中的 `GridCell`、`GridLayout`、`GridDetection` 和 `GridSelectionDiagnostics` 只服务内部网格定位。不得让调用方依赖这些类型中的拟合中间量，也不要把内部诊断并入公开 detail。

## 模块职责

### Custom 适配层

`IconRecognitionRecognition.cpp` 只负责：

- 解析 MaaFramework 传入的 JSON 和 ROI；
- 把字段映射到 `RecognitionRequest`；
- 调用进程内共享的 `IconRecognizer`；
- 写入命中框、detail 和可选 debug 文件。

参数语义和默认值应定义在公开类型或解析代码附近，并同步到外部开发者指南。不要在适配层加入网格定位或业务流程。

### 核心编排层

`IconRecognizer.cpp` 是识别流程的编排入口，负责：

- 校验阈值、ROI 和候选过滤条件；
- 按界面选择默认候选集与模板尺寸；
- 调用网格定位或构造临时单格；
- 对每个格子执行候选缩减、匹配、门控和诊断记录；
- 排序、去重并生成稳定的错误结果。

新增步骤时应先判断它属于所有界面共用的识别编排，还是某个界面的内部策略。界面专用逻辑优先下沉到 `detail/`，避免继续扩大入口函数。

### 网格定位层

网格定位的文件职责见[网格配置维护](grid-profiles.md)，具体决策流程见[识别算法](algorithm.md)。该层只输出格子位置和可选诊断，不加载物品图标，也不执行物品分类。

### 模板与匹配层

- `TemplateCatalog` 读取 catalog 并准备图标资源；
- `TemplateTypes` 和 `CompositeIcon` 处理模板表示与复合图标；
- `MaskPolicy` 生成界面适配的匹配遮罩；
- `RarityClassifier` 与 `RarityCandidates` 缩小候选集合；
- `IconMatcher` 和 `SubpixelMatcher` 计算基础分与精细相位分；
- `ForegroundTexture` 及界面专用遮罩负责拒绝明显不应接受的格子。

候选过滤只改变参与评分的模板集合，不得改变网格位置。模板评分也不得反向修改已经确定的格子几何。

### 诊断层

内部诊断分为网格级与格子级：

- 网格级记录候选比较、规则网格拟合和回退原因；
- 格子级记录候选模板、分数、稀有度缩减，以及候选为何没有进入公开结果；
- 性能诊断记录各阶段耗时与模板数量。

正常执行到结果汇总阶段时会收集网格级和格子级诊断；提前返回的 `invalid_image` 或 `exception` 可能不包含诊断。这些诊断供内部实现、debug 文件和人工测试使用；性能诊断以及 Custom 入口的 debug 文件由 debug 开关控制。添加诊断字段时，需要同步人工测试输出；除非公开契约确实需要，否则不要修改 `RecognitionResult::to_json()`。

## 维护约束

- Pipeline 负责界面跳转和操作流程；本组件只做单次图标识别。
- Custom、Go 和 C++ 三种入口最终必须复用同一个 `RecognitionRequest` / `RecognitionResult` 语义。
- 调参常量的含义、影响和标定依据写在对应定义旁，并由针对性测试锁定；内部 Markdown 不复制常量名称和值。
- 调用者可见的参数、默认值、参考 ROI 或返回字段发生变化时，更新中英文开发者使用指南。
- 只改变内部算法而不改变外部行为时，更新本目录的算法、网格或测试文档，不向调用指南暴露中间实现。
