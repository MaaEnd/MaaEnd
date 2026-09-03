from __future__ import annotations

import contextlib
import io
import os
import unittest
from unittest.mock import patch

import main


class MainTest(unittest.TestCase):
    def test_help_exits_without_starting_server(self) -> None:
        stdout = io.StringIO()
        with (
            patch.object(main.runpy, "run_path") as run_path,
            contextlib.redirect_stdout(stdout),
            self.assertRaises(SystemExit) as raised,
        ):
            main.main(["--help"])

        self.assertEqual(raised.exception.code, 0)
        self.assertIn("--port PORT", stdout.getvalue())
        self.assertIn("--no-browser", stdout.getvalue())
        run_path.assert_not_called()

    def test_cli_options_override_environment(self) -> None:
        with (
            patch.dict(
                os.environ,
                {"MAPNAV_PORT": "8770"},
                clear=False,
            ),
            patch.object(main.runpy, "run_path") as run_path,
        ):
            os.environ.pop("MAPNAV_NO_BROWSER", None)
            main.main(["--port", "9000", "--no-browser"])

            self.assertEqual(os.environ["MAPNAV_PORT"], "9000")
            self.assertEqual(os.environ["MAPNAV_NO_BROWSER"], "1")

        run_path.assert_called_once_with(str(main.SERVE_PY), run_name="__main__")

    def test_omitted_options_preserve_environment(self) -> None:
        with (
            patch.dict(
                os.environ,
                {
                    "MAPNAV_PORT": "9010",
                    "MAPNAV_NO_BROWSER": "1",
                },
                clear=False,
            ),
            patch.object(main.runpy, "run_path") as run_path,
        ):
            main.main([])

            self.assertEqual(os.environ["MAPNAV_PORT"], "9010")
            self.assertEqual(os.environ["MAPNAV_NO_BROWSER"], "1")

        run_path.assert_called_once_with(str(main.SERVE_PY), run_name="__main__")

    def test_rejects_out_of_range_port_without_starting_server(self) -> None:
        stderr = io.StringIO()
        with (
            patch.object(main.runpy, "run_path") as run_path,
            contextlib.redirect_stderr(stderr),
            self.assertRaises(SystemExit) as raised,
        ):
            main.main(["--port", "65536"])

        self.assertEqual(raised.exception.code, 2)
        self.assertIn("端口必须在 1 到 65535 之间", stderr.getvalue())
        run_path.assert_not_called()


if __name__ == "__main__":
    unittest.main()
