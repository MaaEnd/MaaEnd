import type { FullConfig } from '@nekosu/maa-tools'
import type { PropSelector, StringNode } from '@nekosu/maa-tools/pm'

import { fetchCases } from './tests/scripts/loader.mts'

const customRecoTaskRef: PropSelector = (name, param, utils) => {
  const result: StringNode[] = []
  if (name === 'autoEcoFarmFindNearestRecognitionResult') {
    for (const [key, obj] of utils.parseObject(param)) {
      if (key === 'RecognitionNodeName') {
        if (utils.isString(obj)) {
          result.push(obj)
        }
      }
    }
  }
  return result
}

const customActTaskRef: PropSelector = (name, param, utils) => {
  const result: StringNode[] = []
  if (name === 'SubTask') {
    for (const [key, obj] of utils.parseObject(param)) {
      if (key === 'sub') {
        for (const task of utils.parseArray(obj)) {
          if (utils.isString(task)) {
            result.push(task)
          }
        }
      }
    }
  }
  return result
}

const config: FullConfig = {
  cwd: import.meta.dirname,

  maaVersion: 'latest',
  maaStdoutLevel: 'Error',
  maaLogDir: 'tests/maatools',

  interfacePath: 'assets/interface.json',

  parser: {
    customReco: {
      taskRef: customRecoTaskRef,
    },
    customAction: {
      taskRef: customActTaskRef,
    },
  },

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
