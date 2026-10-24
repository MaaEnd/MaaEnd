import json
import os
import tempfile
import time
import unittest
from pathlib import Path

import numpy as np
from maa.controller import CustomController
from maa.custom_action import CustomAction
from maa.custom_recognition import CustomRecognition
from maa.library import Library
from maa.resource import Resource
from maa.tasker import Tasker

SOURCE = Path(__file__).resolve().parents[4]
PIPELINE = SOURCE / "assets/resource_cloud_web/pipeline/SceneManager/SceneCloudWeb.json"


def state(page, **kwargs):
    return {
        "page": page,
        "account": "signed_in",
        "video_ready": page == "stream",
        "startup_resource_failed": page == "startup_error",
        "document_epoch": 1,
        **kwargs,
    }


class Browser(CustomController):
    def __init__(self, states):
        self.states = list(states)
        self.clicks = []
        self.reloads = []
        self.age = 0
        self.frames = 0
        self.done = False
        super().__init__()

    def advance(self):
        if len(self.states) > 1:
            self.states.pop(0)
            self.age = 0

    def connect(self):
        return True

    def request_uuid(self):
        return "pipeline-test"

    def start_app(self, intent):
        return False

    def stop_app(self, intent):
        return False

    def get_features(self):
        return 0

    def screencap(self):
        self.frames += 1
        self.age += 1
        if self.age >= 4 and self.states[0].get("auto"):
            self.advance()
        return np.zeros((720, 1280, 3), dtype=np.uint8)

    def click(self, x, y):
        current = self.states[0]
        targets = [v for k, v in current.items() if k.endswith("_target") and v is not None]
        if {"x": x, "y": y} not in targets:
            return False
        self.clicks.append(current["page"])
        self.advance()
        return True

    def swipe(self, *args):
        return False

    def touch_down(self, *args):
        return False

    def touch_move(self, *args):
        return False

    def touch_up(self, *args):
        return False

    def click_key(self, *args):
        return False

    def input_text(self, *args):
        return False

    def key_down(self, *args):
        return False

    def key_up(self, *args):
        return False


class Page(CustomRecognition):
    def __init__(self, browser):
        self.browser = browser
        super().__init__()

    def analyze(self, context, argv):
        query = json.loads(argv.custom_recognition_param)
        current = self.browser.states[0]
        if any(current.get(k) != v for k, v in query.get("match", {}).items()):
            return None
        if "changed_from" in query:
            previous = context.tasker.get_latest_node(query["changed_from"])
            if not previous:
                return None
            detail = previous.recognition.raw_detail
            if detail["best"]["detail"]["document_epoch"] == current["document_epoch"]:
                return None
        point = current.get(query.get("target"), {"x": 0, "y": 0})
        if point is None:
            return None
        return self.AnalyzeResult(box=(point["x"], point["y"], 1, 1), detail=current)


class Action(CustomAction):
    def __init__(self, browser, done=False):
        self.browser = browser
        self.done = done
        super().__init__()

    def run(self, context, argv):
        if self.done:
            self.browser.done = True
        else:
            self.browser.reloads.append(json.loads(argv.custom_action_param)["ignore_cache"])
            self.browser.advance()
        return True


class StartupPipelineTest(unittest.TestCase):
    def run_case(self, states, *, login=False, success=True, clicks=(), reloads=()):
        browser = Browser(states)
        resource = Resource()
        page = Page(browser)
        reload_action = Action(browser)
        done_action = Action(browser, done=True)
        resource.register_custom_recognition("CloudPageState", page)
        resource.register_custom_action("CloudPageReload", reload_action)
        resource.register_custom_action("TestDone", done_action)
        pipeline = json.loads(PIPELINE.read_text(encoding="utf-8"))
        # Keep production recognition, edges, anchors and hit budgets. Shorten
        # polling/deadlines only so deliberate failure cases finish in CI.
        for data in pipeline.values():
            data["rate_limit"] = 10
            if "timeout" in data:
                data["timeout"] = 300
            if "pre_delay" in data:
                data["pre_delay"] = 0
            if "post_delay" in data:
                data["post_delay"] = 0
        pipeline["TestEntry"] = {"next": ["CloudWebLogin"] if login else ["[JumpBack]CloudWebPrepare", "TestDone"]}
        pipeline["TestDone"] = {"action": {"type": "Custom", "param": {"custom_action": "TestDone"}}}
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder)
            (path / "pipeline").mkdir()
            (path / "pipeline/startup.json").write_text(json.dumps(pipeline), encoding="utf-8")
            self.assertTrue(resource.post_bundle(path).wait().succeeded)
            self.assertTrue(browser.post_connection().wait().succeeded)
            tasker = Tasker()
            self.assertTrue(tasker.bind(resource, browser))
            job = tasker.post_task("TestEntry")
            deadline = time.monotonic() + 5
            while not job.done and time.monotonic() < deadline:
                time.sleep(0.02)
            if not job.done:
                tasker.post_stop().wait()
                self.fail("Startup Pipeline did not terminate within the test deadline")
            job.wait()
            self.assertEqual(job.succeeded, success)
            if login:
                self.assertIsNotNone(tasker.get_latest_node("__ScenePrivateCloudWebSignedIn"))
            else:
                self.assertEqual(browser.done, success)
            self.assertEqual(browser.clicks, list(clicks))
            self.assertEqual(browser.reloads, list(reloads))

    def test_start_and_queue(self):
        self.run_case(
            [
                state("landing", start_available=True, start_target={"x": 1100, "y": 600}),
                state("queue", auto=True),
                state("starting", auto=True),
                state("stream"),
            ],
            clicks=["landing"],
        )

    def test_wait_for_user_login(self):
        self.run_case([state("login", account="signed_out", auto=True), state("landing")], login=True)

    def test_login_does_not_start_game(self):
        self.run_case([state("landing", start_target={"x": 1100, "y": 600})], login=True)

    def test_busy_session(self):
        self.run_case(
            [
                state("session_busy", session_busy_confirm_target={"x": 640, "y": 450}),
                state("landing", start_target={"x": 1100, "y": 600}),
                state("stream"),
            ],
            clicks=["session_busy", "landing"],
        )

    def test_ended_notice(self):
        self.run_case(
            [
                state("ended", ended_confirm_target={"x": 640, "y": 450}),
                state("landing", start_target={"x": 1100, "y": 600}),
                state("stream"),
            ],
            clicks=["ended", "landing"],
        )

    def test_queue_timeout(self):
        self.run_case(
            [
                state("queue_timeout", queue_timeout_confirm_target={"x": 640, "y": 450}),
                state("landing", start_target={"x": 1100, "y": 600}),
                state("stream"),
            ],
            clicks=["queue_timeout", "landing"],
        )

    def test_two_reload_budget(self):
        self.run_case(
            [
                state("startup_error"),
                state("startup_error", document_epoch=2),
                state("landing", document_epoch=3, start_target={"x": 1100, "y": 600}),
                state("stream", document_epoch=3),
            ],
            clicks=["landing"],
            reloads=[False, True],
        )

    def test_exhausted_reload_budget(self):
        self.run_case(
            [
                state("startup_error"),
                state("startup_error", document_epoch=2),
                state("startup_error", document_epoch=3),
            ],
            success=False,
            reloads=[False, True],
        )

    def test_blocked_confirmation_never_clicks(self):
        self.run_case([state("ended", ended_confirm_target=None)], success=False)


if __name__ == "__main__":
    if library_path := os.environ.get("MAAEND_TEST_FW_DIR"):
        Library.open(Path(library_path))
    Tasker.set_log_dir("")
    Tasker.set_save_on_error(False)
    unittest.main()
