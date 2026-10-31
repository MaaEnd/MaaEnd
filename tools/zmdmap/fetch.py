"""``zmdmap fetch``：从 zmdmap 下载已发布的原始数据，再入库。

上游发布的清单（``manifest.json``）给出这批数据是哪一版、每份原始形态的内容指纹，数据按
``?t=<清单给的内容指纹>`` 从发布 CDN 拉取。``t`` 是完整指纹，于是「同一份内容 = 同一个 URL、内容一变就是
新 URL」：这个 CDN 只看 URL 判断缓存，这样它就没机会拿旧内容顶新的；地址里不带版本号，版本只用来写日志和
标签。

清单是必需的前提：取不到（没发布、404、形状不对）就直接报错 —— 没有清单既不知道该下哪几份，也不知道本地
是不是已经最新，硬下只会让本地数据与批次对不上。下哪几份由调用方给出：各任务生成器只要自己用的那几份，就
只下那几份，不必被整包牵着走。清单里没有的文件也不会下：那是「这批没发它」，本地有就保持原样；本地也没有
的，需要投影的那几份直接报错，只需复制的提示一句跳过。

每份下回来都先核对内容再落盘：有清单指纹就按它算，清单里没有这一份就按文件自称的 ``sha256``。不符就报错、
不写盘 —— 免得坏内容配上好指纹进 ``data/raw/``，入库之后再也比不出来。这里不重试：URL 已经由完整指纹决定，
再取一次还是同一份，不符只能是上游发布的那份和清单不一致。``--force`` 时校验不过也照收，只提示。

``version`` 与 ``sha256`` 都沿用上游写的（它们是这份数据原始形态的标签与批次标识）；``sha256`` 在这里会按
内容核一遍，文件自称的值和内容对不上时写回实际算出来的指纹，于是字段始终与内容一致 —— 下次 ``fetch`` 拿它
跟清单比才靠得住。

本地已是最新时跳过下载：逐份拿本地文件里自称的 ``sha256`` 跟清单比，一样的跳过，不一样的才下。清单本身
每次都带时间戳取 —— 它只有几百字节，却决定了整批判断，被远端缓存住就会拿旧索引判断。无论下载与否都会跑
一次入库。上游还没发布的数据只提示、不当失败：本地可以用 ``uv run zmdmap generate --only <名字>`` 生成。
清单里的 ``version`` 是判断本地新旧的依据，必须有：没写就报错退出，不退回「只比指纹」。本地那份的 ``version``
比清单新时也不下 —— 上游这份是旧的，下回来只会把新内容换回旧内容，要覆盖得用 ``--force``。
``--dry-run`` 走到「这一批会下哪几份」为止就停：清单照读、照样分类，但不下载、不写文件。

发布的都是**原始形态**（只带游戏世界坐标），一律先落不入库的 ``data/raw/``，再由
:mod:`tools.zmdmap.materialize` 材料化进入库的 ``data/``：需要投影的补上底图像素，不需要投影的原样复制。

下载逐个进行：一份取完再取下一份。每份只有几百 KB，串行的耗时可以接受，判断与报错也简单得多。
"""

import json
import time
from collections.abc import Sequence
from pathlib import Path
from typing import Any
from urllib.parse import urlencode

import click
import msgspec
import requests

from .datasets import DATASET_NAMES, files_of, select_datasets
from .materialize import copy_plain, report_data_changes, run_projection
from .utils.config import DATA_BASE_URL, DEFAULT_OUT_DIR, MANIFEST_FILE, ZMDMAP_DATA_DIR
from .utils.datafile import (
    HASH_KEY,
    dump_json,
    payload_hash,
    payload_of,
    read_hash,
    read_version,
    version_of,
)
from .utils.http import get
from .utils.log import logger
from .utils.shapes import is_object
from .utils.version import version_order


class DownloadError(ValueError):
    """一份数据下载失败；``status_code`` 为 ``None`` 表示网络层错误。"""

    def __init__(self, file_name: str, message: str, *, status_code: int | None = None) -> None:
        super().__init__(f"下载 {file_name} 失败: {message}")
        self.file_name = file_name
        self.status_code = status_code


class ManifestError(ValueError):
    """取不到发布清单，或者清单的形状不对。"""


class Manifest(msgspec.Struct):
    """上游发布的清单：这批数据是哪一版，以及每份原始形态的内容指纹。"""

    version: str | None = None
    """这批数据的版本标签，如 ``1.6.4+11262943-8``；:func:`manifest_plan` 拿它跟本地那份的 ``version`` 比
    新旧。清单必须写它，没写的清单 :func:`download` 直接报错，不下 —— 判断不了本地是不是更新。"""
    files: dict[str, str] = msgspec.field(default_factory=dict[str, str])
    """文件名 -> 原始形态的内容指纹"""


def read_manifest() -> Manifest:
    """取上游清单；取不到就抛 :class:`ManifestError` —— 没有清单这一批下不了。

    清单决定了「后面下哪几份、内容是不是已经最新」，被远端缓存住就等于拿旧索引判断，所以每次带一个时间戳
    当 URL 的一部分。实测这个 CDN 只看 URL：请求头带 ``Cache-Control: no-cache`` 照样命中缓存（``Age``
    不变），换 URL 才真的回源。只有它需要这样 —— 数据文件的 URL 自带清单给的完整指纹（``t=<内容指纹>``），
    本来就随内容变。
    """
    # 清单只有几百字节，每次都回源不心疼；它错了会连带整批判断都错。
    token = urlencode({"t": int(time.time() * 1000)})
    url = f"{DATA_BASE_URL}/{MANIFEST_FILE}?{token}"
    try:
        response = get(url)
    except requests.RequestException as error:
        # 网络层错误与 5xx 重试用完都落到这里：这一批没有清单可用。
        raise ManifestError(f"取清单失败（{url}）：{error}") from error
    if response.status_code == 404:
        raise ManifestError(
            f"上游还没有发布 {MANIFEST_FILE}（{url} 返回 404）：先确认数据发完再 fetch"
        )
    if response.status_code >= 400:
        raise ManifestError(f"取清单失败（{url}）：{response.status_code} {response.reason}")
    try:
        return msgspec.json.decode(response.text, type=Manifest)
    except msgspec.DecodeError as error:
        raise ManifestError(
            f"{MANIFEST_FILE} 不是预期形状（{url}）：{error}；可能是错误页或上游换了格式"
        ) from error


def manifest_plan(
    target_dir: Path, file_names: Sequence[str], manifest: Manifest
) -> tuple[list[str], list[str], list[str], list[str]]:
    """按清单把这批数据分成四类，顺序同返回值：

    - **要下载的**：本地没有这个文件，或者本地自称的 ``sha256`` 与清单记的指纹不同。调用方下载后入库。
    - **清单里没有但本地有的**：这批没发它（``generate --only`` 只发点名的几份），保持本地原样 —— 下回来的是
      哪一批说不清，也没有指纹可核对（只能信文件自称的那个）。调用方提示一句即可。
    - **清单里没有且本地也没有的**：同样不下载。需要投影的由调用方直接报错（缺这份没法继续），只需复制的
      提示一句跳过。
    - **本地比清单新的**：本地自称的 ``version`` 比清单的 ``version`` 新
      （:func:`tools.zmdmap.utils.version.version_order`），说明上游这份是旧的 —— 发布端回滚过，或者本地已经
      按当前代码生成过、上游还没重新发布。保持本地原样，要覆盖得用 ``--force``。

    判断只读 ``target_dir`` 里同名文件自称的 ``version`` 与 ``sha256``
    （:func:`tools.zmdmap.utils.datafile.read_version`、:func:`tools.zmdmap.utils.datafile.read_hash`），
    不读本体。先看版本：本地更新就不下；版本相同或更旧时看指纹：不同才下。所以同一个版本号下重新发布过、
    内容变了照样重下，只换了版本号、内容没变则不会重下。

    指纹与清单一致的那几份四类都不进：本地就是这一批，不用动。

    清单的 ``version`` 是必填项：:func:`download` 会先确认它存在，所以这里不必再判空 —— 缺版本时根本走不到
    这一步（判断不了新旧，不盲下）。

    ``target_dir`` 是本地这份数据所在的地方：入库模式下是 ``data/``（在仓库里，新克隆下来也有），
    ``--raw-only`` 下是原始形态目录自己（那正是它的产物）。原始形态目录是 ``.gitignore`` 的本地缓存，
    空着不代表本地数据旧，所以默认不拿它当参照。
    """
    todo: list[str] = []
    unlisted: list[str] = []
    absent: list[str] = []
    newer: list[str] = []
    for file_name in file_names:
        local = read_hash(target_dir / file_name)
        expected = manifest.files.get(file_name)
        if expected is None:  # 这批没发它：本地有就原样留着，没有也不下
            (unlisted if local is not None else absent).append(file_name)
            continue
        local_version = read_version(target_dir / file_name)
        if local_version and manifest.version and version_order(local_version) > version_order(manifest.version):
            newer.append(file_name)  # 上游这份是旧的：不拿它覆盖本地更新的数据
        elif local != expected:  # 指纹对不上，或者本地根本没有这一份：要下
            todo.append(file_name)
    return todo, unlisted, absent, newer


def _data_url(file_name: str, token: str) -> str:
    """数据文件的下载地址：查询串只有 ``t=<内容指纹>``，没有版本号（CDN 不认它）。"""
    return f"{DATA_BASE_URL}/{file_name}" + (f"?{urlencode({'t': token})}" if token else "")


def _request_json(file_name: str, url: str) -> Any:
    """取一份数据并解析成 JSON；网络故障、非 2xx、非法 JSON 一律换成 :class:`DownloadError`。"""
    logger.info(f"下载 {url}")
    try:
        response = get(url)
    except requests.RequestException as error:
        # 网络层错误与 5xx 重试用完都落到这里：type 不同，但对调用方都是「这一份没取到」。
        raise DownloadError(file_name, str(error)) from error
    if response.status_code >= 400:
        raise DownloadError(
            file_name,
            f"{response.status_code} {response.reason}",
            status_code=response.status_code,
        )
    try:
        return json.loads(response.text)
    except json.JSONDecodeError as error:
        raise DownloadError(file_name, f"不是合法 JSON：{error}") from error


def content_problem(file_name: str, data: Any, digest: str, expect: str | None) -> str | None:
    """内容与预期不符时的说明；一致或无从核对时返回 ``None``。

    有清单给的指纹就以它为准 —— 那是发布端按内容算的。清单里没有这一份（这批没发它）时退回文件自称的
    ``sha256``；两个都没有就无从核对，只能照收（上游的老形态）。
    """
    if expect is not None:
        return None if digest == expect else f"与清单不符：清单 {expect}，拿到 {digest}"
    claimed = data.get(HASH_KEY) if is_object(data) else None
    if not claimed:
        logger.warning(f"{file_name} 没带 sha256 字段，无从核对内容，照收")
        return None
    return None if claimed == digest else f"自称的 sha256 与内容不符：{claimed} != {digest}"


def _fetch_verified(
    file_name: str,
    token: str,
    expect: str | None,
    *,
    accept_mismatch: bool,
) -> tuple[Any, str]:
    """取一份数据并核对内容，返回 ``(解析结果, 内容指纹)``；不符就抛 :class:`DownloadError`，不写盘。

    不重试：``t`` 用的是清单给的**完整**指纹，同一份内容永远同一个 URL、内容一变就是新 URL，远端缓存没法拿
    旧的顶替。所以内容还对不上，只能是上游发布的那份和清单不一致 —— 再取一次还是同一份，重试只会白等。
    ``accept_mismatch`` 为真（``--force``）时只提示、照收。
    """
    data = _request_json(file_name, _data_url(file_name, token))
    digest = payload_hash(payload_of(data))
    problem = content_problem(file_name, data, digest, expect)
    if problem is None:
        return data, digest
    if accept_mismatch:
        logger.warning(f"{file_name} {problem}；--force 下照收这份内容")
        return data, digest
    raise DownloadError(
        file_name,
        f"{problem}（URL 里的 t 就是清单给的完整指纹，还取到别的内容说明上游发布有问题；"
        "确认没问题可以加 --force 照收）",
    )


def _download_json(
    file_name: str,
    destination: Path,
    token: str,
    expect: str | None = None,
    *,
    accept_mismatch: bool = False,
) -> None:
    """下载一份原始数据，核对内容之后再落盘。

    ``token`` 是查询串里的 ``t``（清单指纹，``--force`` 时是时间戳）；``expect`` 是清单给的指纹；
    ``accept_mismatch``（``--force``）时校验不过也照收，只提示。
    """
    data, digest = _fetch_verified(file_name, token, expect, accept_mismatch=accept_mismatch)

    # 元数据沿用上游 generate 写的那份 ``version``。``sha256`` 换成这一步核出来的指纹：清单核过时它就是清单
    # 的值，``--force`` 照收时它是实际内容的值 —— 两种情况下文件里的字段都与内容一致。若照抄上游自称的那个
    # （可能与内容不符），入库后 ``data/`` 会带着一个比不出来的字段，坏内容再也发现不了。
    if isinstance(data, dict):
        if expect is not None:
            data[HASH_KEY] = digest
        if not version_of(data):
            logger.warning(
                f"{file_name} 没带 version 字段（上游发布时就没写）：fetch 只照抄，不自己编版本"
            )
    formatted = dump_json(data)
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(formatted, encoding="utf-8", newline="\n")
    size_kb = len(formatted.encode("utf-8")) / 1024
    logger.info(f"已缓存 {destination} ({size_kb:.0f} KB)")


def fetch_and_cache(
    raw_dir: Path,
    projected_files: Sequence[str],
    plain_files: Sequence[str],
    *,
    token: str | None = None,
    hashes: dict[str, str],
    force: bool = False,
) -> list[str]:
    """把这几份发布的原始数据逐个下载到 ``raw_dir``；返回上游还没发布、被跳过的文件名。

    ``hashes`` 是清单给的每份内容指纹，**原样**当 ``t`` 参数：同一份内容的 URL 稳定、内容一变就是新 URL，
    内容寻址，远端缓存不可能拿旧的顶替。走到这里的文件都在清单里（不在清单里的不会被下载），所以一定有指纹。
    ``token`` 给了就整批替掉（``--force`` 时是时间戳），让这几份一律回源。``force`` 时校验不过也照收，只提示。
    """
    raw_dir.mkdir(parents=True, exist_ok=True)
    failures: dict[str, DownloadError] = {}
    for file_name in (*projected_files, *plain_files):
        try:
            _download_json(
                file_name,
                raw_dir / file_name,
                token or hashes.get(file_name, ""),
                hashes.get(file_name),
                accept_mismatch=force,
            )
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
        logger.warning(f"上游还没发布 {file_name}，跳过（本地可用 uv run zmdmap generate 生成）")
        missing.append(file_name)

    return missing


def download(
    raw_dir: Path,
    target_dir: Path,
    projected_files: Sequence[str],
    plain_files: Sequence[str],
    *,
    force: bool = False,
    dry_run: bool = False,
) -> list[str]:
    """按需把这几份数据下载到 ``raw_dir``；返回这次**真的下过**的文件名（也就是还要入库的那几份）。

    下不下由 :func:`manifest_plan` 分类决定，不必额外请求任何接口：

    - 清单没写 ``version``：直接抛 :class:`ManifestError`，不下 —— 没有版本就判断不了本地这份比上游新还是
      旧，按指纹硬下可能把本地的新内容换成旧内容；
    - 清单里没有这一份：保持本地原样；本地也没有的，需要投影的直接报错、只需复制的提示一句跳过；
    - 本地版本比清单新：保持本地原样，提示一句可以用 ``--force`` 覆盖；
    - 本地指纹与清单不同：下载；
    - 本地指纹与清单一致：跳过。

    所以本地已经是最新时，这里只读一次清单就结束，不下载、不写任何文件。

    清单是必需的前提：取不到就直接抛 :class:`ManifestError`，不会退化成「不比对就整批下」。没有清单既不知道
    该下哪几份，也不知道本地是不是已经最新，硬下只会把本地数据换成对不上批次的内容。

    ``force`` 是不看版本与指纹、直接重下这几份，并且请求 URL 上的 ``t`` 用时间戳而不是清单指纹 ——
    「怀疑远端说辞」时的唯一手段（这个 CDN 只看 URL，请求头说了不算）。CI 上没有额外开关：本地与 CI 跑的是
    同一套判断。

    ``dry_run`` 只把「这一批会下哪几份」算出来（照样读清单、照样分类），不下文件也不写盘。
    """
    todo = [*projected_files, *plain_files]
    token = str(int(time.time() * 1000)) if force else None

    manifest = read_manifest()
    version = manifest.version
    if force:
        logger.info("强制下载（--force），不比对本地内容（版本与指纹仍按清单）")
    else:
        if not version:
            raise ManifestError(
                f"{MANIFEST_FILE} 里没有 version：判断不了本地这份比上游新还是旧，不下。"
                "等上游按发布流程重新生成清单；确认要覆盖本地就加 --force 重下"
            )
        todo, unlisted, absent, newer = manifest_plan(target_dir, todo, manifest)
        label = f"清单 v{version}"
        if unlisted:
            logger.warning(f"这几份不在{label}里，保持本地原样：{'、'.join(unlisted)}")
        if absent:
            required = [file_name for file_name in absent if file_name in set(projected_files)]
            if required:
                raise DownloadError(
                    required[0],
                    f"{label}里没有这一份，本地也没有：上游这批没发它，等发完再 fetch；"
                    "本地也可以先跑 uv run zmdmap generate 生成",
                )
            for file_name in absent:
                logger.warning(f"{file_name}：{label}里没有它、本地也没有，跳过（上游还没发这一份）")
        if newer:
            logger.warning(f"本地版本比{label}新，保持本地原样（要覆盖用 --force 重下）：{'、'.join(newer)}")
        if todo:
            logger.info(f"{label}：{len(todo)} 份内容有变化")
        else:
            if absent or unlisted or newer:
                logger.info(f"{label}：没有要下载的（其余几份按上面的提示处理）")
            else:
                logger.info(f"本地内容与{label}一致，跳过下载")
            return []

    hashes = manifest.files
    written = list(todo)
    if todo:
        if dry_run:
            logger.info(f"--dry-run：这一批会下载 {len(todo)} 份")
        else:
            pending = set(todo)
            missing = fetch_and_cache(
                raw_dir,
                [file_name for file_name in projected_files if file_name in pending],
                [file_name for file_name in plain_files if file_name in pending],
                token=token,
                hashes=hashes,
                force=force,
            )
            if missing:
                logger.warning(f"上游还没发布：{', '.join(missing)}")
                unpublished = set(missing)
                written = [file_name for file_name in todo if file_name not in unpublished]
    if not dry_run:
        logger.info(f"完成，当前版本: v{version}" if version else "完成")
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
@click.option("--dry-run", is_flag=True, help="只报告会下载、入库哪几份，不下载也不写文件")
def fetch_command(
    out_dir: Path,
    data_dir: Path,
    only: tuple[str, ...],
    nav: str | None,
    force: bool,
    with_download: bool,
    raw_only: bool,
    dry_run: bool,
) -> None:
    """下载 zmdmap 已发布的原始数据，并入库到 data/。"""
    raw_dir = out_dir.expanduser().resolve()
    data_dir = data_dir.expanduser().resolve()
    datasets = select_datasets(only)
    pipeline_files = files_of(datasets, projected=True)
    plain_files = files_of(datasets, projected=False)

    if with_download:
        logger.section("下载")
        files = (*pipeline_files, *plain_files)
        logger.info(f"{len(files)} 份：{'、'.join(files)}")
        # 跟清单比的是「本地这份数据」：入库模式下比入库目录里那一份（它在仓库里，新克隆下来就有），
        # --raw-only 下产出的就是原始形态、比它自己。原始形态目录是 .gitignore 的本地缓存，空着不算旧。
        target_dir = raw_dir if raw_only else data_dir
        try:
            pending = set(
                download(
                    raw_dir,
                    target_dir,
                    pipeline_files,
                    plain_files,
                    force=force,
                    dry_run=dry_run,
                )
            )
        except (OSError, ValueError) as error:
            raise click.ClickException(str(error)) from error
    else:
        logger.info("下载：跳过（--no-download），直接用现有 data/raw/")
        pending = {*pipeline_files, *plain_files}

    if dry_run:
        logger.section("将执行（--dry-run，未执行）")
        logger.info(f"原始数据目录：{raw_dir}")
        logger.info(f"入库数据目录：{data_dir}")
        if raw_only:
            logger.info("入库    ：未启用（--raw-only）")
        else:
            projected = [file_name for file_name in pipeline_files if file_name in pending]
            plain = [file_name for file_name in plain_files if file_name in pending]
            logger.info(f"投影    ：{len(projected)} 份 {raw_dir} -> {data_dir}")
            logger.info(f"复制    ：{len(plain)} 份 {raw_dir} -> {data_dir}")
        logger.info("--dry-run：没有下载，也没有写任何数据文件")
        return

    if raw_only:
        logger.info("入库：未执行（--raw-only）")
        return

    if not pending:
        # 本地已经是清单里那一批，一份都不用下；原始形态可能根本不在（.gitignore 的缓存），
        # 拿它重投影反而会把入库数据改回旧内容，所以这一步整个跳过。
        logger.section("入库")
        logger.info("跳过：本地数据已经是清单里那一批（原始形态目录没有也不影响）")
        report_data_changes(data_dir, [dataset.file_name for dataset in datasets])
        return

    logger.section("入库（原始形态 -> data/）")
    projected = run_projection(
        raw_dir, data_dir, [file_name for file_name in pipeline_files if file_name in pending],
        nav=nav, force=force,
    )
    copied = copy_plain(
        raw_dir, data_dir, [file_name for file_name in plain_files if file_name in pending],
        force=force,
    )
    logger.section("汇总")
    logger.info(f"入库：正常（投影 {projected} 份、复制 {copied} 份，输出到 {data_dir}）")
    report_data_changes(data_dir, [dataset.file_name for dataset in datasets])
