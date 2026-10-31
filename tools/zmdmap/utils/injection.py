"""按 ``build`` 的参数注入输入服务（照着 nonebot 的依赖注入做法）。

数据集不接收一个装满输入的容器，而是在 :meth:`~tools.zmdmap.datasets.base.Dataset.build` 的参数上直接写
自己需要什么：

- 参数写类型注解 ``TableCfg`` / ``JsonData`` / ``LuaScripts`` / ``Assets``（可以用 ``Annotated`` 包一层）；
- 或者不写注解、参数名正好是 ``table_cfg`` / ``json_data`` / ``lua`` / ``assets``。

生成前先把签名解析成「参数 -> 服务」，运行时按参数名传进去；认不出来的参数、以及没声明要的服务都不会传。
有默认值的参数当作普通参数，不参与注入。命令行要知道「这次要检查哪些输入目录」时也读这份解析结果，所以
数据集不必再声明 ``requires_lua`` 之类的开关。
"""

import inspect
from collections.abc import Callable, Mapping
from dataclasses import dataclass
from typing import Annotated, Any, get_args, get_origin

from .inputs import Assets, JsonData, LuaScripts, TableCfg

#: 参数注解（类型）-> 服务名。
SERVICE_TYPES: dict[Any, str] = {
    TableCfg: "table_cfg",
    JsonData: "json_data",
    LuaScripts: "lua",
    Assets: "assets",
}

#: 没写注解时按参数名认的服务。
SERVICE_NAMES: tuple[str, ...] = ("table_cfg", "json_data", "lua", "assets")

#: 服务名 -> 命令行里对应的目录选项（``--list`` 与报错提示用）。
SERVICE_OPTIONS: dict[str, str] = {
    "table_cfg": "--table-cfg-dir",
    "json_data": "--json-data-dir",
    "lua": "--lua-dir",
    "assets": "--assets-dir",
}


class InjectionError(ValueError):
    """``build`` 的参数没法对应到输入服务：写错了名字或类型，或者写了 ``*args`` / ``**kwargs``。"""


@dataclass(frozen=True, slots=True)
class Injection:
    """一个 ``build`` 要注入的参数：``(参数名, 服务名)``。"""

    params: tuple[tuple[str, str], ...]

    @property
    def services(self) -> tuple[str, ...]:
        """要准备哪些服务（去重，保持声明顺序）。"""
        return tuple(dict.fromkeys(service for _, service in self.params))

    def kwargs(self, services: Mapping[str, Any]) -> dict[str, Any]:
        """按参数名把服务取出来，直接可以 ``build(**kwargs)``。"""
        return {name: services[service] for name, service in self.params}


def parse_injection(build: Callable[..., Any]) -> Injection:
    """解析 ``build`` 的签名；认不出来的参数（且没有默认值）报 :class:`InjectionError`。"""
    pairs: list[tuple[str, str]] = []
    for name, parameter in inspect.signature(build).parameters.items():
        if parameter.kind in (inspect.Parameter.VAR_POSITIONAL, inspect.Parameter.VAR_KEYWORD):
            raise InjectionError(
                f"{name!r}：build 不接受 *args / **kwargs，请把要用的输入服务写成具名参数（可用：{_names()}）"
            )
        service = _service_of(name, parameter)
        if service is None:
            if parameter.default is not inspect.Parameter.empty:
                # 有默认值：当作普通参数，用默认值，不参与注入。
                continue
            raise InjectionError(
                f"{name!r} 没有对应的输入服务：写类型注解（TableCfg / JsonData / LuaScripts / Assets），"
                f"或者用同名参数（可用：{_names()}）"
            )
        pairs.append((name, service))
    return Injection(tuple(pairs))


def _service_of(name: str, parameter: inspect.Parameter) -> str | None:
    annotation = parameter.annotation
    if annotation is inspect.Parameter.empty:
        return name if name in SERVICE_NAMES else None
    if get_origin(annotation) is Annotated:
        annotation = get_args(annotation)[0]
    return SERVICE_TYPES.get(annotation)


def _names() -> str:
    return "、".join(SERVICE_NAMES)
