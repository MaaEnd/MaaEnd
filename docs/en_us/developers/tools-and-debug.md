# Tools & debugging

## Development tools

MaaFramework provides a rich set of [development tools](https://github.com/MaaXYZ/MaaFramework/blob/main/README_en.md#development-tool). Set the working directory to the **project root** during debugging.

| Tool | Description |
| -------------------------------------------------------------------------- | ------------------------------------------------------------- |
| [MaaDebugger](https://github.com/MaaXYZ/MaaDebugger) | Standalone debugger |
| [Maa Pipeline Support](https://github.com/neko-para/maa-support-extension) | VS Code extension: debug, screenshots, ROI, color pick |
| [MFAToolsPlus](https://github.com/SweetSmellFox/MFAToolsPlus) | Cross-platform toolbox for data capture and simulated testing |
| [MaaPipelineEditor](https://mpe.codax.site/docs) | Visual Pipeline reading and authoring; feature-complete, with progressive local feature extensions |
| [MaaLogAnalyzer](https://github.com/MaaXYZ/MaaLogAnalyzer) | Visual log analysis for MaaFramework-based apps |
| [MAA-pipeline-generate](https://github.com/Joe-Bao/MAA-pipeline-generate) | Batch-generate Pipeline templates that differ only slightly |
| [Auto-green-background](https://github.com/Joe-Bao/Auto-green-background) | Automatic green-screen tool |

## Video before errors (development plan)

The planned feature saves approximately the last 30 seconds of task frames when an error occurs, so logs can be compared with screen transitions and actions. This feature is not implemented yet.

- Reuse screenshots or `vision` data already produced by the task, compress them into video, and bound cache duration and size.
- Export video only on errors; release the cache on normal completion or a user stop.
- Retain existing error screenshots; video failures must not affect tasks or log export.
- Verify the responsibilities of MaaFramework, the Agent, and MXU, including encoders and cross-platform dependencies, before implementation.
- Validate playback, chronological order, bounded caching, normal completion, user stops, and multiple tasks.

## Community

Development QQ Group: [1072587329](https://qm.qq.com/q/EyirQpBiW4) (A working group, welcome to join and develop together, but user issues are not handled here)
