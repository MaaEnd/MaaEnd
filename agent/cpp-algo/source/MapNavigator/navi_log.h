#pragma once

#include <MaaUtils/Logger.h>

// 日志键取 name, 值取 value, 输出与 VAR(name) 逐字节相同
#define VAR_AS(name, value) MAA_LOG_NS::separator::none << "[" #name "=" << (value) << "] " << MAA_LOG_NS::separator::space
