import { TestCases } from '@nekosu/maa-tools'
import path from 'node:path'

type MatrixValue = string | string[]
type Rect = [number, number, number, number]
type MatrixContext = {
  controller: string
  resource: string
}
type RawBox = Rect | Record<string, Rect>
type RawHit =
  | string
  | {
      node: string
      box?: RawBox
    }
type RawCase = {
  name?: string
  image: string
  hits: RawHit[]
}
type RawTestCases = {
  configs: {
    name?: string
    resource: MatrixValue
    controller: MatrixValue
    imageRoot?: string
  }
  cases: RawCase[]
}

function normalizeMatrixValues(value: MatrixValue): string[] {
  return [...new Set(Array.isArray(value) ? value : [value])]
}

function isRect(value: unknown): value is Rect {
  return Array.isArray(value) && value.length === 4 && value.every((item) => Number.isInteger(item) && item >= 0)
}

function resolveBox(
  box: RawBox | undefined,
  matrix: MatrixContext,
  testGroupName: string | undefined,
  image: string,
  node: string,
): Rect | null {
  if (!box) {
    return null
  }
  if (isRect(box)) {
    return box
  }
  if (typeof box !== 'object' || Array.isArray(box)) {
    console.log(`invalid box config: ${testGroupName ?? '<unnamed>'} ${image} ${node}`)
    return null
  }

  const candidates = [
    `${matrix.controller}:${matrix.resource}`,
    `${matrix.controller}/${matrix.resource}`,
    `${matrix.resource}:${matrix.controller}`,
    `${matrix.resource}/${matrix.controller}`,
    matrix.controller,
    matrix.resource,
    'default',
    '*',
  ]

  for (const key of candidates) {
    const rect = box[key]
    if (isRect(rect)) {
      return rect
    }
  }

  console.log(
    `unresolved box matrix: ${testGroupName ?? '<unnamed>'} ${image} ${node} for ${matrix.controller}/${matrix.resource}`,
  )
  return null
}

function normalizeHit(
  hit: RawHit,
  matrix: MatrixContext,
  testGroupName: string | undefined,
  image: string,
): TestCases['cases'][number]['hits'][number] | null {
  if (typeof hit === 'string') {
    return hit
  }
  if (!hit.box) {
    return hit.node
  }

  const box = resolveBox(hit.box, matrix, testGroupName, image, hit.node)
  if (!box) {
    return null
  }

  return {
    node: hit.node,
    box,
  }
}

function formatMatrixTestName(matrix: MatrixContext, name: string | undefined): string {
  const prefix = `(${matrix.controller}-${matrix.resource})`
  return `${prefix}${name ?? '<unnamed>'}`
}

export async function fetchCases(): Promise<TestCases[]> {
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
  ] = (await loadAllTestCases(testsRoot, '**/test_*.json')) as unknown as [RawTestCases[], string[]]
  for (const file of failPaths) {
    console.log(`load testcases failed: ${file}`)
  }

  const expandedTestCases: TestCases[] = []
  for (const testCases of allTestCases) {
    const controllers = normalizeMatrixValues(testCases.configs.controller)
    const resources = normalizeMatrixValues(testCases.configs.resource)

    const resourcePaths = resources.map((resource) => ({
      resource,
      resourcePath: resourceMap[resource],
    }))
    const unknownResources = resourcePaths.filter(({ resourcePath }) => !resourcePath)
    if (unknownResources.length > 0) {
      for (const { resource } of unknownResources) {
        console.log(`unknown resource: ${resource}`)
      }
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
      for (const { resource, resourcePath } of resourcePaths as Array<{
        resource: string
        resourcePath: string
      }>) {
        const matrix = {
          controller,
          resource,
        }
        const normalizedCases: TestCases['cases'] = []
        let invalidMatrix = false

        for (const testCase of testCases.cases) {
          const normalizedHits: TestCases['cases'][number]['hits'] = []

          for (const hit of testCase.hits) {
            const normalizedHit = normalizeHit(hit, matrix, testCases.configs.name, testCase.image)
            if (!normalizedHit) {
              invalidMatrix = true
              break
            }
            normalizedHits.push(normalizedHit)
          }

          if (invalidMatrix) {
            break
          }

          normalizedCases.push({
            ...testCase,
            hits: normalizedHits,
          })
        }

        if (invalidMatrix) {
          continue
        }

        expandedTestCases.push({
          configs: {
            ...testCases.configs,
            name: formatMatrixTestName(matrix, testCases.configs.name),
            controller,
            resource,
            imageRoot: path.join(controllerPath, resourcePath),
          },
          cases: normalizedCases,
        })
      }
    }
  }
  return expandedTestCases
}
