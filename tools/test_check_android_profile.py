import tempfile
import unittest
from pathlib import Path

from tools.check_android_profile import check_android_profile


class CheckAndroidProfileTest(unittest.TestCase):
    def _write(self, root: Path, rel: str, content: str = "{}") -> Path:
        path = root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")
        return path

    def test_valid_profile_passes_and_ignores_desktop_only_controllers_and_resources(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self._write(
                root,
                "assets/interface.json",
                """{
                    // JSONC 注释与尾随逗号
                    "interface_version": 2,
                    "languages": {
                        "zh_cn": "locales/zh_cn.json",
                    },
                    "controller": [
                        {
                            "name": "ADB",
                            "type": "Adb",
                            "attach_resource_path": ["./resource_adb"],
                        },
                        {
                            "name": "CloudADB",
                            "type": "Adb",
                            "attach_resource_path": ["./resource_adb", "./resource_cloud", "./resource_cloud_adb"],
                        },
                        {
                            "name": "CloudWeb",
                            "type": "Win32",
                            "attach_resource_path": ["./resource_cloud_web"],
                        },
                    ],
                    "resource": [
                        {
                            "name": "官服",
                            "path": ["./resource"],
                        },
                        {
                            "name": "桌面专用资源",
                            "controller": ["CloudWeb"],
                            "path": ["./resource_desktop_only"],
                        },
                    ],
                    "import": ["tasks/Main.json"],
                }""",
            )
            self._write(root, "assets/locales/zh_cn.json")
            self._write(root, "assets/tasks/Main.json")
            self._write(root, "assets/resource/pipeline/a.json")
            self._write(root, "assets/resource_adb/pipeline/a.json")
            self._write(root, "assets/resource_cloud/pipeline/a.json")
            self._write(root, "assets/resource_cloud_adb/image/a.png", "png")

            profile = self._write(
                root,
                "Android/profile.yaml",
                """
assets: ../assets
include:
  - interface.json
  - tasks/**
  - locales/**
  - resource/**
  - resource_adb/**
  - resource_cloud/**
  - resource_cloud_adb/**
""",
            )

            self.assertEqual(check_android_profile(profile), [])

    def test_missing_adb_attach_resource_path_in_profile_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self._write(
                root,
                "assets/interface.json",
                """{
                    "interface_version": 2,
                    "controller": [
                        {
                            "name": "CloudADB",
                            "type": "Adb",
                            "attach_resource_path": ["./resource_adb", "./resource_cloud_adb"]
                        }
                    ],
                    "resource": [{"name": "CN", "path": ["./resource"]}]
                }""",
            )
            self._write(root, "assets/resource/pipeline/a.json")
            self._write(root, "assets/resource_adb/pipeline/a.json")
            self._write(root, "assets/resource_cloud_adb/image/a.png", "png")

            profile = self._write(
                root,
                "Android/profile.yaml",
                """
assets: ../assets
include:
  - interface.json
  - resource/**
  - resource_adb/**
""",
            )

            problems = check_android_profile(profile)
            self.assertEqual(len(problems), 1)
            self.assertIn("CloudADB", problems[0])
            self.assertIn("resource_cloud_adb", problems[0])

    def test_excluded_directory_contents_fail(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self._write(
                root,
                "assets/interface.json",
                """{
                    "interface_version": 2,
                    "controller": [{"name": "ADB", "type": "Adb", "attach_resource_path": ["./resource_adb"]}],
                    "resource": [{"name": "CN", "path": ["./resource"]}]
                }""",
            )
            self._write(root, "assets/resource/pipeline/a.json")
            self._write(root, "assets/resource_adb/image/only.png", "png")

            profile = self._write(
                root,
                "Android/profile.yaml",
                """
assets: ../assets
include:
  - interface.json
  - resource/**
  - resource_adb/**
exclude:
  - resource_adb/**/*.png
""",
            )

            problems = check_android_profile(profile)
            self.assertEqual(len(problems), 1)
            self.assertIn("resource_adb", problems[0])


if __name__ == "__main__":
    unittest.main()
