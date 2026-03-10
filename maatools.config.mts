import { loadAllTestCases, type FullConfig, type TestCases } from '@nekosu/maa-tools'
import path from 'node:path'

async function fetchCases(): Promise<TestCases[]> {
  const resourceMap: Record<string, string> = {
    官服: 'Official_CN',
    // 'B 服': '',
    // Global: '',
  }
  const controllerMap: Record<string, string> = {
    'Win32-Window': 'Win32',
    'Win32-Window-Background': 'Win32',
    Win32: 'Win32',
    'Win32-Front': 'Win32',
    ADB: 'ADB',
    // 'PlayCover': '',
  }

  const testsRoot = path.resolve(import.meta.dirname, 'tests')
  const [
    allTestCases,
    failPaths,
  ] = await loadAllTestCases(testsRoot, '**/test_*.json')
  for (const file of failPaths) {
    console.log(`load testcases failed: ${file}`)
  }

  for (const testCases of allTestCases) {
    const controllerPath = controllerMap[testCases.configs.controller]
    const resourcePath = resourceMap[testCases.configs.resource]
    if (!controllerPath) {
      console.log(`unknown controller: ${testCases.configs.controller}`)
      continue
    }
    if (!resourcePath) {
      console.log(`unknown resource: ${testCases.configs.resource}`)
      continue
    }
    testCases.configs.imageRoot = path.join(controllerPath, resourcePath)
  }
  return allTestCases
}

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
