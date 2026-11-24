"""uv run test-capture 的入口：按流程文件逐屏截图。

跑流程（--flow 指流程文件，--controller 可以写多个，依次跑）；只写 run 不给参数时，
会列出有哪些流程和控制器可选：

    uv run test-capture run --flow tests/OutpostTrading/capture_flow.py --controller ADB
    uv run test-capture run --flow tests/OutpostTrading/capture_flow.py --controller Win32-Front --controller ADB
    uv run test-capture run --flow tests/OutpostTrading/capture_flow.py --controller CloudWeb
    uv run test-capture run --flow tests/OutpostTrading/capture_flow.py --controller ADB --dry-run

跑到一半按 Ctrl+C 停下，之后用 --resume 接着那个目录继续跑，跑过的步骤不再跑：

    uv run test-capture run --resume .cache/test-capture/20261124-090000

跑之前想看流程长什么样，可以画成 HTML 流程图（不连设备），默认存到 .cache/test-capture/diagrams/：

    uv run test-capture diagram
    uv run test-capture diagram --flow tests/DeliveryJobs/capture_flow.py 流程.html

忘了加 --testset，可以直接把那次运行目录里的截图补导进测试集，不连设备：

    uv run test-capture export .cache/test-capture/20261122-075434

写流程时想让编辑器补全节点名，先用这个生成一次节点名类型存根（跑流程、画流程图也会顺手刷新）：

    uv run test-capture nodes

流程文件的写法、输出目录和已知限制见 tools/test_capture/README.md。
"""

from __future__ import annotations

import io
import json
import signal
import sys
from collections.abc import Callable
from datetime import datetime
from functools import wraps
from pathlib import Path
from typing import Any, cast

import click

from . import cancel
from .diagram import render as render_diagram
from .flow import FlowError, as_dict, as_list, load_flow
from .nodes import NODE_NAMES_STUB, write_node_names
from .output import DEFAULT_KEEP, new_run_dir, prune_runs
from .runner import DEFAULT_WAIT, RunSettings, run_flow, split_segments
from .runtime import ROOT, load_interface, prepare_framework
from .testset import TestSetError, TestSetWriter, export_run

DEFAULT_OUT = ROOT / ".cache/test-capture"
DEFAULT_DIAGRAM_DIR = DEFAULT_OUT / "diagrams"
# 这个脚本能连的控制器类型；interface.json 里其余类型（PlayCover、Linux、MacOS）暂时没接。
SUPPORTED_KINDS = {"Adb": "安卓 ADB", "Win32": "Windows 窗口"}


def _find_flows() -> list[Path]:
    """仓库里所有流程文件：tests/<任务>/capture_flow.py。"""
    return sorted((ROOT / "tests").glob("*/capture_flow.py"))


def _rel(path: Path) -> str:
    try:
        return path.resolve().relative_to(ROOT).as_posix()
    except ValueError:
        return str(path)


def _param(ctx: click.Context, name: str) -> click.Parameter:
    """按名字取当前子命令的某个参数对象，用来让 click 自己拼「缺少参数」的提示。"""
    return next(param for param in ctx.command.params if param.name == name)


def _supported_controllers() -> list[str]:
    """interface.json 里这个脚本能连的控制器名。"""
    entries = [as_dict(item) for item in as_list(load_interface().get("controller"))]
    return [str(entry.get("name")) for entry in entries if entry.get("type") in SUPPORTED_KINDS]


class _FlowPath(click.Path):
    """--flow 的取值：照常按文件路径校验，缺参数时用 click 的格式列出有哪些流程。"""

    def __init__(self) -> None:
        super().__init__(exists=True, dir_okay=False, path_type=Path)

    def get_missing_message(self, param: click.Parameter, ctx: click.Context | None) -> str | None:
        flows = [_rel(path) for path in _find_flows()]
        return f"可选：{'、'.join(flows)}" if flows else None


class _ControllerName(click.ParamType[str]):
    """--controller 的取值：控制器名，缺参数时用 click 的格式列出有哪些可选。"""

    name = "controller"

    def get_missing_message(self, param: click.Parameter, ctx: click.Context | None) -> str | None:
        names = _supported_controllers()
        return f"可选：{'、'.join(names)}" if names else None


def _diagrams(flow_path: Path | None, target: str, controllers: tuple[str, ...]) -> None:
    """画流程图，不连设备也不动游戏。

    给了 --flow：target 是 HTML 文件路径，空着就存到默认目录；
    没给 --flow：把所有流程都画一遍，target 是输出目录，空着就是默认目录。
    每张图按流程所在的任务目录命名，例如 DeliveryJobs.html。
    """
    if flow_path is not None:
        flows = [flow_path.resolve()]
        out = Path(target) if target else DEFAULT_DIAGRAM_DIR / f"{flow_path.resolve().parent.name}.html"
        outputs = [out]
    else:
        flows = _find_flows()
        if not flows:
            raise FlowError("tests/ 下没有 capture_flow.py，没有流程可画")
        folder = Path(target) if target else DEFAULT_DIAGRAM_DIR
        outputs = [folder / f"{path.parent.name}.html" for path in flows]
    for path, out in zip(flows, outputs, strict=True):
        flow = load_flow(path)
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(render_diagram(flow, controllers), encoding="utf-8")
        click.echo(f"流程图：{_rel(out)}（{flow.name}）")
    if not controllers:
        click.echo("带上 --controller 能把每段谁先跑、谁跳过、在哪换平台也画出来")


def _read_manifest(run_dir: Path) -> dict[str, Any]:
    """读一个运行目录的 manifest；不是运行目录就报错。"""
    manifest_path = run_dir / "manifest.json"
    if not manifest_path.is_file():
        raise FlowError(f"{run_dir} 里没有 manifest.json，不是 test-capture 的运行目录")
    return cast("dict[str, Any]", json.loads(manifest_path.read_text(encoding="utf-8")))


def _write_manifest(run_dir: Path, manifest: dict[str, Any]) -> None:
    """把进度写进 manifest.json：先写临时文件再换名，中断也不会留下半个文件。"""
    manifest["updatedAt"] = datetime.now().isoformat(timespec="seconds")
    path = run_dir / "manifest.json"
    tmp = run_dir / "manifest.json.tmp"
    tmp.write_text(json.dumps(manifest, ensure_ascii=False, indent=4) + "\n", encoding="utf-8")
    tmp.replace(path)


def _run(
    flow_path: Path | None,
    controllers: tuple[str, ...],
    resume: Path | None,
    out_dir: Path,
    adb_path: str | None,
    adb_serial: str | None,
    maafw: str | None,
    dry_run: bool,
    open_game: bool,
    keep: int,
    wait: float,
    testset: bool,
    testset_resource: str,
    testset_dir: str | None,
) -> Path:
    previous: list[dict[str, Any]] = []
    manifest: dict[str, Any] = {}
    if resume is not None:
        # 接着上一次中断的运行继续跑：目录、流程文件、平台都沿用那边的记录。
        run_dir = resume.resolve()
        manifest = _read_manifest(run_dir)
        previous = cast("list[dict[str, Any]]", manifest.get("controllers") or [])
        if flow_path is None:
            recorded = str(manifest.get("flowFile") or "")
            if not recorded:
                raise FlowError("运行目录里没记流程文件，接着跑要自己给 --flow")
            flow_path = Path(recorded)
        if not controllers:
            controllers = tuple(str(item.get("controller") or "") for item in previous)
    else:
        run_dir = new_run_dir(out_dir.resolve())
    if flow_path is None:
        raise click.UsageError("跑流程要给 --flow，或者用 --resume 接着上一次运行")
    if not controllers:
        raise click.UsageError("跑流程要给 --controller，或者用 --resume 接着上一次运行")

    flow = load_flow(flow_path.resolve())
    interface = load_interface()
    prepare_framework(Path(maafw) if maafw else None)

    if resume is None:
        for stale in prune_runs(out_dir.resolve(), keep):
            click.echo(f"清掉旧截图：{stale.name}")
        manifest = {
            "flow": flow.name or flow_path.name,
            "flowFile": str(flow_path.resolve()),
            "dryRun": dry_run,
            "startedAt": datetime.now().isoformat(timespec="seconds"),
            "controllers": [],
        }
    else:
        manifest["flow"] = flow.name or flow_path.name
        manifest["flowFile"] = str(flow_path.resolve())
        manifest["dryRun"] = dry_run
    click.echo(f"流程「{manifest['flow']}」共 {len(flow.steps)} 步，输出到 {run_dir}")

    settings = RunSettings(
        adb_path=adb_path,
        adb_serial=adb_serial,
        dry_run=dry_run,
        open_game=open_game,
        wait=wait,
    )
    writer: TestSetWriter | None = None
    if testset:
        folder = testset_dir or flow_path.resolve().parent.name
        writer = TestSetWriter(ROOT, testset_resource, folder)

    def on_progress(items: list[dict[str, Any]], once: dict[str, str]) -> None:
        manifest["controllers"] = items
        manifest["once"] = once
        _write_manifest(run_dir, manifest)

    _write_manifest(run_dir, manifest)
    try:
        results = run_flow(interface, flow, controllers, run_dir, settings, writer, previous, on_progress)
    except cancel.Cancelled:
        cancel.reset()
        _write_manifest(run_dir, manifest)
        click.echo(f"已中断；进度已经存下，用 --resume {run_dir} 接着跑，跑过的步骤不会再跑", err=True)
        raise click.exceptions.Exit(130) from None
    manifest["controllers"] = results
    failed = [item["controller"] for item in results if item.get("error")]
    segments = split_segments(flow)
    skipped = [
        item["controller"]
        for item in results
        if not item.get("error")
        and len(item.get("segments") or []) + len(item.get("skippedSegments") or []) < len(segments)
    ]
    # 只能做一次的步骤：第一个跑到的平台做完，后面到的平台跳过，这几段它们本来就没有图。
    once_skipped = [
        (str(item["controller"]), len(item.get("skippedSegments") or []))
        for item in results
        if item.get("skippedSegments")
    ]

    _write_manifest(run_dir, manifest)
    if writer is not None:
        where = "tests/MaaEndTestset/<控制器>/<资源>/" + writer.folder
        if failed:
            click.echo(f"有平台没跑完，这次的测试图不全，没写完的步骤没有新图；目录：{where}", err=True)
        elif skipped:
            click.echo(f"有平台因为前面失败，后面几段没跑，图不全；目录：{where}", err=True)
        else:
            click.echo(f"测试图：{where}（用例里 imageRoot 写 {writer.folder} 才对得上）")
    if len(segments) > 1:
        click.echo(f"共 {len(segments)} 段，每段在 {len(controllers)} 个平台上各跑一遍")
    if once_skipped:
        detail = "、".join(f"{name} 跳过 {count} 段" for name, count in once_skipped)
        click.echo(f"有的步骤只能做一次，别的平台跳过了（{detail}），那几段没有图")
    if failed:
        click.echo(f"失败的平台：{'、'.join(failed)}", err=True)
    click.echo(f"{'结束（有失败）' if failed else '完成'}：{run_dir}")
    if failed:
        raise click.exceptions.Exit(1)
    return run_dir


def _refresh_node_names(always_report: bool) -> None:
    """按当前 pipeline 刷新节点名类型存根，编辑器靠它补全节点名、标出写错的。"""
    count, changed = write_node_names(ROOT)
    if changed or always_report:
        where = NODE_NAMES_STUB.relative_to(ROOT).as_posix()
        state = "已更新" if changed else "没有变化"
        click.echo(f"节点名类型存根{state}：{where}，共 {count} 个节点")


def _export(run_dir: Path, resource: str, folder: str | None) -> None:
    """把一次运行目录里的截图重新脱敏导出到测试集，不连设备。"""
    run_dir = run_dir.resolve()
    manifest_path = run_dir / "manifest.json"
    if not manifest_path.is_file():
        raise FlowError(f"{run_dir} 里没有 manifest.json，不是 test-capture 的运行目录")
    if folder is None:
        manifest = cast("dict[str, Any]", json.loads(manifest_path.read_text(encoding="utf-8")))
        flow_file = str(manifest.get("flowFile") or "")
        folder = Path(flow_file).parent.name if flow_file else None
        if folder is None:
            raise FlowError("运行目录里没记流程文件，不知道写进哪个任务目录；用 --testset-dir 指定")
    click.echo(f"从 {run_dir.name} 导出测试图，任务目录 {folder}")
    written = export_run(run_dir, ROOT, resource, folder)
    for path in written:
        click.echo(f"  {path}")
    click.echo(f"共 {len(written)} 张，在 tests/MaaEndTestset/<控制器>/<资源>/{folder}/")
    click.echo("用例文件不会自动生成：把这些图片名和 imageRoot 补到 tests/<任务>/test_*.json 里")


# 每个子命令的 -h 和 --help 通用。
HELP = {"help_option_names": ["-h", "--help"]}


def _errors(func: Callable[..., None]) -> Callable[..., None]:
    """子命令统一收尾：流程自己的报错直接给一句话，硬中断不多打堆栈。"""

    @wraps(func)
    def wrapper(*args: Any, **kwargs: Any) -> None:
        try:
            func(*args, **kwargs)
        except (FlowError, TestSetError) as error:
            raise click.ClickException(str(error)) from error
        except KeyboardInterrupt:
            # 连按两次 Ctrl+C 之类的硬中断，别打一屏堆栈出来。
            raise click.exceptions.Exit(130) from None

    return wrapper


class _Group(click.Group):
    """子命令写错时，列出有哪些子命令。"""

    def resolve_command(
        self, ctx: click.Context, args: list[str]
    ) -> tuple[str | None, click.Command | None, list[str]]:
        try:
            return super().resolve_command(ctx, args)
        except click.UsageError:
            commands = "、".join(self.list_commands(ctx))
            raise click.UsageError(f"没有 {args[0]!r} 这个子命令，可选的：{commands}") from None


@click.group(cls=_Group, context_settings=HELP)
def _command() -> None:
    """按预先写好的步骤流程逐屏截图。"""


@_command.command("run", context_settings=HELP)
@click.option(
    "--flow",
    "flow_path",
    type=_FlowPath(),
    default=None,
    help="流程文件，一般放 tests/<任务>/capture_flow.py",
)
@click.option(
    "--controller",
    "controllers",
    type=_ControllerName(),
    multiple=True,
    help="assets/interface.json 里的控制器名（ADB / Win32-Front / CloudWeb 等），可重复以依次跑多个平台",
)
@click.option(
    "--resume",
    "resume",
    type=click.Path(exists=True, file_okay=False, path_type=Path),
    default=None,
    help="接着上一次中断的运行继续跑：给个运行目录，跑过的步骤不会再跑",
)
@click.option(
    "--out",
    "out_dir",
    type=click.Path(path_type=Path),
    default=DEFAULT_OUT,
    show_default=False,
    help="输出根目录，默认 .cache/test-capture",
)
@click.option("--adb-path", default=None, help="adb 可执行文件路径，默认自动查找")
@click.option("--adb-serial", default=None, help="设备地址，例如 127.0.0.1:5555")
@click.option("--maafw", default=None, help="MaaFramework 动态库目录，默认 install/maafw")
@click.option("--dry-run", is_flag=True, help="只截图不点击，用来先对流程")
@click.option(
    "--open-game/--no-open-game",
    default=True,
    show_default=True,
    help="开跑前先跑控制器自己的预任务再回大世界：云网页版拉起外壳、桌面版拉起游戏，安卓端没开就跑 OpenGame 任务",
)
@click.option(
    "--keep",
    type=click.IntRange(min=1),
    default=DEFAULT_KEEP,
    show_default=True,
    help="输出目录里保留最近几次运行",
)
@click.option(
    "--wait",
    type=click.FloatRange(min=0),
    default=DEFAULT_WAIT,
    show_default=True,
    help="每次截图前等待画面稳定的秒数，步骤里可以用 shot(settle=) 单独改",
)
@click.option(
    "--testset/--no-testset",
    default=False,
    help="截图脱敏后写进 tests/MaaEndTestset/<控制器>/<资源>/<任务目录>",
)
@click.option(
    "--testset-resource",
    default="官服",
    show_default=True,
    help="测试集的 configs.resource，对应 tests/MaaEndTestset/<控制器>/<资源目录>",
)
@click.option(
    "--testset-dir",
    default=None,
    help="资源服目录下的任务子目录，默认用流程文件所在的文件夹名",
)
@_errors
@click.pass_context
def _run_command(
    ctx: click.Context,
    flow_path: Path | None,
    controllers: tuple[str, ...],
    resume: Path | None,
    out_dir: Path,
    adb_path: str | None,
    adb_serial: str | None,
    maafw: str | None,
    dry_run: bool,
    open_game: bool,
    keep: int,
    wait: float,
    testset: bool,
    testset_resource: str,
    testset_dir: str | None,
) -> None:
    """按流程文件逐屏截图。"""
    if flow_path is None and resume is None:
        # 没给流程，也没说接着哪次运行：让 click 报「缺少 --flow」，并带上可选的流程。
        raise click.MissingParameter(ctx=ctx, param=_param(ctx, "flow_path"))
    if flow_path is not None and resume is None and not controllers:
        raise click.MissingParameter(ctx=ctx, param=_param(ctx, "controllers"))
    _refresh_node_names(always_report=False)
    _run(
        flow_path,
        controllers,
        resume,
        out_dir,
        adb_path,
        adb_serial,
        maafw,
        dry_run,
        open_game,
        keep,
        wait,
        testset,
        testset_resource,
        testset_dir,
    )


@_command.command("diagram", context_settings=HELP)
@click.option(
    "--flow",
    "flow_path",
    type=click.Path(exists=True, dir_okay=False, path_type=Path),
    default=None,
    help="只画这一个流程；不写就画 tests/ 下所有流程",
)
@click.option(
    "--controller",
    "controllers",
    multiple=True,
    help="带上平台名，图里会标出每段谁先跑、谁跳过、在哪换平台",
)
@click.argument("target", required=False, default=None)
@_errors
def _diagram_command(flow_path: Path | None, controllers: tuple[str, ...], target: str | None) -> None:
    """画 HTML 流程图，不连设备也不动游戏。

    TARGET 不给就存到 .cache/test-capture/diagrams/；带 --flow 时 TARGET 是这一个流程的 HTML 文件，不带 --flow 时是这个目录，里面每个流程一张。
    """
    _refresh_node_names(always_report=False)
    _diagrams(flow_path, target or "", controllers)


@_command.command("export", context_settings=HELP)
@click.argument("run_dir", type=click.Path(exists=True, file_okay=False, path_type=Path))
@click.option(
    "--testset-resource",
    default="官服",
    show_default=True,
    help="测试集的 configs.resource，对应 tests/MaaEndTestset/<控制器>/<资源目录>",
)
@click.option(
    "--testset-dir",
    default=None,
    help="资源服目录下的任务子目录，默认用运行目录里记的流程文件所在文件夹名",
)
@_errors
def _export_command(run_dir: Path, testset_resource: str, testset_dir: str | None) -> None:
    """把一次运行目录里的截图脱敏补导进测试集，不连设备。

    忘了加 --testset 时用它补导，不用重跑。
    """
    _export(run_dir, testset_resource, testset_dir)


@_command.command("nodes", context_settings=HELP)
@_errors
def _nodes_command() -> None:
    """刷新节点名类型存根，编辑器靠它补全节点名、标出写错的。

    跑流程和画流程图时也会自动刷新，内容没变就不写。
    """
    _refresh_node_names(always_report=True)


def _on_sigint(signum: int, frame: object) -> None:
    """Ctrl+C：只记一笔，等当前这一步的等待循环看到标记再停。"""
    if cancel.requested():
        # 第二次再按就别等了，直接抛出去，由上层收尾。
        raise KeyboardInterrupt
    cancel.request()


def main() -> None:
    # 报错也走 stderr，不一起改成 UTF-8 的话中文在 GBK 控制台上是乱码。
    cast("io.TextIOWrapper", sys.stdout).reconfigure(encoding="utf-8")
    cast("io.TextIOWrapper", sys.stderr).reconfigure(encoding="utf-8")
    signal.signal(signal.SIGINT, _on_sigint)
    _command()


if __name__ == "__main__":
    main()
