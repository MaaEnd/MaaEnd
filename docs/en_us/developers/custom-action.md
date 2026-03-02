<!-- markdownlint-disable MD060 -->

# Development Guide - Custom Action Reference

`Custom` is a generic node type in the Pipeline used to invoke **custom actions**.  
The concrete logic is registered on the project side via `MaaResourceRegisterCustomAction` (for example, implementations under `agent/go-service`), while the Pipeline is only responsible for **parameter passing and scheduling**.

Unlike normal click/recognition nodes, `Custom` does not limit what the action actually does—  
as long as it is registered during the resource loading stage, it can be called in any Pipeline in a unified way, for example:

- Take a screenshot once and save it locally (such as the `ScreenShot` action).
- Perform complex multi-step interactions (long-press, drag, combo keys, etc.).
- Do statistics, logging, or telemetry reporting.

---

## ScreenShot Action

`ScreenShot` is a screenshot action invoked via `Custom`, implemented in `agent/go-service/screenshot`.  
It takes a screenshot of the current screen and saves it as a PNG file under the `debug` folder in the working directory.

- **Parameters (`custom_action_param`)**

    - You need to pass in a JSON object (for example `{}` or `{"type": "..."}`), which will be serialized into a string by the framework and then passed to Go.
    - Fields:
        - `type?: string`: optional filename prefix. If specified, the value will be used as a prefix, e.g. `ColorCheckFailed_...png`; if omitted or empty, only a timestamp-based name will be used without a prefix.
        - `dir?: string`: optional directory where screenshots are saved. Supports relative or absolute paths, defaults to `debug`; if it does not exist, it will be created automatically. The cleanup logic only applies to PNG files in this directory.
        - `clean_days?: number`: optional cleanup window in days, default `3`. Before each screenshot, PNG files in this directory that are **older than `clean_days` days** will be deleted.

- **Behavior**
    - Before capturing, the controller interface is called to refresh the screen cache, and then the latest screen image is read.
    - If capturing fails, the image is empty, or file writing fails, an error will be logged and the action will return failure.
    - Screenshot files are always saved under the directory specified by `dir` (default `debug`):
        - If the directory does not exist, it will be created automatically.
        - Each time it runs, PNG files older than **`clean_days` days** in this directory will be cleaned up (3 days by default).
    - The filename contains a human-readable time and a nanosecond suffix to avoid collisions when multiple screenshots are taken within the same second.

> Currently the `ScreenShot` action does **not** use the `target` / `target_offset` fields.  
> Regardless of whether these fields are configured in the Pipeline, it will always capture the **entire screen**.

<!-- markdownlint-enable MD060 -->
