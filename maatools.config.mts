import type { FullConfig } from '@nekosu/maa-tools'
import { delimiter, dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

import { fetchCases } from './tests/scripts/loader.mts'
import parserConfig from './tools/parser'

const cwd = dirname(fileURLToPath(import.meta.url))

// custom 识别用例需要 AgentServer；产物位于 install/，由 `uv run build-and-install` 构建。
const installDir = join(cwd, 'install')
const maafwDir = join(installDir, 'maafw')
const agentPath = (name: string) => join(installDir, 'agent', process.platform === 'win32' ? `${name}.exe` : name)
const agentEnv = { PATH: maafwDir + delimiter + (process.env.PATH ?? '') }

const config: FullConfig = {
  cwd,

  maaVersion: 'latest',
  maaStdoutLevel: 'Error',
  maaLogDir: 'tests/maatools',

  interfacePath: 'assets/interface.json',

  parser: parserConfig,

  check: {
    override: {
      'mpe-config': 'error',
    },
  },

  test: {
    casesCwd: 'tests/MaaEndTestset',
    cases: fetchCases,
    errorDetailsPath: 'tests/maatools/error_details.json',
    // 只有待测节点用到 custom 识别时才会拉起；纯内置识别用例不依赖 install/ 下的产物。
    agent: [
      { child_exec: agentPath('cpp-algo'), env: agentEnv },
      { child_exec: agentPath('go-service'), env: { ...agentEnv, MAAFW_BINARY_PATH: maafwDir } },
    ],
  },

  vscode: {
    agents: {
      'agent/go-service': 'launch-go-agent',
      'agent/cpp-algo': 'launch-cpp-agent',
    },
  },
}

export default config
