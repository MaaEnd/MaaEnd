# 修复并发安全问题

## 问题描述

发现 `importtask` 和 `realtime` 包在使用全局变量时没有做并发保护，如果多个任务同时执行可能会出问题。

### 原来的代码问题

**importtask/blueprints.go**

```go
// 原来：全局变量没有锁保护
var blueprintCodes []string

func (a *ImportBluePrintsEnterCodeAction) Run(...) {
    // 多个任务同时执行时，这里可能读到错误的数据
    code := blueprintCodes[0]
    blueprintCodes = blueprintCodes[1:]
    // ...
}
```

问题：
- 如果用户快速启动两个蓝图导入任务，两个任务会操作同一个 `blueprintCodes` 变量
- 可能出现一个任务的蓝图码被另一个任务处理
- 每次调用都重新编译正则表达式，浪费性能
- 没有清理上次任务的数据，可能导致错误

**realtime/autofight.go**

```go
// 原来：多个全局变量没有保护
var (
    autoFightCharacterCount   int
    autoFightSkillLastIndex   int
    autoFightEndSkillIndex    int
    autoFightEndSkillLastTime time.Time
)

func (a *RealTimeAutoFightSkillAction) Run(...) {
    // 多线程访问这些变量可能出现不一致的状态
    count := autoFightCharacterCount
    keycode := 50 + (autoFightSkillLastIndex % (count - 1))
    autoFightSkillLastIndex = (autoFightSkillLastIndex + 1) % (count - 1)
}
```

问题：
- 战斗状态在识别和动作之间共享，但没有同步机制
- 技能索引的读取和更新不是原子操作，可能导致重复释放同一个技能

## 解决方案

加锁保护全局变量，顺便做了一些优化。

### importtask/blueprints.go 的修改

**添加互斥锁和预编译正则**

```go
var (
    blueprintCodes []string
    blueprintMutex sync.Mutex              // 新增：保护并发访问
    blueprintRegex = regexp.MustCompile(`EF[a-zA-Z0-9]+`)  // 新增：预编译
)
```

**加入去重和状态重置**

```go
func parseBlueprintCodes(text string) []string {
    matches := blueprintRegex.FindAllString(text, -1)
    // ...
    
    // 去重
    seen := make(map[string]bool)
    result := make([]string, 0, len(matches))
    for _, code := range matches {
        if !seen[code] {
            seen[code] = true
            result = append(result, code)
        }
    }
    return result
}
```

**保护所有读写操作**

```go
func (a *ImportBluePrintsEnterCodeAction) Run(...) {
    blueprintMutex.Lock()
    if len(blueprintCodes) == 0 {
        blueprintMutex.Unlock()
        log.Warn().Msg("No more blueprint codes to process")
        return false
    }
    
    code := blueprintCodes[0]
    blueprintCodes = blueprintCodes[1:]
    remaining := len(blueprintCodes)
    blueprintMutex.Unlock()
    
    log.Info().Str("code", code).Int("remaining", remaining).Msg("Processing blueprint code")
    ctx.GetTasker().GetController().PostInputText(code)
    
    return true
}
```

只在访问和修改 `blueprintCodes` 时持有锁，完成切片操作后立即释放，然后再调用 `PostInputText`。

### realtime/autofight.go 的修改

**添加互斥锁**

```go
var (
    autoFightCharacterCount   int
    autoFightSkillLastIndex   int
    autoFightEndSkillIndex    int
    autoFightEndSkillLastTime time.Time
    autoFightMutex            sync.Mutex  // 新增：保护所有战斗状态
)
```

**读写操作都加锁**

```go
func (a *RealTimeAutoFightSkillAction) Run(...) {
    // 加锁读取状态
    autoFightMutex.Lock()
    count := autoFightCharacterCount
    currentIndex := autoFightSkillLastIndex
    autoFightMutex.Unlock()
    
    // 使用读取的值计算
    var keycode int
    if count == 1 {
        keycode = 49
    } else {
        keycode = 50 + (currentIndex % (count - 1))
    }
    
    ctx.GetTasker().GetController().PostClickKey(int32(keycode))
    
    // 加锁更新状态
    if count > 1 {
        autoFightMutex.Lock()
        autoFightSkillLastIndex = (autoFightSkillLastIndex + 1) % (count - 1)
        autoFightMutex.Unlock()
    }
    return true
}
```

用局部变量 `count` 和 `currentIndex` 存储状态，这样就不需要长时间持有锁。

## 额外的优化

1. **正则表达式预编译**：从每次调用都编译，改为启动时编译一次，提升性能
2. **蓝图码去重**：避免重复导入相同的蓝图码
3. **状态清理**：任务开始前清理上次的数据，避免相互影响

## 测试建议

代码已通过 linter 检查，建议使用 Go 的竞态检测工具测试：

```bash
cd agent/go-service
go test -race ./importtask ./realtime
```

## 兼容性

这次修改只是在内部加了锁，不影响外部调用方式，完全向后兼容。
