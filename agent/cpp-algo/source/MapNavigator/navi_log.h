#pragma once

#include <MaaUtils/Logger.h>

// 输出与 VAR(name) 逐字节相同, 包括 name 里宏的展开
#define VAR_AS(name, value) MAA_LOG_NS::separator::none << VAR_AS_RAW(name, value) << MAA_LOG_NS::separator::space
#define VAR_AS_RAW(name, value) "[" #name "=" << (value) << "] "
