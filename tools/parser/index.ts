import type { ParserConfig, PropSelector, PropSelectorResult } from '@nekosu/maa-tools/pm'
import { tryAddTask, tryAddTaskArray, tryAddTemplate } from './utils'

const customRecoParser: PropSelector = (name, param, utils) => {
  const result: PropSelectorResult[] = []
  if (name === 'autoEcoFarmFindNearestRecognitionResult') {
    for (const [key, obj] of utils.parseObject(param)) {
      if (key === 'RecognitionNodeName') {
        tryAddTask(utils, result, obj)
      }
    }
  }
  return result
}

const customActParser: PropSelector = (name, param, utils) => {
  const result: PropSelectorResult[] = []
  if (name === 'SubTask') {
    for (const [key, obj] of utils.parseObject(param)) {
      if (key === 'sub') {
        for (const task of utils.parseArray(obj)) {
          tryAddTask(utils, result, task)
        }
      }
    }
  } else if (name === 'ClearHitCount') {
    for (const [key, obj] of utils.parseObject(param)) {
      if (key === 'nodes') {
        tryAddTaskArray(utils, result, obj, 'ignore')
      }
    }
  } else if (name === 'BetterSliding') {
    for (const [key, obj] of utils.parseObject(param)) {
      if (key === 'IncreaseButton' || key === 'DecreaseButton' || key === 'SwipeButton') {
        tryAddTemplate(utils, result, obj)
      }
    }
  }
  return result
}

const parser: ParserConfig = {
  customReco: customRecoParser,
  customAction: customActParser,
}

export default parser
