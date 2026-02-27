# Development Guide

**MaaEnd** is developed based on [MaaFramework](https://github.com/MaaXYZ/MaaFramework), adopting [Solution 2](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/zh_cn/1.1-%E5%BF%AB%E9%80%9F%E5%BC%80%E5%A7%8B.md#%E6%96%B9%E6%A1%88%E4%BA%8Cjson--%E8%87%AA%E5%AE%9A%E4%B9%89%E9%80%BB%E8%BE%91%E6%89%A9%E5%B1%95%E6%8E%A8%E8%8D%90) (JSON + Custom Logic Extension Recommendation).
Our main workflow uses [Pipeline JSON Low-Code](/assets/resource/pipeline), and complex logic is implemented via coding in [go-service](/agent/go-service).
If you intend to join the development of MaaEnd, you can first read the [MaaFramework Documentation](https://maafw.com/) to understand low-code logic and the use of related editing/debugging tools. You can also watch the [MaaFramework Tutorial Video](https://www.bilibili.com/video/BV1yr421E7MW), but note that the video is outdated-please refer to the documentation as the primary source~

## Local Deployment

We provide an automated **workspace initialization script**-simply execute:

```bash
python tools/setup_workspace.py
```

This will fully set up the environment required for development.

> [!NOTE]
>
> If problems occur, you can also follow the Manual Configuration Guide below to operate step by step.

<details>
<summary>Click to expand the Manual Configuration Guide.</summary>
<br>

1. Clone the project and subrepositories completely.

    ```bash
    git clone https://github.com/MaaEnd/MaaEnd --recursive
    ```

    **Do not omit `--recursive`**

    If you have already cloned the project but did not use the `--recursive` parameter, you can execute the following command in the project's root directory:

    ```bash
    git submodule update --init --recursive
    ```

2. Download [MaaFramework](https://github.com/MaaXYZ/MaaFramework/releases) and extract the contents to the `deps` folder.

3. Download MaaDeps pre-built.

    ```bash
    python tools/maadeps-download.py
    ```

4. Compile go-service and configure paths.

    ```bash
    python tools/build_and_install.py
    ```

    > To compile cpp-algo at the same time, add the `--cpp-algo` parameter:
    >
    > ```bash
    > python tools/build_and_install.py --cpp-algo
    > ```

5. Copy the contents of `deps/bin` (extracted in Step 2) to `install/maafw/` .

6. Download [MXU](https://github.com/MistEO/MXU/releases) and extract it to `install/` .

</details>

## Development Tips

### About Development Experience

- MaaFramework has a wealth of [development tools](https://github.com/MaaXYZ/MaaFramework/tree/main?tab=readme-ov-file#%E5%BC%80%E5%8F%91%E5%B7%A5%E5%85%B7) for low-code editing, debugging, etc.-please make good use of them. The working directory can be set to the `install` folder.
- After modifying the Pipeline each time, you only need to reload the resources in the development tool; however, after modifying go-service each time, you need to execute `python tools/build_and_install.py` to recompile.
- You can use tools like VS Code to set breakpoints or run go-service step by step (start go-service with debug on your own, or attach via vscode). Dude, are you debugging code just by reading logs?
- MXU is a GUI for end users-we do not recommend using it for development and debugging. The aforementioned MaaFramework development tools can greatly improve development efficiency. Seriously, are you just trial-and-erroring blindly?

### About Resources

- All images and coordinates in MaaEnd development need to be based on 720p resolution. MaaFramework will automatically convert them according to the user's device resolution during actual operation. It is recommended to use the above development tools for screenshot capture and coordinate conversion.
- **When prompted that features such as "HDR" or "Automatically manage color for apps" are enabled, do not take screenshots or pick colors-this may cause template effects to be inconsistent with the actual display on the user's device.**
- The resource folder is in a linked state; modifying `install` is equivalent to modifying the content in `assets` no additional copying is required. **However, `interface.json` is copied-if modified, you need to manually copy it back to `assets` before submission.**
- The `resource_fast` folder has default delays removed, which will greatly speed up operation speed but also place higher requirements on the robustness of the pipeline. We recommend using `resource_fast` first, but developers can also choose according to the actual situation of the task.
  _In plain terms, `resource_fast` is much harder to write-after each operation, the next frame may still show transition animations, and you have to find a way to recognize them. But the running speed is faster-feel free to try it if you are confident. If you can't figure it out or are too lazy to do it, put it in `resource` -the operation is slower but easier to write._

### About Reusable Nodes (Common Nodes)

Some highly reusable nodes have been encapsulated with detailed documentation to avoid reinventing the wheel. See:

- [MapTracker Reference Document](./map-tracker.md): Nodes related to minimap positioning and automatic pathfinding.
- [Common Buttons Reference Document](./common-buttons.md): Common button nodes.
- [Custom Action Reference Document](./custom-action.md): Invoke custom logic in go-service via the `Custom` node.
- [SceneManager Reference Document](./scene-manager.md): Universal jump and scene navigation related interfaces.

## Code Specifications

### Pipeline Low-Code Specifications

- Use pre_delay, post_delay, timeout, and on_error fields as little as possible. Add intermediate node recognition processes to avoid blind sleep waiting.
- Ensure that the first round of next hits (i.e., one screenshot) as much as possible-also achieve this by adding intermediate state recognition nodes. In short, expand the next list as much as possible to ensure any game screen is within expectations.
- Each operation must be based on recognition. Do not "recognize once overall -> click A -> click B -> click C", but instead "recognize A -> click A -> recognize B -> click B".
  _You cannot guarantee that the screen is the same after clicking A. In extreme cases, a new gacha banner pops up in the game at this time-clicking B directly may lead to accidental clicks into the gacha interface and misoperations._

> [!NOTE]
>
> Regarding delays, you can refer to [the basic operation mode of ALAS next door](https://github.com/LmeSzinc/AzurLaneAutoScript/wiki/1.-Start#%E5%9F%BA%E6%9C%AC%E8%BF%90%E4%BD%9C%E6%A8%A1%E5%BC%8F)(in Chinese)-the recommended practices are basically equivalent to our `next` field.

### Go Service Code Specifications

- Go Service is only used to handle certain special actions/recognition; the overall process should still be connected in series using Pipeline. Do not write a large amount of process code with Go Service.

### Cpp Algo Code Specifications

- Cpp Algo supports native OpenCV and ONNX Runtime, but it is only recommended for implementing individual recognition algorithms. Business logic such as various operations is recommended to be written with Go Service.
- For other code specifications, please refer to [MaaFramework Development Specifications](https://github.com/MaaXYZ/MaaFramework/blob/main/AGENTS.md#%E5%BC%80%E5%8F%91%E8%A7%84%E8%8C%83)(in Chinese).

## Communication

Developer QQ Group:  [1072587329](https://qm.qq.com/q/EyirQpBiW4) (Working group-welcome to join development, but user issues are not handled here)
