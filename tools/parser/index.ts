import type { ParserConfig, PropSelector, PropSelectorResult } from '@nekosu/maa-tools/pm'
import { tryAddTask, tryAddTaskArray, tryAddTemplate } from './utils'

type ParserNode = Parameters<PropSelector>[1]
type ParserUtils = Parameters<PropSelector>[2]
type MissingPolicy = PropSelectorResult['missingPolicy']

// 插件的模板索引以 image 目录为根，部分 Custom 参数则以 resource 目录为根。
const resourceTemplatePrefixes = ['resource/image/', 'resource_adb/image/', 'image/'] as const

const tryAddTemplateRef = (
  node: ParserNode,
  utils: ParserUtils,
  result: PropSelectorResult[],
  prefixes: readonly string[] = [],
) => {
  if (!utils.isString(node)) {
    return
  }

  const prefix = prefixes.find((item) => node.value.startsWith(item))
  tryAddTemplate(utils, result, prefix ? { ...node, value: node.value.slice(prefix.length) } : node)
}

const tryAddTemplateArray = (
  node: ParserNode,
  utils: ParserUtils,
  result: PropSelectorResult[],
  prefixes: readonly string[] = [],
) => {
  for (const template of utils.parseArray(node)) {
    tryAddTemplateRef(template, utils, result, prefixes)
  }
}

const tryAddTemplateFields = (
  param: ParserNode,
  utils: ParserUtils,
  result: PropSelectorResult[],
  fields: readonly string[],
  arrayFields: readonly string[] = [],
  prefixes: readonly string[] = [],
) => {
  for (const [key, obj] of utils.parseObject(param)) {
    if (fields.includes(key)) {
      tryAddTemplateRef(obj, utils, result, prefixes)
    } else if (arrayFields.includes(key)) {
      tryAddTemplateArray(obj, utils, result, prefixes)
    }
  }
}

const tryAddPipelineOverrideTemplates = (param: ParserNode, utils: ParserUtils, result: PropSelectorResult[]) => {
  for (const [key, obj] of utils.parseObject(param)) {
    if (key === 'template') {
      tryAddTemplateRef(obj, utils, result)
      tryAddTemplateArray(obj, utils, result)
    } else {
      tryAddPipelineOverrideTemplates(obj, utils, result)
    }
  }

  for (const obj of utils.parseArray(param)) {
    tryAddPipelineOverrideTemplates(obj, utils, result)
  }
}

const tryAddTaskFields = (
  param: ParserNode,
  utils: ParserUtils,
  result: PropSelectorResult[],
  fields: readonly string[],
  arrayFields: readonly string[] = [],
  policy: MissingPolicy = 'error',
) => {
  for (const [key, obj] of utils.parseObject(param)) {
    if (fields.includes(key)) {
      tryAddTask(utils, result, obj, policy)
    } else if (arrayFields.includes(key)) {
      tryAddTaskArray(utils, result, obj, policy)
    }
  }
}

const tryAddTaskMapValues = (
  param: ParserNode,
  utils: ParserUtils,
  result: PropSelectorResult[],
  fields: readonly string[],
  policy: MissingPolicy = 'error',
) => {
  for (const [key, obj] of utils.parseObject(param)) {
    if (!fields.includes(key)) {
      continue
    }
    for (const [, value] of utils.parseObject(obj)) {
      tryAddTask(utils, result, value, policy)
    }
  }
}

const customRecoParser: PropSelector = (name, param, utils) => {
  const result: PropSelectorResult[] = []

  // 模板引用
  if (name === 'MapTrackerBigMapFindImage') {
    tryAddTemplateFields(param, utils, result, ['template'], [], resourceTemplatePrefixes)
  } else if (name === 'EssenceGridAdvanceRecognition') {
    tryAddTemplateFields(
      param,
      utils,
      result,
      ['thumb_discard_template_path'],
      ['thumb_lock_template_paths'],
      resourceTemplatePrefixes,
    )
  }

  // 节点引用
  if (name === 'autoEcoFarmFindNearestRecognitionResult' || name === 'EssenceFilterAfterBattleNthRecognition') {
    tryAddTaskFields(param, utils, result, ['recognitionNodeName', 'RecognitionNodeName'])
  } else if (name === 'ExpendableRecognition') {
    tryAddTaskFields(param, utils, result, ['candidate', 'visited_node'])
  } else if (name === 'ExpressionRecognition') {
    tryAddTaskFields(param, utils, result, ['box_node'])
  } else if (name === 'ListCompleteRecognition') {
    tryAddTaskFields(param, utils, result, ['node'])
  }
  return result
}

const customActParser: PropSelector = (name, param, utils) => {
  const result: PropSelectorResult[] = []

  // 模板引用
  if (name === 'autoEcoFarmOverrideTargetTemplate') {
    tryAddTemplateFields(param, utils, result, ['template'])
  } else if (name === 'BetterSliding') {
    tryAddTemplateFields(param, utils, result, ['IncreaseButton', 'DecreaseButton', 'SwipeButton'])
  } else if (name === 'PipelineOverride' || name === 'PipelineOverrideAction') {
    tryAddPipelineOverrideTemplates(param, utils, result)
  }

  // 节点引用
  if (name === 'SubTask') {
    tryAddTaskFields(param, utils, result, [], ['sub'])
  } else if (name === 'AttachToExpectedRegexAction') {
    tryAddTaskFields(param, utils, result, ['target'], ['targets'])
  } else if (name === 'AutoStockStapleQuantityControlAction') {
    tryAddTaskFields(param, utils, result, ['validator_node', 'sliding_node'])
  } else if (name === 'autoEcoFarmOverrideTargetTemplate') {
    tryAddTaskFields(param, utils, result, [], ['nodeNames'])
  } else if (name === 'CharacterSearchAction' || name === 'RepeatUntilFoundAction') {
    tryAddTaskFields(param, utils, result, [], ['wait_nodes'])
  } else if (name === 'ClearHitCount') {
    tryAddTaskFields(param, utils, result, [], ['nodes'], 'ignore')
  } else if (name === 'FailureCollectorRunTask') {
    tryAddTaskFields(param, utils, result, ['task', 'failure_task', 'recovery_task'])
  } else if (name === 'BetterSliding') {
    tryAddTaskFields(param, utils, result, ['OutOfRangeOverrideEnable', 'TargetReachableOverrideEnable'])
  } else if (name === 'RepeatUntilNotFoundAction') {
    tryAddTaskFields(param, utils, result, ['wait_node'])
  } else if (name === 'RealTimeTaskAction') {
    tryAddTaskFields(param, utils, result, [], ['nodes'], 'ignore')
  } else if (name === 'SellProductReserveSession') {
    tryAddTaskFields(param, utils, result, ['sliding_node'])
  } else if (name === 'AddItemData' || name === 'SyncItemData') {
    tryAddTaskMapValues(param, utils, result, ['items'])
  }
  return result
}

const parser: ParserConfig = {
  customReco: customRecoParser,
  customAction: customActParser,
}

export default parser
