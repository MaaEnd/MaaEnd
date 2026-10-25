import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from tools.setup import setup_workspace


class CopyCppAlgoBinaryTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / "artifact"
        self.source.mkdir()
        self.binary = self.source / "cpp-algo.exe"
        self.binary.write_bytes(b"new-agent")
        self.install = self.root / "install"
        self.agent = self.install / "agent"
        self.agent.mkdir(parents=True)
        (self.agent / "cpp-algo.exe").write_bytes(b"old-agent")
        self.enterContext(patch.object(setup_workspace, "CPP_ALGO_DIST_NAME", "cpp-algo.exe"))
        self.enterContext(
            patch.object(
                setup_workspace,
                "CPP_ALGO_COMPANION_FILES",
                ("WebView2Loader.dll", "MaaUtils.dll"),
            )
        )
        self.enterContext(patch("builtins.print"))

    def test_legacy_artifact_with_loader(self):
        (self.source / "WebView2Loader.dll").write_bytes(b"loader")
        setup_workspace.copy_cpp_algo_binary(self.binary, self.install)
        self.assertEqual((self.agent / "cpp-algo.exe").read_bytes(), b"new-agent")
        self.assertEqual((self.agent / "WebView2Loader.dll").read_bytes(), b"loader")

    def test_legacy_artifact_without_companions(self):
        setup_workspace.copy_cpp_algo_binary(self.binary, self.install)
        self.assertEqual((self.agent / "cpp-algo.exe").read_bytes(), b"new-agent")

    def test_missing_source_rolls_back_runtime(self):
        with self.assertRaises(FileNotFoundError):
            setup_workspace._replace_cpp_runtime(
                [
                    (self.binary, self.agent / "cpp-algo.exe"),
                    (self.source / "missing.dll", self.agent / "missing.dll"),
                ]
            )
        self.assertEqual((self.agent / "cpp-algo.exe").read_bytes(), b"old-agent")
        self.assertFalse((self.agent / "missing.dll").exists())

    def test_complete_cloud_artifact(self):
        for name in setup_workspace.CPP_ALGO_COMPANION_FILES:
            (self.source / name).write_bytes(name.encode())
        setup_workspace.copy_cpp_algo_binary(self.binary, self.install)
        self.assertEqual((self.agent / "cpp-algo.exe").read_bytes(), b"new-agent")
        for name in setup_workspace.CPP_ALGO_COMPANION_FILES:
            self.assertEqual((self.agent / name).read_bytes(), name.encode())

    def test_busy_runtime_rolls_back_agent_and_loader(self):
        for name in setup_workspace.CPP_ALGO_COMPANION_FILES:
            (self.source / name).write_bytes(b"new-runtime")
            (self.agent / name).write_bytes(b"old-runtime")
        replace = setup_workspace._replace_file_with_retry

        def fail_on_utils(source, target):
            if target.name == "MaaUtils.dll":
                raise PermissionError("user declined replacement of the running runtime")
            replace(source, target)

        with (
            patch.object(setup_workspace, "_replace_file_with_retry", side_effect=fail_on_utils),
            self.assertRaises(PermissionError),
        ):
            setup_workspace.copy_cpp_algo_binary(self.binary, self.install)
        self.assertEqual((self.agent / "cpp-algo.exe").read_bytes(), b"old-agent")
        for name in setup_workspace.CPP_ALGO_COMPANION_FILES:
            self.assertEqual((self.agent / name).read_bytes(), b"old-runtime")
        self.assertFalse(list(self.agent.glob(".cpp-runtime-*")))

    def test_failed_first_install_removes_new_companions(self):
        for name in setup_workspace.CPP_ALGO_COMPANION_FILES:
            (self.source / name).write_bytes(b"new-runtime")
        replace = setup_workspace._replace_file_with_retry

        def fail_on_utils(source, target):
            if target.name == "MaaUtils.dll":
                raise PermissionError("runtime in use")
            replace(source, target)

        with (
            patch.object(setup_workspace, "_replace_file_with_retry", side_effect=fail_on_utils),
            self.assertRaises(PermissionError),
        ):
            setup_workspace.copy_cpp_algo_binary(self.binary, self.install)
        self.assertEqual((self.agent / "cpp-algo.exe").read_bytes(), b"old-agent")
        for name in setup_workspace.CPP_ALGO_COMPANION_FILES:
            self.assertFalse((self.agent / name).exists())


if __name__ == "__main__":
    unittest.main()
