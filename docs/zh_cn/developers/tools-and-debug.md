# 工具与调试

## 开发工具

MaaFramework 提供了丰富的[开发工具](https://github.com/MaaXYZ/MaaFramework/tree/main?tab=readme-ov-file#%E5%BC%80%E5%8F%91%E5%B7%A5%E5%85%B7)，调试时工作目录设为**项目根目录**。

| 工具 | 简介 |
| -------------------------------------------------------------------------- | ----------------------------------------------------------- |
| [MaaDebugger](https://github.com/MaaXYZ/MaaDebugger) | 独立调试工具 |
| [Maa Pipeline Support](https://github.com/neko-para/maa-support-extension) | VSCode 插件，提供调试、截图、获取 ROI、取色等功能 |
| [MFAToolsPlus](https://github.com/SweetSmellFox/MFAToolsPlus) | 跨平台开发工具箱，提供便捷的数据获取和模拟测试方法 |
| [MaaPipelineEditor](https://mpe.codax.site/docs) | 可视化阅读与构建 Pipeline，功能完备，提供渐进式本地功能扩展 |
| [MaaLogAnalyzer](https://github.com/MaaXYZ/MaaLogAnalyzer) | 可视化分析基于 MaaFramework 开发应用的日志 |
| [MAA-pipeline-generate](https://github.com/Joe-Bao/MAA-pipeline-generate) | 批量生成仅有细微差异的 Pipeline 模板 |
| [Auto-green-background](https://github.com/Joe-Bao/Auto-green-background) | 自动绿幕工具 |

## 出错前视频记录（开发计划）

计划在任务出错时保存最近约 30 秒的画面，方便结合日志还原界面变化与操作过程。该功能尚未实现。

- 复用任务运行时已有的截图或 `vision` 数据，压缩为视频，限制缓存时间和空间。
- 仅在出错时导出视频；正常完成或主动停止时释放缓存。
- 保留现有错误截图，视频处理失败不能影响任务或日志导出。
- 实现前核对 MaaFramework、Agent 与 MXU 的职责边界，确认编码器及跨平台依赖。
- 验证视频可播放、时间顺序正确、缓存有界，以及正常结束、主动停止和多任务场景。

## 交流

开发 QQ 群: [1072587329](https://qm.qq.com/q/EyirQpBiW4) （干活群，欢迎加入一起开发，但不受理用户问题）
