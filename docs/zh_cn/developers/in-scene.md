# 开发手册 - InScene 参考文档

## 1. 万能场景识别介绍

**InScene** 是 MaaEnd 中的场景识别模块，提供了用于识别画面是否处于某些场景中的功能。

### 核心概念

**万能场景识别** 的含义是: 通过 `And` 和 `Or` 算法类型引用 `assets/resource/pipeline/Interface/InScene` 文件夹中的节点，判断当前画面是否是目前任务需要的场景，调用方再执行相关 action 或进入 next。

### 实现原理

在 InScene 里放置用于判断当前界面的节点，使用 MaaFramework 的 `And` 和 `Or` 算法类型引用 `InScene` 中的节点，可以实现在多个任务中统一维护判断逻辑，并在需要时调用。

### 万能场景识别使用方式

在 pipeline 任务中需要判断当前画面的场景时，创建与需要的 `InScene` 节点名不同的节点，并在该节点使用 `And` 和 `Or` 算法类型，子识别列表中添加需要的 `InScene` 节点名。

### 示例

具体用法示例请参考 `assets/resource/pipeline/Interface/Example/InScene.json`，包含具体的调用方式。

### 接口概述

`InScene` 的接口放置于 `assets/resource/pipeline/Interface/InScene` 文件夹下，可以根据文件名判断需要的节点在哪个文件中。
当没有需要的接口时可以自行添加，方便他人使用和维护。
