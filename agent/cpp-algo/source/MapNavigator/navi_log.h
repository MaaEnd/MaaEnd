#pragma once

#include <MaaUtils/Logger.h>

// 日志键取 name, 值取 value, 与 VAR 一样经一层转发展开 name 里的宏, 输出与 VAR(name) 逐字节相同
#define VAR_AS(name, value) MAA_LOG_NS::separator::none << VAR_AS_RAW(name, value) << MAA_LOG_NS::separator::space
#define VAR_AS_RAW(name, value) "[" #name "=" << (value) << "] "
