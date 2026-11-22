#pragma once
#include <MaaFramework/MaaAPI.h>

namespace essencereward
{
MaaBool MAA_CALL Recognize(
    MaaContext*,
    MaaTaskId,
    const char*,
    const char*,
    const char*,
    const MaaImageBuffer*,
    const MaaRect*,
    void*,
    MaaRect*,
    MaaStringBuffer*);
}
