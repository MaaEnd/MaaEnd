package essence

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// essLog 是 essence 模块的子日志器，自动携带 module=essence 字段。
// 所有 essence 包内的日志统一使用此 logger，无需每条消息手动加 "essence:" 前缀。
//
// essLog is the sub-logger for the essence module, with module=essence field.
// All logs in the essence package use this logger instead of manually prefixing "essence:".
var essLog zerolog.Logger = log.With().Str("module", "essence").Logger()
