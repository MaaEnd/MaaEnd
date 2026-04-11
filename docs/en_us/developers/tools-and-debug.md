# Tools & debugging

## Development tools

MaaFramework provides a rich set of [development tools](https://github.com/MaaXYZ/MaaFramework/tree/main?tab=readme-ov-file#%E5%BC%80%E5%8F%91%E5%B7%A5%E5%85%B7). Set the working directory to the **project root** when debugging.

| Tool                                                                       | Description                                                   |
| -------------------------------------------------------------------------- | ------------------------------------------------------------- |
| [MaaDebugger](https://github.com/MaaXYZ/MaaDebugger)                       | Standalone debugger                                           |
| [Maa Pipeline Support](https://github.com/neko-para/maa-support-extension) | VS Code extension: debug, screenshots, ROI, color pick        |
| [MFAToolsPlus](https://github.com/SweetSmellFox/MFAToolsPlus)              | Cross-platform toolbox for data capture and simulated testing |
| [MaaPipelineEditor](https://mpe.codax.site/docs)                           | Visual Pipeline authoring; extensible local features          |
| [MaaLogAnalyzer](https://github.com/MaaXYZ/MaaLogAnalyzer)                 | Visual log analysis for MaaFramework-based apps               |
| [MAA-pipeline-generate](https://github.com/Joe-Bao/MAA-pipeline-generate)  | Batch-generate Pipeline templates that differ only slightly   |
| [Auto-green-background](https://github.com/Joe-Bao/Auto-green-background)  | Automatic green-screen masking for templates                  |

## OCR & i18n

See [Coding standards — OCR & i18n](./coding-standards.md#ocr--i18n) for full rules. In short: write full `expected` strings and let `tools/i18n` expand them; use `// @i18n-skip` only when you need fragments or custom regex.

## Color matching (prefer HSV)

See [Coding standards — Resource standards](./coding-standards.md#resource-standards). Prefer HSV or grayscale over raw RGB across different GPUs.

## Community

Dev QQ group: [1072587329](https://qm.qq.com/q/EyirQpBiW4) (contributors welcome; **not** for end-user support)
