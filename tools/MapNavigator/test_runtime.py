from __future__ import annotations

import ctypes
import unittest
from types import SimpleNamespace
from unittest.mock import Mock, patch

import runtime


class MaaRuntimeLoadingTest(unittest.TestCase):
    @unittest.skipUnless(hasattr(ctypes, "RTLD_GLOBAL"), "requires POSIX dynamic loading")
    def test_linux_exports_framework_symbols_globally(self) -> None:
        modules = {
            "maa.agent_client": SimpleNamespace(AgentClient=Mock()),
            "maa.controller": SimpleNamespace(
                Win32Controller=Mock(), AdbController=Mock(), PlayCoverController=Mock(), LinuxController=Mock()
            ),
            "maa.library": SimpleNamespace(Library=Mock()),
            "maa.resource": SimpleNamespace(Resource=Mock()),
            "maa.tasker": SimpleNamespace(Tasker=Mock()),
            "maa.toolkit": SimpleNamespace(Toolkit=Mock()),
        }
        for platform in ("linux", "win32", "darwin"):
            with (
                self.subTest(platform=platform),
                patch.dict("sys.modules", modules),
                patch.object(runtime.sys, "platform", platform),
                patch.object(runtime, "MAAFW_BIN_DIR") as binary_dir,
                patch.object(runtime.ctypes, "CDLL") as load_library,
            ):
                library_path = binary_dir.__truediv__.return_value
                library_path.exists.return_value = True
                self.assertIsNotNone(runtime.load_maa_runtime())
                if platform == "linux":
                    load_library.assert_called_once_with(str(library_path), mode=ctypes.RTLD_GLOBAL)
                else:
                    load_library.assert_not_called()


if __name__ == "__main__":
    unittest.main()
