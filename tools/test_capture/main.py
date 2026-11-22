"""uv run test-capture 的入口：按流程文件逐屏截图。

用法::

    uv run test-capture --flow tests/OutpostTrading/capture_flow.py --controller ADB
    uv run test-capture --flow tests/OutpostTrading/capture_flow.py --controller Win32-Front --controller ADB
    uv run test-capture --flow tests/OutpostTrading/capture_flow.py --controller CloudWeb
    uv run test-capture --flow tests/OutpostTrading/capture_flow.py --controller ADB --dry-run

忘了加 --testset 可以用 --from-run 补导：走到运行目录，不连设备再看一遍流程：

    uv run test-capture --from-run .cache/test-capture/20261122-075434

流程文件的写法、输出目录和已知限制见 tools/test_capture/README.md。
"""

from __future__ import annotations

import io
import json
import sys
from datetime import datetime
from pathlib import Path
from typing import Any, cast

import click

from .flow import FlowError, load_flow
from .output import DEFAULT_KEEP, new_run_dir, prune_runs
from .runner import DEFAULT_WAIT, RunSettings, run_controller
from .runtime import ROOT, load_interface, prepare_framework
from .testset import TestSetError, TestSetWriter, export_run

DEFAULT_OUT = ROOT / ".cache/test-capture"


def _run(
    flow_path: Path,
    controllers: tuple[str, ...],
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
    flow = load_flow(flow_path.resolve())
    interface = load_interface()
    prepare_framework(Path(maafw) if maafw else None)

    root = out_dir.resolve()
    run_dir = new_run_dir(root)
    for stale in prune_runs(root, keep):
        click.echo(f"清掉旧截图：{stale.name}")
    manifest: dict[str, Any] = {
        "flow": flow.name or flow_path.name,
        "flowFile": str(flow_path.resolve()),
        "dryRun": dry_run,
        "startedAt": datetime.now().isoformat(timespec="seconds"),
        "controllers": [],
    }
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

    failed = False
    for name in controllers:
        try:
            if writer is not None:
                writer.start_controller(name)
            result = run_controller(interface, flow, name, run_dir / name, settings, writer)
            manifest["controllers"].append(result)
        except (FlowError, TestSetError) as error:
            failed = True
            click.echo(str(error), err=True)
            manifest["controllers"].append(
                {
                    "controller": name,
                    "dir": str(run_dir / name),
                    "steps": [],
                    "error": {"message": str(error)},
                }
            )

    (run_dir / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=4) + "\n", encoding="utf-8")
    if writer is not None:
        where = "tests/MaaEndTestset/<控制器>/<资源>/" + writer.folder
        if failed:
            click.echo(f"有平台没跑完，这次的测试图不全，没写完的步骤没有新图；目录：{where}", err=True)
        else:
            click.echo(f"测试图：{where}（用例里 imageRoot 写 {writer.folder} 才对得上）")
    click.echo(f"{'结束（有失败）' if failed else '完成'}：{run_dir}")
    if failed:
        raise click.exceptions.Exit(1)
    return run_dir


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


@click.command(context_settings={"help_option_names": ["-h", "--help"]})
@click.option(
    "--flow",
    "flow_path",
    type=click.Path(exists=True, dir_okay=False, path_type=Path),
    default=None,
    help="流程文件，一般放 tests/<任务>/capture_flow.py",
)
@click.option(
    "--controller",
    "controllers",
    multiple=True,
    help="assets/interface.json 里的控制器名（Adb / Win32，含 CloudWeb 云网页版），可重复以依次跑多个平台",
)
@click.option(
    "--from-run",
    "from_run",
    type=click.Path(exists=True, file_okay=False, path_type=Path),
    default=None,
    help="已有运行目录：只把里面的截图脱敏导出到测试集，不再连设备（忘了 --testset 时补导用）",
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
def _command(
    flow_path: Path | None,
    controllers: tuple[str, ...],
    from_run: Path | None,
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
    """按预先写好的步骤流程逐屏截图。"""
    try:
        if from_run is not None:
            _export(from_run, testset_resource, testset_dir)
            return
        if flow_path is None or not controllers:
            raise click.UsageError("跑流程要同时给 --flow 和 --controller；只导测试集用 --from-run")
        _run(
            flow_path,
            controllers,
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
    except (FlowError, TestSetError) as error:
        raise click.ClickException(str(error)) from error


def main() -> None:
    # 报错也走 stderr，不一起改成 UTF-8 的话中文在 GBK 控制台上是乱码。
    cast("io.TextIOWrapper", sys.stdout).reconfigure(encoding="utf-8")
    cast("io.TextIOWrapper", sys.stderr).reconfigure(encoding="utf-8")
    _command()


if __name__ == "__main__":
    main()
