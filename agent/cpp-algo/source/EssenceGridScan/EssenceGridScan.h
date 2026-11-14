#pragma once

#include <memory>

#include <MaaFramework/MaaAPI.h>

namespace essencegridscan
{

struct EssenceGridState;

class EssenceGrid
{
public:
    EssenceGrid();
    ~EssenceGrid();

    EssenceGrid(const EssenceGrid&) = delete;
    EssenceGrid& operator=(const EssenceGrid&) = delete;

    static MaaBool MAA_CALL advanceRecognitionRun(
        MaaContext* context,
        MaaTaskId task_id,
        const char* node_name,
        const char* custom_recognition_name,
        const char* custom_recognition_param,
        const MaaImageBuffer* image,
        const MaaRect* roi,
        void* trans_arg,
        MaaRect* out_box,
        MaaStringBuffer* out_detail);

    static MaaBool MAA_CALL pendingRecognitionRun(
        MaaContext* context,
        MaaTaskId task_id,
        const char* node_name,
        const char* custom_recognition_name,
        const char* custom_recognition_param,
        const MaaImageBuffer* image,
        const MaaRect* roi,
        void* trans_arg,
        MaaRect* out_box,
        MaaStringBuffer* out_detail);

    static MaaBool MAA_CALL resetActionRun(
        MaaContext* context,
        MaaTaskId task_id,
        const char* node_name,
        const char* custom_action_name,
        const char* custom_action_param,
        MaaRecoId reco_id,
        const MaaRect* box,
        void* trans_arg);

private:
    MaaBool advance(
        MaaContext* context,
        MaaTaskId task_id,
        const char* node_name,
        const MaaImageBuffer* image,
        MaaRect* out_box,
        MaaStringBuffer* out_detail);
    MaaBool pending(MaaRect* out_box, MaaStringBuffer* out_detail);
    MaaBool resetTracking(MaaTaskId task_id, const char* node_name);

    std::unique_ptr<EssenceGridState> state_;
};

} // namespace essencegridscan
