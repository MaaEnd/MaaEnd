## 问题分析

根据 ISSUE 讨论和代码审查，该问题涉及「一键售卖产品」任务在执行过程中出现卡死。从讨论中了解到：

1. **问题根因**：已确认为 MaaFramework 的单击失效问题（参考 [MaaFramework PR #1196](https://github.com/MaaXYZ/MaaFramework/pull/1196)）。当点击瞬间有轻微移动时会导致单击失效，复现成功率较高。

2. **相关代码位置**：
   - 主要逻辑位于 `assets/resource_fast/pipeline/SellProduct/SellCore.json`
   - 关键节点：
     - `SellProductSell`：点击「交易」按钮（第196-216行）
     - `SellProductClickBarRight`：点击进度条最右侧（第141-166行）
     - `SellProductSellCheck`：交易后确认（第217-246行）

3. **可能的改进方向**：
   - 检查是否需要增加点击后的状态验证
   - 考虑使用备选方案 `SellProductDragBar`（滑动滑动条）作为降级方案
   - 评估是否需要增加重试机制或更完善的错误处理

---

通过 `git blame` 分析，最近修改相关代码的贡献者如下（按提交时间倒序）：

- @Constrat（2026-03-09）：更新了 OCR 同步适配
- @overflow65537（2026-03-08）：添加了国际化支持
- @uy/sun（2026-03-06）：多次优化了售卖逻辑和识别节点

恳请以上三位贡献者协助排查此问题，特别是：
- 是否可以在当前 Pipeline 层面增加容错机制
- 是否需要调整点击策略（如使用滑动替代点击）
- 是否有其他可行的临时解决方案

感谢各位的协助！ 🙏
