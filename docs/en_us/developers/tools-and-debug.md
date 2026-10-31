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

### Current integration boundaries

MaaFramework's [`PipelineTask::save_on_error`](https://github.com/MaaXYZ/MaaFramework/blob/main/source/MaaFramework/Task/PipelineTask.cpp) saves the cached screenshot at the point of failure, while [`VisionBase`](https://github.com/MaaXYZ/MaaFramework/blob/main/source/MaaFramework/Vision/VisionBase.cpp) produces annotated `vision` images. When `save_draw` is disabled, `vision` files may not exist, so replay cannot rely solely on converting images on disk.

The recommended approach reuses existing frames in the framework, maintains a bounded cache of approximately 30 seconds, and saves video alongside the existing error screenshot. Ordinary recognition misses must not trigger export, and existing error screenshots must be retained. First verify OpenCV's built-in MJPEG/AVI support rather than assuming users have FFmpeg installed; H.264/MP4 requires separate verification of encoders and distribution dependencies.

MXU's current [log export implementation](https://github.com/MistEO/MXU/blob/5e923e83b30a102fb78da35c30ee2e1559aea682/src-tauri/src/commands/file_ops.rs#L658-L665) filters out video files, so the complete feature also requires updated export rules. An implementation limited to MaaEnd's Agent cannot observe all framework screenshots through recognition callbacks and must be described as local recognition-cycle replay.

## Community

Development QQ Group: [1072587329](https://qm.qq.com/q/EyirQpBiW4) (A working group, welcome to join and develop together, but user issues are not handled here)
