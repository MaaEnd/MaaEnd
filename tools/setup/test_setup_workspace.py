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
                ("WebView2Loader.dll", "cloud-webview.exe", "MaaUtils.dll"),
            )
        )
        self.enterContext(patch("builtins.print"))

    def test_legacy_artifact_without_cloud_host(self):
        (self.source / "WebView2Loader.dll").write_bytes(b"loader")
        setup_workspace.copy_cpp_algo_binary(self.binary, self.install)
        self.assertEqual((self.agent / "cpp-algo.exe").read_bytes(), b"new-agent")
        self.assertEqual((self.agent / "WebView2Loader.dll").read_bytes(), b"loader")
        self.assertFalse((self.agent / "cloud-webview.exe").exists())

    def test_legacy_artifact_without_companions(self):
        setup_workspace.copy_cpp_algo_binary(self.binary, self.install)
        self.assertEqual((self.agent / "cpp-algo.exe").read_bytes(), b"new-agent")

    def test_incomplete_cloud_artifact_preserves_install(self):
        (self.source / "cloud-webview.exe").write_bytes(b"host")
        (self.source / "WebView2Loader.dll").write_bytes(b"loader")
        with self.assertRaises(FileNotFoundError):
            setup_workspace.copy_cpp_algo_binary(self.binary, self.install)
        self.assertEqual((self.agent / "cpp-algo.exe").read_bytes(), b"old-agent")
        self.assertFalse((self.agent / "cloud-webview.exe").exists())

    def test_complete_cloud_artifact(self):
        for name in setup_workspace.CPP_ALGO_COMPANION_FILES:
            (self.source / name).write_bytes(name.encode())
        setup_workspace.copy_cpp_algo_binary(self.binary, self.install)
        self.assertEqual((self.agent / "cpp-algo.exe").read_bytes(), b"new-agent")
        for name in setup_workspace.CPP_ALGO_COMPANION_FILES:
            self.assertEqual((self.agent / name).read_bytes(), name.encode())


if __name__ == "__main__":
    unittest.main()
