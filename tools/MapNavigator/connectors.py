from __future__ import annotations

import ctypes
import inspect
import json
import shutil
import subprocess
from abc import ABC, abstractmethod
from typing import Any

from connection_models import AdbConnectionConfig, AdbDeviceInfo, RecordingSessionConfig, Win32ConnectionConfig
from runtime import AGENT_DIR, MaaRuntime, RESOURCE_ADB_DIR

DEFAULT_ADB_INPUT_METHODS = 1 | 2 | 4
DEFAULT_ADB_SCREENCAP_METHODS = 1 | 2 | 4 | 64


class RecordingConnector(ABC):
    """录制连接器抽象，负责创建并连接具体 controller。"""

    def __init__(self, runtime: MaaRuntime) -> None:
        self._runtime = runtime

    @abstractmethod
    def connect(self) -> Any:
        raise NotImplementedError

    def attach_resource(self, resource: Any) -> None:
        return


class Win32RecordingConnector(RecordingConnector):
    """基于 Win32 窗口句柄建立录制连接。"""

    def __init__(self, runtime: MaaRuntime, config: Win32ConnectionConfig) -> None:
        super().__init__(runtime)
        self._config = config

    def connect(self) -> Any:
        if self._runtime.Win32Controller is None:
            raise RuntimeError("当前 maafw Python 运行时未提供 Win32Controller。")

        hwnd = find_game_window(self._config.window_title)
        if not hwnd:
            raise RuntimeError(f"未找到标题为 {self._config.window_title!r} 的游戏窗口，请确保游戏已运行且未被最小化。")

        controller = self._runtime.Win32Controller(hWnd=hwnd)
        controller.post_connection().wait()
        return controller


class AdbRecordingConnector(RecordingConnector):
    """基于 ADB 设备建立录制连接。"""

    def __init__(self, runtime: MaaRuntime, config: AdbConnectionConfig) -> None:
        super().__init__(runtime)
        self._config = config

    def connect(self) -> Any:
        if self._runtime.AdbController is None:
            raise RuntimeError("当前 maafw Python 运行时未提供 AdbController，无法建立 ADB 连接。")

        adb_path = resolve_adb_path(self._config.adb_path)
        if not adb_path:
            raise RuntimeError("未找到 adb 可执行文件，请在工具中指定 adb 路径或将 adb 加入 PATH。")

        if not self._config.address.strip():
            raise RuntimeError("未选择 ADB 设备，请先刷新设备列表并选择目标设备。")

        resolved_options = resolve_adb_connection_options(
            toolkit_type=self._runtime.Toolkit,
            adb_path=adb_path,
            address=self._config.address.strip(),
            extra_config=self._config.config,
        )
        controller = instantiate_adb_controller(
            adb_controller_type=self._runtime.AdbController,
            adb_path=resolved_options["adb_path"],
            address=resolved_options["address"],
            screencap_methods=resolved_options["screencap_methods"],
            input_methods=resolved_options["input_methods"],
            extra_config=resolved_options["config"],
        )
        controller.post_connection().wait()
        return controller

    def attach_resource(self, resource: Any) -> None:
        attach_path = getattr(resource, "post_path", None)
        if callable(attach_path) and RESOURCE_ADB_DIR.exists():
            try:
                attach_path(str(RESOURCE_ADB_DIR)).wait()
            except Exception as exc:
                raise RuntimeError(f"附加 ADB 资源失败: {exc}") from exc


def build_recording_connector(runtime: MaaRuntime, session: RecordingSessionConfig) -> RecordingConnector:
    if session.kind == "adb":
        return AdbRecordingConnector(runtime, session.adb)
    return Win32RecordingConnector(runtime, session.win32)


def resolve_adb_path(candidate: str) -> str:
    stripped = candidate.strip()
    if stripped:
        return stripped
    resolved = shutil.which("adb")
    return resolved or ""


def list_adb_devices(adb_path: str) -> list[AdbDeviceInfo]:
    resolved_adb = resolve_adb_path(adb_path)
    if not resolved_adb:
        return []

    try:
        completed = subprocess.run(
            [resolved_adb, "devices", "-l"],
            capture_output=True,
            text=True,
            check=True,
        )
    except (FileNotFoundError, subprocess.SubprocessError):
        return []

    devices: list[AdbDeviceInfo] = []
    for raw_line in completed.stdout.splitlines():
        line = raw_line.strip()
        if not line or line.startswith("List of devices attached"):
            continue

        parts = line.split()
        if len(parts) < 2:
            continue

        serial = parts[0]
        state = parts[1]
        metadata: dict[str, str] = {}
        for token in parts[2:]:
            if ":" not in token:
                continue
            key, value = token.split(":", maxsplit=1)
            metadata[key] = value

        devices.append(
            AdbDeviceInfo(
                serial=serial,
                state=state,
                model=metadata.get("model", ""),
                device=metadata.get("device", ""),
                transport_id=metadata.get("transport_id", ""),
            )
        )
    return devices


def find_game_window(expected_title: str) -> int:
    if not hasattr(ctypes, "windll"):
        raise RuntimeError("Win32 录制仅支持在 Windows 环境下使用。")

    enum_windows = ctypes.windll.user32.EnumWindows
    enum_windows_proc = ctypes.WINFUNCTYPE(
        ctypes.c_bool,
        ctypes.c_void_p,
        ctypes.c_void_p,
    )
    get_window_text = ctypes.windll.user32.GetWindowTextW
    is_window_visible = ctypes.windll.user32.IsWindowVisible

    result = [0]
    target_title = expected_title.strip()

    def foreach(hwnd, _l_param):
        if not is_window_visible(hwnd):
            return True

        title_buffer = ctypes.create_unicode_buffer(512)
        get_window_text(hwnd, title_buffer, 512)
        title = title_buffer.value.strip()
        if not title:
            return True

        if title == target_title:
            result[0] = hwnd
            return False
        return True

    cb = enum_windows_proc(foreach)
    enum_windows(cb, 0)
    return result[0]


def instantiate_adb_controller(
    adb_controller_type: Any,
    adb_path: str,
    address: str,
    screencap_methods: int,
    input_methods: int,
    extra_config: dict[str, Any] | None = None,
) -> Any:
    extra_config = extra_config or {}

    signature = None
    try:
        signature = inspect.signature(adb_controller_type)
    except (TypeError, ValueError):
        signature = None

    if signature is not None:
        bound_kwargs = build_adb_kwargs_from_signature(
            signature,
            adb_path,
            address,
            screencap_methods,
            input_methods,
            extra_config,
        )
        if bound_kwargs is not None:
            return adb_controller_type(**bound_kwargs)

    attempt_specs = [
        {
            "adb_path": adb_path,
            "address": address,
            "screencap_methods": screencap_methods,
            "input_methods": input_methods,
            "config": extra_config,
            "agent_path": str(AGENT_DIR),
            "callback": None,
        },
        {
            "adb_path": adb_path,
            "address": address,
            "screencap_methods": screencap_methods,
            "input_methods": input_methods,
            "config": extra_config,
            "agent_path": str(AGENT_DIR),
        },
        {
            "adb_path": adb_path,
            "address": address,
            "screencap_methods": screencap_methods,
            "input_methods": input_methods,
            "config": extra_config,
        },
    ]
    for kwargs in attempt_specs:
        try:
            return adb_controller_type(**kwargs)
        except TypeError:
            continue

    positional_attempts = [
        (adb_path, address, screencap_methods, input_methods, extra_config, str(AGENT_DIR), None),
        (adb_path, address, screencap_methods, input_methods, extra_config, str(AGENT_DIR)),
        (adb_path, address, screencap_methods, input_methods, extra_config),
    ]
    for args in positional_attempts:
        try:
            return adb_controller_type(*args)
        except TypeError:
            continue

    raise RuntimeError("无法根据当前 maafw Python API 签名构造 AdbController，请检查 maafw 版本。")


def build_adb_kwargs_from_signature(
    signature: inspect.Signature,
    adb_path: str,
    address: str,
    screencap_methods: int,
    input_methods: int,
    extra_config: dict[str, Any],
) -> dict[str, Any] | None:
    mapping = {
        "adb_path": adb_path,
        "address": address,
        "screencap_methods": screencap_methods,
        "input_methods": input_methods,
        "config": extra_config,
        "agent_path": str(AGENT_DIR),
        "callback": None,
    }

    kwargs: dict[str, Any] = {}
    for name, parameter in signature.parameters.items():
        if name == "self":
            continue
        if name in mapping:
            kwargs[name] = mapping[name]
            continue
        if parameter.default is inspect.Parameter.empty:
            return None
    return kwargs


def resolve_adb_connection_options(
    toolkit_type: Any,
    adb_path: str,
    address: str,
    extra_config: dict[str, Any],
) -> dict[str, Any]:
    discovered = find_toolkit_adb_device(toolkit_type, adb_path, address)
    if discovered is None:
        return {
            "adb_path": adb_path,
            "address": address,
            "screencap_methods": DEFAULT_ADB_SCREENCAP_METHODS,
            "input_methods": DEFAULT_ADB_INPUT_METHODS,
            "config": dict(extra_config),
        }

    merged_config = dict(discovered.get("config", {}))
    merged_config.update(extra_config)
    return {
        "adb_path": str(discovered.get("adb_path", adb_path) or adb_path),
        "address": str(discovered.get("address", address) or address),
        "screencap_methods": parse_int_like(discovered.get("screencap_methods"), DEFAULT_ADB_SCREENCAP_METHODS),
        "input_methods": parse_int_like(discovered.get("input_methods"), DEFAULT_ADB_INPUT_METHODS),
        "config": merged_config,
    }


def find_toolkit_adb_device(toolkit_type: Any, adb_path: str, address: str) -> dict[str, Any] | None:
    if toolkit_type is None:
        return None

    find_method = select_toolkit_find_method(toolkit_type)
    if find_method is None:
        return None

    candidate_invocations = []
    if adb_path:
        candidate_invocations.append((adb_path,))
    candidate_invocations.append(tuple())

    for args in candidate_invocations:
        try:
            result = find_method(*args)
        except TypeError:
            continue
        except Exception:
            return None

        for device in normalize_toolkit_device_list(result):
            device_address = str(device.get("address", "") or "").strip()
            if device_address == address:
                return device
    return None


def select_toolkit_find_method(toolkit_type: Any) -> Any:
    candidates = [
        "find_adb_devices",
        "find_adb_device",
        "adb_device_find",
        "adb_find",
    ]
    for name in candidates:
        method = getattr(toolkit_type, name, None)
        if callable(method):
            return method
    return None


def normalize_toolkit_device_list(raw_value: Any) -> list[dict[str, Any]]:
    if raw_value is None:
        return []

    if isinstance(raw_value, str):
        try:
            decoded = json.loads(raw_value)
        except json.JSONDecodeError:
            return []
        return normalize_toolkit_device_list(decoded)

    if isinstance(raw_value, dict):
        return [normalize_toolkit_device(raw_value)]

    if isinstance(raw_value, (list, tuple)):
        return [normalize_toolkit_device(item) for item in raw_value]

    items = getattr(raw_value, "devices", None)
    if isinstance(items, (list, tuple)):
        return [normalize_toolkit_device(item) for item in items]

    return []


def normalize_toolkit_device(raw_value: Any) -> dict[str, Any]:
    if isinstance(raw_value, dict):
        payload = raw_value
    else:
        payload = {}
        for key in [
            "adb_path",
            "address",
            "screencap_methods",
            "input_methods",
            "config",
            "name",
        ]:
            attr_names = [key, snake_to_camel(key), snake_to_pascal(key)]
            for attr_name in attr_names:
                if hasattr(raw_value, attr_name):
                    payload[key] = getattr(raw_value, attr_name)
                    break

    config_value = payload.get("config", {})
    if isinstance(config_value, str):
        try:
            config_value = json.loads(config_value)
        except json.JSONDecodeError:
            config_value = {}
    if not isinstance(config_value, dict):
        config_value = {}

    return {
        "adb_path": payload.get("adb_path", ""),
        "address": payload.get("address", ""),
        "screencap_methods": payload.get("screencap_methods", DEFAULT_ADB_SCREENCAP_METHODS),
        "input_methods": payload.get("input_methods", DEFAULT_ADB_INPUT_METHODS),
        "config": config_value,
    }


def parse_int_like(value: Any, default: int) -> int:
    if isinstance(value, bool):
        return int(value)
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        return int(value)
    if isinstance(value, str):
        stripped = value.strip()
        if not stripped:
            return default
        try:
            return int(stripped, 0)
        except ValueError:
            return default
    return default


def snake_to_camel(value: str) -> str:
    parts = value.split("_")
    return parts[0] + "".join(part.capitalize() for part in parts[1:])


def snake_to_pascal(value: str) -> str:
    return "".join(part.capitalize() for part in value.split("_"))
