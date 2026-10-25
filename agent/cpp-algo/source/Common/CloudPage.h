#pragma once

#include <MaaFramework/MaaAPI.h>

namespace cloud
{
MaaBool MAA_CALL PageStateRun(
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
MaaBool MAA_CALL PageReloadRun(MaaContext*, MaaTaskId, const char*, const char*, const char*, MaaRecoId, const MaaRect*, void*);
} // namespace cloud
