<!-- markdownlint-disable MD060 -->

# 开发手册 - Custom 自定义动作参考

`Custom` 是 Pipeline 中用于调用 **自定义动作** 的通用节点类型。  
具体逻辑由项目侧通过 `MaaResourceRegisterCustomAction` 注册（如 `agent/go-service` 中的实现），Pipeline 仅负责 **传参与调度**。

与普通点击、识别节点不同，`Custom` 不限定具体行为——  
只要在资源加载阶段完成注册，就可以在任意 Pipeline 中以统一的方式调用，例如：

- 执行一次截图并保存到本地（如 `ScreenShot` 动作）。
- 进行复杂的多步交互（长按、拖拽、组合键等）。
- 做一些统计、日志或埋点上报。

---

## ScreenShot 截图动作

`ScreenShot` 是一个通过 `Custom` 调用的截图动作，实现位于 `agent/go-service/common`。  
它会对当前画面进行一次截屏，并将结果以 PNG 格式保存到工作目录下的 `debug` 文件夹中。

- **参数（`custom_action_param`）**

    - 需要传入一个 JSON 对象（例如 `{}` 或 `{"type": "..."}`），由框架序列化为字符串后传给 Go。
    - 字段说明：
        - `type?: string`：文件名前缀，可选。若指定，其值会作为前缀使用，例如 `ColorCheckFailed_...png`；未指定或为空时不加前缀，仅使用时间戳命名。
        - `dir?: string`：截图保存目录，可选。支持相对路径或绝对路径，默认为 `debug`，不存在时会自动创建；清理逻辑只会作用于该目录下的 PNG 文件。
        - `clean_days?: number`：清理时间窗口（单位：天），可选，默认 `3`。每次截图前会删除该目录中 **`clean_days` 天之前** 的 PNG 文件。

- **行为**
    - 截图前调用控制器接口刷新画面缓存，然后读取最新的屏幕图像。
    - 若截图失败、图像为空或文件写入失败，会在日志中记录错误并返回失败。
    - 截图文件统一保存在 `dir` 指定的目录下（默认 `debug`）：
        - 若目录不存在会自动创建；
        - 每次执行时会清理该目录中早于 **`clean_days` 天之前** 的 PNG 文件（默认 3 天）。
    - 文件名包含可读时间和纳秒后缀，以避免同一秒内多次截图产生重名。

> 目前 `ScreenShot` 动作不会使用 `target` / `target_offset` 字段，无论是否在 Pipeline 中配置这些字段，都只会对整屏进行截图。

---

## RunNode 按节点名执行节点

`RunNode` 是一个通用调度动作，实现位于 `agent/go-service/common`。  
它会读取参数中的节点名，并调用框架接口执行该节点。

- **参数（`custom_action_param`）**

    - 需要传入 JSON 对象，必须包含：
        - `node_name: string`：要执行的节点名称（必填）。

- **行为**
    - 当 `custom_action_param` 为空、JSON 解析失败或 `node_name` 为空时，动作会返回失败并记录错误日志。
    - 会调用 `RunTask(node_name)` 执行目标节点；若执行失败会返回失败并记录错误日志。
    - 执行成功后会输出成功日志。

- **Pipeline 示例**

```json
{
    "RunOtherNode": {
        "action": {
            "type": "Custom",
            "param": {
                "custom_action": "RunNode",
                "custom_action_param": {
                    "node_name": "SomeOtherNode"
                }
            }
        },
        "next": [
            "NextNode"
        ]
    }
}
```

<!-- markdownlint-enable MD060 -->
