import type { PropSelector, PropSelectorResult } from '@nekosu/maa-tools/pm'

// TODO: 之后更新下把类型导出了, 先简单糊下
type Node = Parameters<PropSelector>[1]
type ParserUtils = Parameters<PropSelector>[2]
type Policy = PropSelectorResult['missingPolicy']

export const tryAddTask = (utils: ParserUtils, result: PropSelectorResult[], node: Node, policy: Policy = 'error') => {
  if (utils.isString(node)) {
    result.push({
      node,
      type: 'taskRef',
      missingPolicy: policy,
    })
  }
}

export const tryAddTaskArray = (
  utils: ParserUtils,
  result: PropSelectorResult[],
  node: Node,
  policy: Policy = 'error',
) => {
  for (const task of utils.parseArray(node)) {
    tryAddTask(utils, result, task, policy)
  }
}

export const tryAddTemplate = (
  utils: ParserUtils,
  result: PropSelectorResult[],
  node: Node,
  policy: Policy = 'error',
) => {
  if (utils.isString(node)) {
    result.push({
      node,
      type: 'template',
      missingPolicy: policy,
    })
  }
}
