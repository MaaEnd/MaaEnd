"""把截图写进 MaaEnd 测试集：脱敏后放到 tests/MaaEndTestset/<控制器>/<资源>/<任务目录>/。

脱敏完全交给 .agents/skills/maaend-test-image 里的脚本：它按左下角 UID 文字判定平台，
用纯绿色盖住该平台的固定 ROI，再用 OCR 兜住其它 UID / # 文字。
本模块只负责挑路径、调用脚本，把结果交给 manifest；用例文件仍按人工习惯维护。
"""

from __future__ import annotations

import json
import re
import shutil
import subprocess
import uuid
from pathlib import Path

from .flow import as_dict, as_list

# 和 tests/scripts/loader.mts 的 controllerMap 保持一致。
CONTROLLER_DIRS = {
    "ADB": "ADB",
    "CloudADB": "CloudADB",
    "CloudWeb": "CloudWeb",
    "Win32": "Win32",
    "Win32-Front": "Win32",
}
# 和 loader.mts 的 resourceMap 保持一致：配置里写中文，目录用英文。
RESOURCE_DIRS = {"官服": "Official_CN"}
REDACT_SCRIPT = Path(".agents/skills/maaend-test-image/scripts/redact_test_image.py")
# 脱敏脚本发现 UID 文字没被固定 ROI 盖全时，会建议一个额外 ROI；照着补上再跑一遍。
ROI_SUGGESTION = re.compile(r"--extra-roi (\d+,\d+,\d+,\d+)")


class TestSetError(RuntimeError):
    """测试集相关操作失败，消息直接展示给用户。"""


def controller_dir(name: str) -> str:
    folder = CONTROLLER_DIRS.get(name)
    if folder is None:
        raise TestSetError(f"测试集只支持 ADB / CloudADB / Win32 / CloudWeb；控制器 {name} 的截图得手动处理")
    return folder


def resource_dir(name: str) -> str:
    folder = RESOURCE_DIRS.get(name)
    if folder is None:
        raise TestSetError(f"还没有 {name} 的资源服目录，可选：{'、'.join(RESOURCE_DIRS)}")
    return folder


class TestSetWriter:
    """按控制器把原图脱敏后放进任务自己的目录。"""

    def __init__(self, repo: Path, resource: str, folder: str) -> None:
        self.repo = repo
        self.resource = resource
        self.folder = folder
        self.current = ""

    def start_controller(self, name: str) -> None:
        """切到某个控制器：之后加进来的图片都写它自己的目录。"""
        self.current = controller_dir(name)

    def target_of(self, step_name: str) -> Path:
        testset = self.repo / "tests" / "MaaEndTestset"
        return testset / self.current / resource_dir(self.resource) / self.folder / f"{step_name}.png"

    def add(self, raw: Path, step_name: str) -> str:
        """把这一张原图脱敏后写进测试集的任务目录，返回相对仓库的路径。"""
        target = self.target_of(step_name)
        target.parent.mkdir(parents=True, exist_ok=True)
        # 脱敏脚本只认 tests/MaaEndTestset/<控制器>/<资源>/<文件>.png 这三层路径，
        # 所以先在资源目录根上生成，再挪进任务目录。
        staging = target.parent.parent / f".{target.stem}.redacting-{uuid.uuid4().hex}.png"
        try:
            self.redact(raw, staging)
            staging.replace(target)
        finally:
            if staging.exists():
                staging.unlink()
        return target.relative_to(self.repo).as_posix()

    @property
    def directory(self) -> str:
        """当前控制器的图片目录，相对仓库，用来提示用户。"""
        return f"tests/MaaEndTestset/{self.current}/{resource_dir(self.resource)}/{self.folder}"

    def redact(self, raw: Path, target: Path) -> None:
        extras: list[str] = []
        while True:
            done = self._call(raw, target, extras)
            if done.returncode == 0:
                return
            output = f"{done.stdout}\n{done.stderr}"
            suggested = ROI_SUGGESTION.search(output)
            # 背景太亮时 OCR 会把杂点当成 UID 文字，脚本据此要求补一个遮盖区域；
            # 照着补上重跑，最多补三次，补不动再报错。
            if suggested is None or len(extras) >= 3 or suggested.group(1) in extras:
                lines = [line for line in output.strip().splitlines() if line.strip()]
                reason = lines[-1] if lines else f"退出码 {done.returncode}"
                raise TestSetError(f"脱敏 {target.name} 失败：{reason}")
            extras.append(suggested.group(1))

    def _call(self, raw: Path, target: Path, extras: list[str]) -> subprocess.CompletedProcess[str]:
        uv = shutil.which("uv")
        if uv is None:
            raise TestSetError("找不到 uv，脱敏脚本依赖它安装 opencv 等包")
        script = self.repo / REDACT_SCRIPT
        if not script.exists():
            raise TestSetError(f"找不到脱敏脚本 {REDACT_SCRIPT}")
        command = [uv, "run", str(script), str(raw), str(target)]
        for roi in extras:
            command.extend(["--extra-roi", roi])
        if target.exists():
            command.append("--force")
        return subprocess.run(command, cwd=self.repo, capture_output=True, text=True, check=False)

def export_run(run_dir: Path, repo: Path, resource: str, folder: str) -> list[str]:
    """把一次运行目录里的截图重新脱敏导出到测试集，返回写进去的相对路径。

    运行目录的 manifest.json 记着每个控制器截了哪些图，所以忘了 --testset 也不用重跑。
    """
    if not folder:
        raise TestSetError("不知道要写进哪个任务目录，用 --testset-dir 指定")
    manifest_path = run_dir / "manifest.json"
    if not manifest_path.is_file():
        raise TestSetError(f"{run_dir} 里没有 manifest.json，不是 test-capture 的运行目录")
    manifest = as_dict(json.loads(manifest_path.read_text(encoding="utf-8")))
    writer = TestSetWriter(repo, resource, folder)
    written: list[str] = []
    for item in as_list(manifest.get("controllers")):
        controller = as_dict(item)
        name = str(controller.get("controller") or "")
        writer.start_controller(name)
        for step in as_list(controller.get("steps")):
            for shot in as_list(as_dict(step).get("shots")):
                data = as_dict(shot)
                image = str(data.get("image") or "")
                raw = run_dir / name / image
                if not image or not raw.is_file():
                    raise TestSetError(f"运行目录里缺截图：{raw}")
                written.append(writer.add(raw, str(data.get("name") or Path(image).stem)))
    if not written:
        raise TestSetError(f"{run_dir} 里一张截图都没有，不用导出")
    return written
