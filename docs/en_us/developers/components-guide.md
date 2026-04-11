# Components guide

This document describes MaaEnd’s architecture and reusable components. Before adding new nodes, read it to see whether something already exists.

## Project architecture

MaaEnd is built on [MaaFramework](https://github.com/MaaXYZ/MaaFramework). The main flow uses [Pipeline JSON low-code](/assets/resource/pipeline); complex logic is implemented in [go-service](/agent/go-service). If you plan to contribute, read the [MaaFramework docs](https://maafw.com/) first; there is also a [MaaFramework tutorial video](https://www.bilibili.com/video/BV1yr421E7MW) (dated—prefer the docs).

Think of the project in four layers:

1. `assets/interface.json` — project entry, controllers, resources, task imports, Agent startup.
2. `assets/tasks/**/*.json` — how tasks appear in the UI, entry nodes, options.
3. `assets/resource/pipeline/**/*.json` — “what to recognize, where to tap, what’s next.” **This is what you touch most often.**
4. `agent/go-service/**` — only logic that Pipeline cannot express well (heavy recognition, math, iteration, special interaction).

Execution path for a task:

`UI Task → Pipeline entry node → recognize/act loop → optional Go custom logic`

**Prefer changing Task + Pipeline first—not jumping straight to Go.**

## Where to change what

| Kind of change                                     | Where to look                                             |
| -------------------------------------------------- | --------------------------------------------------------- |
| UI copy, task names, option labels                 | `assets/locales/interface/en_us.json` (and other locales) |
| Task wiring, entry node, UI options                | `assets/tasks/**/*.json`                                  |
| Recognition, taps, navigation, waits, flow details | `assets/resource/pipeline/**/*.json`                      |
| Complex logic (algorithms, loops, math)            | `agent/go-service/**`                                     |

## Reusable Pipeline nodes

These can be called directly from Pipeline; every Pipeline author should know them:

| Node           | Description                                            | Doc                                      |
| -------------- | ------------------------------------------------------ | ---------------------------------------- |
| Common buttons | White/yellow confirm, cancel, close, teleport, etc.    | [common-buttons.md](./common-buttons.md) |
| SceneManager   | Universal navigation from any screen to a target scene | [scene-manager.md](./scene-manager.md)   |

## Reusable Custom nodes

Implemented in Go/C++ with stronger coupling to game behavior. Per [coding standards](./coding-standards.md#go-service-standards), avoid unless necessary.

| Node                                            | Description                                       | Doc                                                                        |
| ----------------------------------------------- | ------------------------------------------------- | -------------------------------------------------------------------------- |
| SubTask / ClearHitCount / ExpressionRecognition | Subtasks, hit-count reset, expression recognition | [custom.md](./custom.md)                                                   |
| AutoFight                                       | In-combat automation                              | [components/auto-fight.md](./components/auto-fight.md)                     |
| CharacterController                             | Camera, movement, face target                     | [components/character-controller.md](./components/character-controller.md) |
| QuantizedSliding                                | Discrete quantity sliders                         | [components/quantized-sliding.md](./components/quantized-sliding.md)       |
| MapLocator                                      | AI + CV minimap localization                      | [components/map-locator.md](./components/map-locator.md)                   |
| MapTracker                                      | Minimap tracking & path movement                  | [components/map-tracker.md](./components/map-tracker.md)                   |
| MapNavigator                                    | High-precision navigation + GUI recorder          | [components/map-navigator.md](./components/map-navigator.md)               |

## Task maintenance docs

These tasks have dedicated maintenance guides—**read the matching doc before changing the task**:

| Task           | Doc                                                                      |
| -------------- | ------------------------------------------------------------------------ |
| AutoStockpile  | [tasks/auto-stockpile-maintain.md](./tasks/auto-stockpile-maintain.md)   |
| DijiangRewards | [tasks/dijiang-rewards-maintain.md](./tasks/dijiang-rewards-maintain.md) |
| CreditShopping | [tasks/credit-shopping-maintain.md](./tasks/credit-shopping-maintain.md) |

## External documentation

- [MaaFramework official docs](https://maafw.com/)
- [Pipeline protocol](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/en_us/3.1-PipelineProtocol.md)
- [Project interface V2](https://github.com/MaaXYZ/MaaFramework/blob/main/docs/en_us/3.3-ProjectInterfaceV2.md)
