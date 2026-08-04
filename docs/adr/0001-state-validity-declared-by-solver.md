# ADR-0001：状态合法性由求解器声明，识别层不得复制范围校验

- 状态：Accepted（2026-08）
- 涉及：`agent/go-service/trialofswordmancy/`（`gamestate.go` / `recognition.go` / `solver/`）

## Context

选剑演武总成识别（`Recognition.Run`）每步把截图读数转换为 `solver.State` 交给 MDP 求解器。架构评审期间曾提议：

1. 在识别推导层（`deriveGameState`）增加读数范围校验——OCR 演算次数读到 4（屏幕理论范围 0..3）等模型外读数视为识别失败；
2. 为 `deriveGameState` 配单元测试：先是表驱动用例，后改为「输出必为求解器可达状态」的跨模块性质测试。

两条均被否决，理由如下：

- **「OCR 失败」的定义是「结果中不含数字」**（parse 失败）。OCR 返回一个自洽但模型外的值（如 4）不是识别失败——读数本身有效，只是落在 MDP 模型外。
- **状态合法性是 solver 的声明式领地**：`stateFilter`（§6.8，1:1 迁移自 TypeScript）是「哪些状态存在」的唯一权威。识别层复制一套范围校验，等于把 solver 的谓词抄过 seam——将来等级变化使合法值扩界时，识别层会误杀合法读数，而 solver 本可优雅回答「不可达」。
- **Decide 对不可达状态的处置是刻意设计**：红色 focus「状态不可达」+ 中止。识别层只保证「读数的忠实转换」，模型外的状态由求解器裁决。
- **表驱动用例等价于重抄实现**：断言的只是「代码做了它做的事」，不是「做对了」，不提供独立判据。跨模块性质测试在「合法性由 solver 声明」的设计下同样无契约可钉——derive 的输出域与 solver 状态空间的关系由 solver 单方面定义，可断言的全是恒真式。

## Decision

1. 识别推导层（`deriveGameState`）的失败面**唯一**：读数互相矛盾（手牌推导越界 = 缓存 Deck 与 remainDeck OCR 不自洽）。此情形两个读数必有一个是错的，不在矛盾信息上做决策，显式中止。
2. OCR/模板返回的模型外**自洽**读数（演算次数 4、翻倍次数 3、翻倍态与次数看似矛盾等）一律透传，由 `solver.stateFilter` 判不可达、Decide 中止。
3. `deriveGameState` 不配单元测试。识别正确性由真实截图集成测试（`tests/`）与线上运行保障。

## Consequences

- 新增游戏等级、合法值扩界时，只需改 solver 侧（状态空间/常量），识别层零改动。
- 误读（如 `allOCRText` 拼框读出 21）会以「状态不可达」中止而非「OCR 失败」——根因排查需结合 recognition 的 `game state recognized` 日志，这是接受的代价。
- 未来若为识别层增加任何范围校验，视为违反本 ADR，除非 solver 的声明式领地同步迁移。
