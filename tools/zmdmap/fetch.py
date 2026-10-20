"""``zmdmap fetch``：从 zmdmap 下载已发布的原始数据，再入库。

上游发布的清单（``manifest.json``）给出这批数据是哪一版、每份原始形态的内容指纹，数据按
``?ver=<版本>&t=<指纹前 8 位>`` 从发布 CDN 拉取。清单还没发布时不比对，这几份直接下载；无论清单在不在，
下载回来的文件都原样保留上游写的 ``version`` 与 ``sha256``（它们是这份数据原始形态的标签与批次标识），
这里只读不改。下哪几份由调用方给出 —— 各任务生成器只要自己用的那几份，就只下那几份，不必被整包牵着走。

本地已是最新时跳过下载：逐份拿本地文件里自称的 ``sha256`` 跟清单比，一样的跳过，不一样的才下；下完核对
一遍拿到的内容（不符只提示、不当失败）。清单本身每次都带时间戳取 —— 它只有几百字节，却决定了整批判断，
被远端缓存住就会拿旧索引判断。无论下载与否都会跑一次入库。上游还没发布的数据只提示、不当失败：本地可以用
``uv run zmdmap generate --only <名字>`` 生成。

发布的都是**原始形态**（只带游戏世界坐标），一律先落不入库的 ``data/raw/``，再由
:mod:`tools.zmdmap.materialize` 材料化进入库的 ``data/``：需要投影的补上底图像素，不需要投影的原样复制。

下载走 :mod:`tools.zmdmap.utils.http` 的共享连接池并发拉取：数据文件各自独立，串行等一个文件会把整包耗时乘上文件数。
"""

import json
import sys
import time
from collections.abc import Sequence
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import urlencode

import click
import httpx

from .datasets import DATASET_NAMES, files_of, select_datasets
from .materialize import copy_plain, report_data_changes, run_projection
from .utils.config import DATA_BASE_URL, DEFAULT_OUT_DIR, MANIFEST_FILE, ZMDMAP_DATA_DIR
from .utils.datafile import (
    HASH_KEY,
    dump_json,
    payload_hash,
    payload_of,
    read_hash,
    version_of,
)
from .utils.http import get, new_client

#: 并发下载的连接数；共用 :func:`tools.zmdmap.utils.http.new_client` 的连接池。
DOWNLOAD_WORKERS = 4


class DownloadError(ValueError):
    """一份数据下载失败；``status_code`` 为 ``None`` 表示网络层错误。"""

    def __init__(self, file_name: str, message: str, *, status_code: int | None = None) -> None:
        super().__init__(f"下载 {file_name} 失败: {message}")
        self.file_name = file_name
        self.status_code = status_code


@dataclass
class Manifest:
    """上游发布的清单：这批数据是哪一版，以及每份原始形态的内容指纹。"""

    version: str | None
    files: dict[str, str]


def parse_manifest(text: str) -> Manifest | None:
    """解析清单；形状不对就当没清单（上游还是老形态，或者拿到的是错误页）。"""
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return None
    if not isinstance(data, dict):
        return None
    files = data.get("files")
    if not isinstance(files, dict):
        return None
    return Manifest(
        version_of(data), {name: value for name, value in files.items() if isinstance(value, str)}
    )


def read_manifest() -> Manifest | None:
    """取上游清单；没发布（404）或形状不对时返回 ``None``，由调用方按「不比对」处理。

    清单决定了「后面下哪几份」，被远端缓存住就等于拿旧索引判断，所以每次带一个时间戳当 URL 的一部分。
    实测这个 CDN 只看 URL：请求头带 ``Cache-Control: no-cache`` 照样命中缓存（``Age`` 不变），换 URL 才真的
    回源。只有它需要这样 —— 数据文件的 URL 自带清单给的内容指纹（``t=<指纹前 8 位>``），本来就随内容变。
    """
    # 清单只有几百字节，每次都回源不心疼；它错了会连带整批判断都错。
    token = urlencode({"t": int(time.time() * 1000)})
    try:
        with new_client() as client:
            response = get(client, f"{DATA_BASE_URL}/{MANIFEST_FILE}?{token}")
    except httpx.HTTPError as error:
        # 5xx 重试完仍失败也走这条路：取不到清单就直接下载，不让清单的问题挡住整次 fetch。
        print(f"[fetch-data] 取清单失败 ({error})", file=sys.stderr)
        return None
    if response.status_code >= 400:
        return None
    return parse_manifest(response.text)


def manifest_plan(
    target_dir: Path, file_names: Sequence[str], manifest: Manifest
) -> tuple[list[str], list[str]]:
    """按清单挑出要重下的文件，并另列出「不在这批里、保持本地原样」的那几份。

    比的是 ``target_dir`` 里同名文件**自称的** ``sha256`` 与清单里记的指纹
    （:func:`tools.zmdmap.utils.datafile.read_hash`）—— 只读字段、不读本体。与文件里写的 ``version``
    无关：上游只换了版本号、内容没变时不必重下，本地那份也一个字都不会改。

    清单列的是「这一批发了哪几份」（``generate --only`` 就只列那几份），所以清单里没有的那份不是「过期」、
    是「这批没它」：本地已经有就不动它，本地也没有才只能下回来，否则这份数据就彻底缺了。

    ``target_dir`` 是「本地这份数据」所在的地方：入库模式下就是入库目录（``data/``，它在仓库里，新克隆
    下来也有），``--raw-only`` 下是原始形态目录自己（那正是它的产物）。原始形态目录是 ``.gitignore`` 的
    本地缓存，空着不代表本地数据旧，所以默认不拿它当参照。
    """
    todo: list[str] = []
    unlisted: list[str] = []
    for file_name in file_names:
        local = read_hash(target_dir / file_name)
        expected = manifest.files.get(file_name)
        if local is None:  # 本地没有：只能下回来，清单里有没有都一样
            todo.append(file_name)
        elif expected is None:  # 不在这批里：保持本地那份
            unlisted.append(file_name)
        elif local != expected:  # 指纹对不上：过期了
            todo.append(file_name)
    return todo, unlisted


def _download_json(
    client: httpx.Client,
    file_name: str,
    destination: Path,
    version: str | None,
    token: str | None,
    expect: str | None = None,
) -> None:
    params: dict[str, str] = {}
    if version:
        params["ver"] = version
    if token:
        params["t"] = token
    url = f"{DATA_BASE_URL}/{file_name}" + (f"?{urlencode(params)}" if params else "")
    print(f"[fetch-data] 下载 {url}", flush=True)
    try:
        response = get(client, url)
    except httpx.TransportError as error:
        raise DownloadError(file_name, str(error)) from error
    if response.status_code >= 400:
        raise DownloadError(
            file_name,
            f"{response.status_code} {response.reason_phrase}",
            status_code=response.status_code,
        )
    try:
        data = json.loads(response.text)
    except json.JSONDecodeError as error:
        raise DownloadError(file_name, f"不是合法 JSON：{error}") from error

    # 元数据原样保留：``version`` 与 ``sha256`` 都是上游 generate 写的（``sha256`` 是这份原始形态的批次
    # 标识），这里一个字都不改，只按仓库的写法把文件重新排版。清单里的指纹与文件自称的指纹都跟本体重算的
    # 结果对一遍，不符说明拿到的不是它该有的那一份（远端缓存串了 / 上游算错了）。
    payload = payload_of(data)
    digest = payload_hash(payload)
    if expect and digest != expect:
        print(
            f"[fetch-data] {file_name} 与清单不符：清单 {expect}，拿到 {digest}"
            "（远端缓存串了？可以加 --force 再试）",
            file=sys.stderr,
        )
    if isinstance(data, dict):
        known = data.get(HASH_KEY)
        if not known:
            print(
                f"[fetch-data] {file_name} 没带 sha256 字段，只能照收（指纹由上游 generate 写）",
                file=sys.stderr,
            )
        elif known != digest:
            print(
                f"[fetch-data] {file_name} 自称的 sha256 与内容不符：{known} != {digest}",
                file=sys.stderr,
            )
        if not version_of(data):
            print(
                f"[fetch-data] {file_name} 没带 version 字段，只能照收（版本由上游 generate 写）",
                file=sys.stderr,
            )
    formatted = dump_json(data)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(formatted, encoding="utf-8", newline="\n")
    size_kb = len(formatted.encode("utf-8")) / 1024
    print(f"[fetch-data] 已缓存 {destination} ({size_kb:.0f} KB)", flush=True)


def fetch_and_cache(
    raw_dir: Path,
    version: str | None,
    projected_files: Sequence[str],
    plain_files: Sequence[str],
    *,
    token: str | None = None,
    hashes: dict[str, str] | None = None,
) -> list[str]:
    """并发把这几份发布的原始数据下载到 ``raw_dir``；返回上游还没发布、被跳过的文件名。

    ``hashes`` 是清单给的每份内容指纹：有指纹的用指纹前 8 位当 ``t`` 参数 —— 同一份内容的 URL 稳定、内容
    一变就是新 URL，绕开远端缓存。``token`` 给了就替掉指纹（``--force`` 时是时间戳），让这几份一律回源。
    """
    hashes = hashes or {}
    raw_dir.mkdir(parents=True, exist_ok=True)
    failures: dict[str, DownloadError] = {}
    with new_client() as client, ThreadPoolExecutor(max_workers=DOWNLOAD_WORKERS) as executor:
        pending = {
            file_name: executor.submit(
                _download_json,
                client,
                file_name,
                raw_dir / file_name,
                version,
                token or hashes.get(file_name, "")[:8],
                hashes.get(file_name),
            )
            for file_name in (*projected_files, *plain_files)
        }
        for file_name, future in pending.items():
            try:
                future.result()
            except DownloadError as error:
                failures[file_name] = error

    # 需要投影的数据必须齐全；原样复制的允许上游还没发布（404 时跳过）。
    for file_name in projected_files:
        if file_name in failures:
            raise failures[file_name]

    missing: list[str] = []
    for file_name in plain_files:
        error = failures.get(file_name)
        if error is None:
            continue
        if error.status_code != 404:
            raise error
        print(
            f"[fetch-data] 上游还没发布 {file_name}，跳过（本地可用 uv run zmdmap generate 生成）",
            file=sys.stderr,
        )
        missing.append(file_name)

    return missing


def download(
    raw_dir: Path,
    target_dir: Path,
    projected_files: Sequence[str],
    plain_files: Sequence[str],
    *,
    force: bool = False,
) -> list[str]:
    """按需把这几份数据下载到 ``raw_dir``；返回这次**真的下过**的文件名（也就是还要入库的那几份）。

    本地是不是最新以发布清单为准：逐份拿 ``target_dir`` 里同名文件**自称的** ``sha256`` 跟清单比，一样的跳过
    下载，不一样的才下 —— 不必问任何接口，也认得「同一个版本号下重新发布过」；``version`` 不参与比较。
    清单还没发布时不比对，这几份直接下载；``version`` 与 ``sha256`` 都沿用数据文件自带的（上游 ``generate``
    写进文件的那份），这里不改它们。

    ``force`` 是不比对、直接重下这几份，并且请求 URL 上的 ``t`` 用时间戳而不是清单指纹 —— 「怀疑远端说辞」
    时的唯一手段（这个 CDN 只看 URL，请求头说了不算）。CI 上没有额外开关：本地与 CI 跑的是同一套判断。
    """
    todo = [*projected_files, *plain_files]
    token = str(int(time.time() * 1000)) if force else None

    manifest = read_manifest()
    version = manifest.version if manifest is not None else None
    if force:
        print("[fetch-data] 强制下载（--force），不比对本地内容")
    elif manifest is not None:
        todo, unlisted = manifest_plan(target_dir, todo, manifest)
        label = f"v{version}" if version else "清单没写版本"
        if unlisted:
            print(
                f"[fetch-data] 清单里没有这几份，保持本地原样：{'、'.join(unlisted)}",
                file=sys.stderr,
            )
        if todo:
            print(f"[fetch-data] 清单 {label}：{len(todo)} 份内容有变化")
        else:
            print(f"[fetch-data] 本地内容与清单一致（{label}），跳过下载")
            return []
    else:
        print(
            f"[fetch-data] 上游还没发布 {MANIFEST_FILE}，不比对本地内容，直接下载"
            "（版本沿用数据文件自带的）",
            file=sys.stderr,
        )

    hashes = manifest.files if manifest is not None else {}
    written = list(todo)
    if todo:
        pending = set(todo)
        missing = fetch_and_cache(
            raw_dir,
            version,
            [file_name for file_name in projected_files if file_name in pending],
            [file_name for file_name in plain_files if file_name in pending],
            token=token,
            hashes=hashes,
        )
        if missing:
            print(f"[fetch-data] 上游还没发布：{', '.join(missing)}", file=sys.stderr)
            unpublished = set(missing)
            written = [file_name for file_name in todo if file_name not in unpublished]
    print(f"[fetch-data] 完成，当前版本: v{version}" if version else "[fetch-data] 完成")
    return written


@click.command(name="fetch")
@click.option(
    "--out-dir",
    type=click.Path(path_type=Path, file_okay=False),
    default=DEFAULT_OUT_DIR,
    show_default=True,
    help="原始数据目录（原样产物，只带世界坐标）",
)
@click.option(
    "--data-dir",
    type=click.Path(path_type=Path, file_okay=False),
    default=ZMDMAP_DATA_DIR,
    show_default=True,
    help="入库数据目录（需要投影的投影后落这里，其余原样复制进来）",
)
@click.option(
    "--only",
    type=click.Choice(DATASET_NAMES),
    multiple=True,
    help="只处理列出的数据集（可重复给；缺省全部）",
)
@click.option(
    "--nav",
    default=None,
    help="投影用的 nav 数据（本地路径或 URL；缺省用子模块里的 map/navmesh/base.nav.gz）",
)
@click.option(
    "--force",
    is_flag=True,
    help="重新下载（请求带时间戳，不走远端缓存），并重写未变化的输出文件",
)
@click.option(
    "--download/--no-download",
    "with_download",
    default=True,
    help="先下载已发布数据（--no-download 时只用本地 data/raw/ 重新入库）",
)
@click.option("--raw-only", is_flag=True, help="只下载原始形态（只写 --out-dir），不做入库")
def fetch_command(
    out_dir: Path,
    data_dir: Path,
    only: tuple[str, ...],
    nav: str | None,
    force: bool,
    with_download: bool,
    raw_only: bool,
) -> None:
    """下载 zmdmap 已发布的原始数据，并入库到 data/。"""
    raw_dir = out_dir.expanduser().resolve()
    data_dir = data_dir.expanduser().resolve()
    datasets = select_datasets(only)
    pipeline_files = files_of(datasets, projected=True)
    plain_files = files_of(datasets, projected=False)

    if with_download:
        click.echo("── 下载 ──────────────────────────────────────────")
        files = (*pipeline_files, *plain_files)
        click.echo(f"   {len(files)} 份：{'、'.join(files)}")
        # 跟清单比的是「本地这份数据」：入库模式下比入库目录里那一份（它在仓库里，新克隆下来就有），
        # --raw-only 下产出的就是原始形态、比它自己。原始形态目录是 .gitignore 的本地缓存，空着不算旧。
        target_dir = raw_dir if raw_only else data_dir
        try:
            pending = set(download(raw_dir, target_dir, pipeline_files, plain_files, force=force))
        except (OSError, ValueError) as error:
            raise click.ClickException(str(error)) from error
    else:
        click.echo("── 下载：跳过（--no-download），直接用现有 data/raw/")
        pending = {*pipeline_files, *plain_files}

    if raw_only:
        click.echo("入库：未执行（--raw-only）")
        return

    if not pending:
        # 本地已经是清单里那一批，一份都不用下；原始形态可能根本不在（.gitignore 的缓存），
        # 拿它重投影反而会把入库数据改回旧内容，所以这一步整个跳过。
        click.echo("\n── 入库 ──────────────────────────────────────────")
        click.echo("   跳过：本地数据已经是清单里那一批（原始形态目录没有也不影响）")
        report_data_changes(data_dir, [dataset.file_name for dataset in datasets])
        return

    click.echo("\n── 入库（原始形态 -> data/）─────────────────────")
    projected = run_projection(
        raw_dir, data_dir, [file_name for file_name in pipeline_files if file_name in pending],
        nav=nav, force=force,
    )
    copied = copy_plain(
        raw_dir, data_dir, [file_name for file_name in plain_files if file_name in pending],
        force=force,
    )
    click.echo("\n── 汇总 ──────────────────────────────────────────")
    click.echo(f"入库：正常（投影 {projected} 份、复制 {copied} 份，输出到 {data_dir}）")
    report_data_changes(data_dir, [dataset.file_name for dataset in datasets])
