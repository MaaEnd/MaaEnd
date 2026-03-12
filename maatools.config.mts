import type { FullConfig } from '@nekosu/maa-tools'

import { fetchCases } from './tests/scripts/loader.mts'

const config: FullConfig = {
  cwd: import.meta.dirname,

  maaVersion: 'latest',
  maaStdoutLevel: 'Error',
  maaLogDir: 'tests/maatools',

  interfacePath: 'assets/interface.json',

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
