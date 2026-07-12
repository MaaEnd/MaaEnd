# Development Guide - InScene Reference

## 1. Universal Scene Recognition Overview

**InScene** is the scene recognition module in MaaEnd. It provides functionality for identifying whether the current screen matches certain scenes.

### Core Concept

**Universal scene recognition** means: using `And` and `Or` algorithm types to reference nodes from the `assets/resource/pipeline/Interface/InScene` folder, determining whether the current screen is the scene required by the current task, then letting the caller execute the relevant action or proceed to `next`.

### How It Works

Place recognition nodes inside `InScene` that identify specific game screens. By using MaaFramework's `And` and `Or` algorithm types to reference nodes from `InScene`, you can maintain screen recognition logic centrally across multiple tasks and invoke it when needed.

### How to Use

When a Pipeline task needs to identify the current screen, create a node with a name different from the `InScene` node you intend to use. In that node, use `And` or `Or` algorithm types and add the required `InScene` node names to the child recognition list.

### Examples

For concrete usage examples, see `assets/resource/pipeline/Interface/Example/InScene.json`, which demonstrates the specific invocation pattern.

### Interface Overview

`InScene` interfaces are placed under `assets/resource/pipeline/Interface/InScene`. The filename usually indicates which nodes can be found inside.
When no existing interface fits your needs, feel free to add a new one for others to use and maintain.
