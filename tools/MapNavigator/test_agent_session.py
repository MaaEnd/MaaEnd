from __future__ import annotations

import os
import subprocess
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

import agent_session
from agent_session import AgentSession


class AgentSessionProcessTest(unittest.TestCase):
    def test_agent_does_not_inherit_terminal_interrupts(self) -> None:
        runtime = Mock()
        resource = runtime.Resource.return_value
        client = runtime.AgentClient.return_value
        client.connected = True
        tasker = runtime.Tasker.return_value
        tasker.inited = True

        connector = Mock()
        connector.connect.return_value = Mock()
        process = Mock()
        process.poll.return_value = None

        with (
            patch.object(agent_session, "CPP_AGENT_EXE", Path(__file__)),
            patch.object(agent_session, "get_agent_env", return_value={}),
            patch.object(agent_session, "new_agent_id", return_value="MapNavigatorTestAgent"),
            patch.object(agent_session.time, "sleep"),
            patch.object(agent_session.subprocess, "Popen", return_value=process) as popen,
        ):
            session = AgentSession(runtime)
            session.open(connector, agent_name="MapNavigatorTestAgent")
            session.close()

        options = popen.call_args.kwargs
        if os.name == "nt":
            self.assertEqual(options["creationflags"], subprocess.CREATE_NEW_PROCESS_GROUP)
            self.assertNotIn("start_new_session", options)
        else:
            self.assertTrue(options["start_new_session"])
            self.assertNotIn("creationflags", options)
        process.terminate.assert_called_once_with()
        process.wait.assert_called_once_with()
        connector.attach_resource.assert_called_once_with(resource)


if __name__ == "__main__":
    unittest.main()
