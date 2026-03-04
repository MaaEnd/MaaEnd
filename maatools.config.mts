import type { FullConfig } from '@nekosu/maa-tools'
import { globSync, readFileSync } from 'node:fs'
import path from 'node:path'

const testsRoot = path.resolve(import.meta.dirname, 'tests')
const testCaseFiles = globSync('**/test_*.json', { cwd: testsRoot }).sort((a, b) => a.localeCompare(b))
const cases = testCaseFiles.map((relativePath) =>
  JSON.parse(readFileSync(path.resolve(testsRoot, relativePath), 'utf-8')),
)

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
    cases,
  },
}

export default config
