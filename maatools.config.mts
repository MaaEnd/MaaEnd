import type { FullConfig, TestCases } from '@nekosu/maa-tools'
import path from 'node:path'

type ControllerConfig = string | string[]

function normalizeControllers(controller: ControllerConfig): string[] {
  return Array.isArray(controller) ? controller : [controller]
}

async function fetchCases(): Promise<TestCases[]> {
  const { loadAllTestCases } = await import('@nekosu/maa-tools')

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

  const expandedTestCases: TestCases[] = []
  for (const testCases of allTestCases) {
    const controllers = normalizeControllers(testCases.configs.controller as ControllerConfig)
    const resourcePath = resourceMap[testCases.configs.resource]
    if (!resourcePath) {
      console.log(`unknown resource: ${testCases.configs.resource}`)
      continue
    }

    const controllerPaths = controllers.map((controller) => ({
      controller,
      controllerPath: controllerMap[controller],
    }))
    const unknownControllers = controllerPaths.filter(({ controllerPath }) => !controllerPath)
    if (unknownControllers.length > 0) {
      for (const { controller } of unknownControllers) {
        console.log(`unknown controller: ${controller}`)
      }
      continue
    }

    for (const { controller, controllerPath } of controllerPaths as Array<{
      controller: string
      controllerPath: string
    }>) {
      expandedTestCases.push({
        ...testCases,
        configs: {
          ...testCases.configs,
          controller,
          imageRoot: path.join(controllerPath, resourcePath),
        },
      })
    }
  }
  return expandedTestCases
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
