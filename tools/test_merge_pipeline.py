"""Regression tests for destructive pipeline merging on linked resources."""

import contextlib
import io
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from merge_pipeline import is_directory_link, merge_pipeline


SCRIPT = Path(__file__).with_name("merge_pipeline.py")


class MergePipelineTests(unittest.TestCase):
    def setUp(self):
        self.temp_dir = tempfile.TemporaryDirectory(prefix="maaend-merge-test-")
        self.addCleanup(self.temp_dir.cleanup)
        self.root = Path(self.temp_dir.name)
        self.source = self.root / "assets" / "resource"
        self.input_file = self.source / "pipeline" / "group" / "task.json"
        self.input_file.parent.mkdir(parents=True)
        self.input_file.write_text('{"FixtureTask": {"recognition": "DirectHit"}}', encoding="utf-8")
        self.install = self.root / "install"
        self.install.mkdir()

    def directory_link(self, link, target):
        link.parent.mkdir(parents=True, exist_ok=True)
        if os.name == "nt":
            result = subprocess.run(
                ["cmd", "/c", "mklink", "/J", str(link), str(target)],
                capture_output=True,
                text=True,
            )
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        else:
            link.symlink_to(target, target_is_directory=True)

    def symbolic_link(self, link, target, directory=False):
        try:
            link.symlink_to(target, target_is_directory=directory)
        except OSError as error:
            if os.name == "nt" and error.winerror == 1314:
                self.skipTest("Creating symbolic links requires Windows Developer Mode or administrator privileges")
            raise

    def assert_rejected(self, resource, protected_files=None):
        protected_files = list(protected_files or [self.input_file])
        local_input = resource / "pipeline" / "group" / "task.json"
        if local_input.is_file():
            protected_files.append(local_input)
        output = resource / "pipeline" / "nodes.json"
        output_existed = output.exists() or output.is_symlink()
        before = {path: path.read_bytes() for path in protected_files}
        with contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaisesRegex(RuntimeError, "链接"):
                merge_pipeline(resource)
        self.assertEqual({path: path.read_bytes() for path in protected_files}, before)
        self.assertEqual(output.exists() or output.is_symlink(), output_existed)
        self.assertFalse((self.source / "pipeline" / "nodes.json").exists())

    def run_cli(self, install):
        return subprocess.run(
            [sys.executable, str(SCRIPT), str(install)],
            capture_output=True,
            text=True,
            encoding="utf-8",
            env={**os.environ, "PYTHONIOENCODING": "utf-8"},
        )

    def test_copied_resource_merges_json5_without_changing_source(self):
        resource = self.install / "resource"
        shutil.copytree(self.source, resource)
        (resource / "pipeline" / "other.json").write_text(
            '// comment\n{"OtherTask": {"action": "DoNothing",},}', encoding="utf-8"
        )
        original = self.input_file.read_bytes()
        with contextlib.redirect_stdout(io.StringIO()):
            merge_pipeline(resource)
        pipeline = resource / "pipeline"
        self.assertEqual([path.name for path in pipeline.iterdir()], ["nodes.json"])
        self.assertEqual(
            json.loads((pipeline / "nodes.json").read_text(encoding="utf-8")),
            {"FixtureTask": {"recognition": "DirectHit"}, "OtherTask": {"action": "DoNothing"}},
        )
        self.assertEqual(self.input_file.read_bytes(), original)

    def test_resource_directory_link_is_rejected(self):
        resource = self.install / "resource"
        self.directory_link(resource, self.source)
        self.assert_rejected(resource)

    def test_pipeline_directory_link_is_rejected(self):
        resource = self.install / "resource"
        resource.mkdir()
        self.directory_link(resource / "pipeline", self.source / "pipeline")
        self.assert_rejected(resource)

    def test_linked_install_ancestor_is_rejected_before_resolving(self):
        linked_parent = self.root / "linked-parent"
        self.directory_link(linked_parent, self.root / "assets")
        self.assert_rejected(linked_parent / "resource")

    def test_linked_pipeline_subdirectory_is_rejected(self):
        resource = self.install / "resource"
        (resource / "pipeline").mkdir(parents=True)
        self.directory_link(resource / "pipeline" / "external", self.input_file.parent)
        self.assert_rejected(resource)

    def test_existing_output_file_link_is_rejected(self):
        resource = self.install / "resource"
        shutil.copytree(self.source, resource)
        external = self.root / "external.json"
        external.write_text('{"Keep": {}}', encoding="utf-8")
        self.symbolic_link(resource / "pipeline" / "nodes.json", external)
        self.assert_rejected(resource, [self.input_file, external, resource / "pipeline" / "group" / "task.json"])

    def test_broken_file_link_is_rejected(self):
        resource = self.install / "resource"
        shutil.copytree(self.source, resource)
        self.symbolic_link(resource / "pipeline" / "missing.json", self.root / "missing.json")
        self.assert_rejected(resource)

    def test_broken_directory_link_is_rejected(self):
        resource = self.install / "resource"
        shutil.copytree(self.source, resource)
        self.symbolic_link(resource / "pipeline" / "missing", self.root / "missing", directory=True)
        self.assert_rejected(resource)

    def test_symbolic_resource_directory_link_is_rejected(self):
        resource = self.install / "resource"
        self.symbolic_link(resource, self.source, directory=True)
        self.assert_rejected(resource)

    def test_file_link_guard_is_checked_before_output_write(self):
        resource = self.install / "resource"
        shutil.copytree(self.source, resource)
        output = resource / "pipeline" / "nodes.json"
        output.write_text('{"Keep": {}}', encoding="utf-8")
        # Exercise the guard even on Windows accounts unable to create file symlinks.
        with patch(
            "merge_pipeline.is_directory_link", side_effect=lambda path: path == output or is_directory_link(path)
        ):
            self.assert_rejected(resource, [self.input_file, output])

    def test_broken_link_guard_is_checked_before_directory_skip(self):
        resource = self.install / "resource"
        resource.mkdir()
        pipeline = resource / "pipeline"
        with patch(
            "merge_pipeline.is_directory_link", side_effect=lambda path: path == pipeline or is_directory_link(path)
        ):
            self.assert_rejected(resource)

    def test_directory_link_cycle_is_rejected_without_recursing(self):
        resource = self.install / "resource"
        shutil.copytree(self.source, resource)
        self.directory_link(resource / "pipeline" / "cycle", resource / "pipeline")
        self.assert_rejected(resource)

    def test_cli_preflights_all_resources_before_any_write(self):
        ordinary = self.install / "resource"
        shutil.copytree(self.source, ordinary)
        self.directory_link(self.install / "resource_extra", self.source)
        original = self.input_file.read_bytes()
        result = self.run_cli(self.install)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("链接", result.stdout)
        self.assertEqual((ordinary / "pipeline" / "group" / "task.json").read_bytes(), original)
        self.assertEqual(self.input_file.read_bytes(), original)
        self.assertFalse((ordinary / "pipeline" / "nodes.json").exists())
        self.assertFalse((self.source / "pipeline" / "nodes.json").exists())

    def test_cli_direct_script_merges_copied_resource(self):
        shutil.copytree(self.source, self.install / "resource")
        result = self.run_cli(self.install)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertTrue((self.install / "resource" / "pipeline" / "nodes.json").is_file())
        self.assertTrue(self.input_file.is_file())


if __name__ == "__main__":
    unittest.main()
