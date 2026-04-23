import type { FullConfig } from '@nekosu/maa-tools'
import { dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

import { fetchCases } from './tests/scripts/loader.mts'
import parserConfig from './tools/parser'

const cwd = dirname(fileURLToPath(import.meta.url))

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
  },

  vscode: {
    agents: {
      'agent/go-service': 'launch-go-agent',
      'agent/cpp-algo': 'launch-cpp-agent',
    },
  },
}

export default config
