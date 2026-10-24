"""把导出的 prefab / 动画 JSON 读成统一的节点模型。

解包资源是嵌套 GameObject 的形态（``$path_id`` / ``m_Name`` / ``$transform`` / ``components`` /
``children``，业务字段直接写在对象里），:func:`parse_unity_document` 把它转成 :class:`UnityDocument`；
下游的绑定、布局和父链只看这个模型。旧 RAW 导出（``$tree`` + ``$objects``）不再支持，遇到直接报错，
重新解包即可。

这里有两个容易踩的点：

- pathID 是 64 位整数，导出时存成十进制**字符串**（用数字会被 JS 丢掉精度）。``{"$path_id": n}`` 可能指向
  GameObject、Transform 或组件，三份索引分开建，含义看它落在哪一份里。
- 四元数按字段名读（``x`` / ``y`` / ``z`` / ``w``），不靠键的顺序。
"""

import json
from dataclasses import dataclass, field
from typing import TYPE_CHECKING, Any

from .errors import InputError

if TYPE_CHECKING:
    from pathlib import Path

    from .geometry import Quaternion, Vector3


def load_json_document(path: Path) -> Any:
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except OSError as error:
        raise InputError(f"读不到 {path}：{error}") from error
    except json.JSONDecodeError as error:
        raise InputError(f"{path} 不是合法 JSON：{error}") from error


# ── 引用与取值 ─────────────────────────────────────────────────────────────


@dataclass(frozen=True, slots=True)
class Reference:
    path_id: str


def as_reference(value: Any) -> Reference | None:
    source = narrow_dict(value)
    if "$path_id" in source:
        return Reference(str(source["$path_id"]))
    return None


def dict_entries(value: Any) -> dict[str, Any]:
    """Unity 的字典有两种导出形态：``{"k": v}`` 和 ``{"_keyData": [...], "_valueData": [...]}``，都归一成前一种。"""
    source = narrow_dict(value)
    for key_field, value_field in (("_keyData", "_valueData"), ("_keys", "_values")):
        if key_field not in source and value_field not in source:
            continue
        keys = narrow_list(source.get(key_field))
        values = narrow_list(source.get(value_field))
        if len(keys) != len(values):
            raise InputError(f"字典的键和值数量不一致：{key_field}/{value_field}")
        return {str(key): item for key, item in zip(keys, values, strict=True)}
    return source


def _vec3(value: object, default: Vector3) -> Vector3:
    raw = narrow_dict(value)
    if not raw:
        return default
    try:
        return (float(raw["x"]), float(raw["y"]), float(raw["z"]))
    except (KeyError, TypeError, ValueError) as error:
        raise InputError(f"Vector3 格式不对：{value!r}") from error


def _quat(value: object, default: Quaternion) -> Quaternion:
    raw = narrow_dict(value)
    if not raw:
        return default
    try:
        return (float(raw["x"]), float(raw["y"]), float(raw["z"]), float(raw["w"]))
    except (KeyError, TypeError, ValueError) as error:
        raise InputError(f"四元数格式不对：{value!r}") from error


# ── 节点模型 ───────────────────────────────────────────────────────────────


@dataclass(slots=True)
class UnityComponent:
    path_id: str
    type_name: str
    data: dict[str, Any]

    def get(self, key: str, default: Any = None) -> Any:
        return self.data.get(key, default)


@dataclass(slots=True)
class UnityNode:
    game_object_path_id: str
    transform_path_id: str
    name: str
    components: list[UnityComponent]
    children: list[UnityNode] = field(default_factory=list["UnityNode"])
    path: str = ""  # 斜杠拼起来的节点路径，只用于报错

    def component(self, type_name: str) -> UnityComponent | None:
        for comp in self.components:
            if comp.type_name == type_name:
                return comp
        return None

    def components_of(self, type_name: str) -> list[UnityComponent]:
        return [comp for comp in self.components if comp.type_name == type_name]


@dataclass(slots=True)
class TransformData:
    """导出的 transform 字段，原样保留、先不算布局：``position`` 就是 ``m_LocalPosition``。"""

    path_id: str
    type_name: str  # "Transform" | "RectTransform"
    position: Vector3
    rotation: Quaternion
    scale: Vector3
    raw: dict[str, Any]

    @property
    def is_rect(self) -> bool:
        return self.type_name == "RectTransform"


@dataclass(slots=True)
class UnityDocument:
    source_path: Path
    root: UnityNode
    games: dict[str, UnityNode]
    transforms: dict[str, TransformData]
    components: dict[str, UnityComponent]


def _component_type_name(data: dict[str, Any]) -> str:
    for key in ("$script", "m_Script", "type", "m_Type", "$type"):
        value = data.get(key)
        if isinstance(value, str) and value:
            return value
    return "Component"


# ── 新包导出形态 ───────────────────────────────────────────────────────────


def _parse_document(path: Path, document: dict[str, Any]) -> UnityDocument:
    games: dict[str, UnityNode] = {}
    transforms: dict[str, TransformData] = {}
    components: dict[str, UnityComponent] = {}

    def build(raw: dict[str, Any], parent_path: str) -> UnityNode:
        go_id = str(raw.get("$path_id", ""))
        if not go_id:
            raise InputError("有 GameObject 没写 $path_id")
        name = str(raw.get("m_Name", ""))
        node_path = f"{parent_path}/{name}" if parent_path else name
        node = UnityNode(game_object_path_id=go_id, transform_path_id="", name=name, components=[])
        node.path = node_path

        transform_raw = narrow_dict(raw.get("$transform"))
        if "$path_id" in transform_raw:
            tid = str(transform_raw["$path_id"])
            transform = TransformData(
                path_id=tid,
                type_name="RectTransform" if "m_SizeDelta" in transform_raw else "Transform",
                position=_vec3(transform_raw.get("m_LocalPosition"), (0.0, 0.0, 0.0)),
                rotation=_quat(transform_raw.get("m_LocalRotation"), (0.0, 0.0, 0.0, 1.0)),
                scale=_vec3(transform_raw.get("m_LocalScale"), (1.0, 1.0, 1.0)),
                raw=transform_raw,
            )
            node.transform_path_id = tid
            transforms[tid] = transform

        for comp_raw in narrow_list(raw.get("components")):
            component_raw = narrow_dict(comp_raw)
            if not component_raw:
                continue
            cid = str(component_raw.get("$path_id", ""))
            if not cid:
                raise InputError(f"GameObject {go_id} 上有组件没写 $path_id")
            component = UnityComponent(path_id=cid, type_name=_component_type_name(component_raw), data=component_raw)
            components[cid] = component
            node.components.append(component)

        for child_raw in narrow_list(raw.get("children")):
            child = narrow_dict(child_raw)
            if child:
                node.children.append(build(child, node_path))
        games[go_id] = node
        return node

    root = build(document, "")
    return UnityDocument(
        source_path=path,
        root=root,
        games=games,
        transforms=transforms,
        components=components,
    )


def parse_unity_document(path: Path) -> UnityDocument:
    source = narrow_dict(load_json_document(path))
    if not source:
        raise InputError("资源文件的最外层必须是 JSON 对象")
    if "$tree" in source or "$objects" in source:
        raise InputError(f"{path}：旧 RAW 导出格式不再支持，请重新解包成嵌套 GameObject 的 JSON")
    return _parse_document(path, source)


# ── 动画：只取入场状态需要的终值 ───────────────────────────────────────────


type KeyValue = float | Quaternion


@dataclass(frozen=True, slots=True)
class Keyframe:
    time: float
    value: KeyValue


@dataclass(frozen=True, slots=True)
class TransformCurve:
    """某条动画里某一个属性的曲线，可取末尾（clip 结束）的值。

    ``property`` 是属性名（``m_LocalScale.x``、``m_LocalPosition.x`` 等），``path`` 是动画绑定到的节点
    路径（``""`` 表示动画根自己）。
    """

    path: str
    property: str
    keyframes: tuple[Keyframe, ...]

    def end_value(self) -> KeyValue | None:
        return self.keyframes[-1].value if self.keyframes else None


def parse_animation(path: Path) -> list[TransformCurve]:
    """只取终值：入场基线是入场动画**最后**一帧，不是第一帧，也不用 out 或复用的 clip。

    位置、缩放、旋转曲线和 ``m_FloatCurves`` 都读：``m_RotationCurves`` 为空并不等于「这条 clip 不写
    transform」。
    """
    document = load_json_document(path)
    source = narrow_dict(document)
    if not source:
        raise InputError("动画文件的最外层必须是 JSON 对象")
    curves: list[TransformCurve] = []

    def binding_path(raw: dict[str, Any]) -> str:
        binding = narrow_dict(raw.get("binding") or raw.get("m_Binding"))
        return str(raw.get("path", raw.get("m_Path", binding.get("path", binding.get("m_Path", "")))))

    def curve_keys(raw: dict[str, Any]) -> Any:
        curve = narrow_dict(raw.get("curve") or raw)
        return curve.get("m_Curve")

    for raw_curve in narrow_list(source.get("m_PositionCurves")):
        curve = narrow_dict(raw_curve)
        keys = curve_keys(curve)
        for axis in ("x", "y", "z"):
            curves.append(TransformCurve(binding_path(curve), f"m_LocalPosition.{axis}", tuple(_keys(keys, axis))))
    for raw_curve in narrow_list(source.get("m_RotationCurves")):
        curve = narrow_dict(raw_curve)
        curves.append(TransformCurve(binding_path(curve), "m_LocalRotation", tuple(_quat_keys(curve_keys(curve)))))
    for raw_curve in narrow_list(source.get("m_ScaleCurves")):
        curve = narrow_dict(raw_curve)
        keys = curve_keys(curve)
        for axis in ("x", "y", "z"):
            curves.append(TransformCurve(binding_path(curve), f"m_LocalScale.{axis}", tuple(_keys(keys, axis))))
    for raw_curve in narrow_list(source.get("m_FloatCurves")):
        curve = narrow_dict(raw_curve)
        attribute = str(curve.get("attribute", curve.get("m_Attribute", "")))
        name = attribute if attribute.startswith("m_") else f"m_{attribute[:1].upper()}{attribute[1:]}"
        curves.append(TransformCurve(binding_path(curve), name, tuple(_keys(curve_keys(curve), "value"))))
    # 压缩旋转和欧拉角不能因为常规旋转数组为空就被悄悄忽略。
    if source.get("m_CompressedRotationCurves"):
        raise InputError("压缩旋转曲线需要先写解码规则")
    for raw_curve in narrow_list(source.get("m_EulerCurves")):
        curve = narrow_dict(raw_curve)
        for axis in ("x", "y", "z"):
            curves.append(
                TransformCurve(
                    binding_path(curve),
                    f"localEulerAnglesRaw.{axis}",
                    tuple(_keys(curve_keys(curve), axis)),
                )
            )
    return curves


def narrow_list(value: object) -> list[object]:
    """把 JSON 数组收成列表；不是数组就当空列表。"""
    if not isinstance(value, list):
        return []
    items: list[object] = []
    for item in value:  # pyright: ignore[reportUnknownVariableType]
        items.append(item)  # pyright: ignore[reportUnknownArgumentType]
    return items


def narrow_dict(value: object) -> dict[str, Any]:
    """JSON 里拿到的都是无类型对象；只在这里收窄一次，其余模块就能保持有类型。"""
    if not isinstance(value, dict):
        return {}
    narrowed: dict[str, Any] = {}
    for key, item in value.items():  # pyright: ignore[reportUnknownVariableType]
        narrowed[str(key)] = item  # pyright: ignore[reportUnknownArgumentType]
    return narrowed


def _keys(keys_raw: object, field: str) -> list[Keyframe]:
    out: list[Keyframe] = []
    for raw_key in narrow_list(keys_raw):
        key = narrow_dict(raw_key)
        time = float(key.get("time", key.get("m_Time", 0.0)))
        raw_value: Any = key.get("value", key.get(field, 0.0))
        if isinstance(raw_value, dict):
            nested: dict[str, Any] = narrow_dict(dict(raw_value))  # pyright: ignore[reportUnknownArgumentType]
            raw_value = nested.get(field, 0.0)
        out.append(Keyframe(time, float(raw_value)))
    out.sort(key=lambda keyframe: keyframe.time)
    return out


def _quat_keys(keys_raw: object) -> list[Keyframe]:
    out: list[Keyframe] = []
    for raw_key in narrow_list(keys_raw):
        key = narrow_dict(raw_key)
        time = float(key.get("time", key.get("m_Time", 0.0)))
        raw_value: Any = key.get("value", key.get("m_Value", {}))
        out.append(Keyframe(time, _quat(raw_value, (0.0, 0.0, 0.0, 1.0))))
    out.sort(key=lambda keyframe: keyframe.time)
    return out
