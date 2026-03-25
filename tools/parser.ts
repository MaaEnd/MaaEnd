import { ParserConfig, PropSelector, PropSelectorResult } from '@nekosu/maa-tools/pm'

const customRecoParser: PropSelector = (name, param, utils) => {
  const result: PropSelectorResult[] = []
  if (name === 'autoEcoFarmFindNearestRecognitionResult') {
    for (const [key, obj] of utils.parseObject(param)) {
      if (key === 'RecognitionNodeName') {
        if (utils.isString(obj)) {
          result.push({
            node: obj,
            type: 'taskRef',
            missingPolicy: 'error',
          })
        }
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
          if (utils.isString(task)) {
            result.push({
              node: task,
              type: 'taskRef',
              missingPolicy: 'error',
            })
          }
        }
      }
    }
  } else if (name === 'ClearHitCount') {
    for (const [key, obj] of utils.parseObject(param)) {
      if (key === 'nodes') {
        for (const task of utils.parseArray(obj)) {
          if (utils.isString(task)) {
            result.push({
              node: task,
              type: 'taskRef',
              missingPolicy: 'ignore',
            })
          }
        }
      }
    }
  } else if (name === 'QuantizedSliding') {
    for (const [key, obj] of utils.parseObject(param)) {
      if (key === 'IncreaseButton' || key === 'DecreaseButton') {
        if (utils.isString(obj)) {
          result.push({
            node: obj,
            type: 'template',
            missingPolicy: 'error',
          })
        }
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
