import type { FullConfig, TestCases } from '@nekosu/maa-tools'
import fs from 'node:fs/promises'
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
  const testCaseFiles = await Array.fromAsync(fs.glob('**/test_*.json', { cwd: testsRoot }))
  testCaseFiles.sort((a, b) => a.localeCompare(b))
  return (
    await Promise.all(
      testCaseFiles.map(async (file) => {
        try {
          const content = await fs.readFile(path.resolve(testsRoot, file), 'utf8')
          const testCases = JSON.parse(content) as TestCases
          const resourcePath = resourceMap[testCases.configs.resource]
          const controllerPath = controllerMap[testCases.configs.controller]
          if (!resourcePath || !controllerPath) {
            return null
          }
          testCases.configs.imageRoot = path.join(controllerPath, resourcePath)
          return testCases
        } catch {
          return null
        }
      }),
    )
  ).filter((tc) => !!tc)
}

const config: FullConfig = {
  cwd: import.meta.dirname,

  maaVersion: 'latest',
  maaStdoutLevel: 'Error',

  check: {
    interfacePath: 'assets/interface.json',
    override: {
      'mpe-config': 'error',
    },
  },

  test: {
    interfacePath: 'assets/interface.json',
    casesCwd: 'tests/MaaEndTesting',
    cases: await fetchCases(),
  },
}

export default config
