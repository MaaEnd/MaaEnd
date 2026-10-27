#pragma once

#include <chrono>
#include <cstdint>
#include <map>
#include <mutex>
#include <optional>
#include <string>
#include <vector>

#include <boost/unordered/unordered_flat_map.hpp>
#include <meojson/json.hpp>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/NoWarningCV.hpp>

namespace colorfreeze
{

// ColorFreeze 在原生 ColorMatch 之外附加时间条件：当前帧先满足原生匹配，
// 再检查各颜色范围的联合分布是否相对固定基准保持稳定，持续达到 time 才命中。
// 不要求预先知道动画最终颜色，也不要求同一种颜色停留在相同像素位置。

// 沿用原生 WaitFreezes / 项目 ListComplete 的 threshold 命名；值越大，稳定判定越严格。
// 默认要求至少 98% 的颜色分布重合；1 表示完整联合颜色及命中数量必须完全一致。
inline constexpr double kDefaultThreshold = 0.98;

struct ColorRange
{
    // 顺序与 method 转换后的通道顺序一致；上下界均包含边界值。
    // 每个 ColorRange 对应一组 lower / upper，重叠范围仍独立统计、独立判断。
    std::vector<int> lower;
    std::vector<int> upper;
};

struct Parameters
{
    // 与原生 ColorMatch 一致：OpenCV 的 BGR -> RGB 转换代码。
    int method = cv::COLOR_BGR2RGB;
    // 已将单范围和多范围写法统一成列表，列表顺序用于对应各帧的分布。
    std::vector<ColorRange> ranges;
    // 必须连续通过稳定检查的时长，单位毫秒；解析时要求大于零，禁止首帧直接通过。
    std::chrono::milliseconds time {};
    // 每组分布都必须达到此相似度，范围 (0, 1]；等于阈值时仍视为稳定。
    double threshold = kDefaultThreshold;
    // 仅保存传给原生 ColorMatch 的参数，不包含本识别器的 time / threshold。
    json::value native;
};

// 解析 ColorMatch 参数及 time / threshold；无效配置记录错误并返回 nullopt。
// 此处检查包装层需要的类型和范围形状，原生匹配字段仍由 ColorMatch 校验并执行。
std::optional<Parameters> ParseParameters(const std::string& text);

struct Distribution
{
    // 使用完整的联合颜色值，不分箱、不记录像素位置；最多四个 8 位通道。
    // 各通道共同组成键，避免通道各自的直方图相同、实际颜色组合却不同的误判。
    // 扁平哈希将条目放在连续存储中，减少颜色丰富时的逐节点分配及比较时的离散访存。
    // 容器重哈希会使引用失效，因此只保存颜色值和计数，不跨插入操作保留条目地址。
    boost::unordered_flat_map<std::uint32_t, std::size_t> colors;
    // 同时保留命中总数，防止只比较归一化颜色比例而忽略目标面积变化。
    std::size_t count = 0;
};

// 对每个颜色范围分别统计整个 ROI 内的命中分布，与 connected / index 的筛选无关。
// 输入是本轮截图裁出的 ROI；每个输出元素与 Parameters::ranges 的同位置范围对应。
// 结果只拥有颜色计数，不持有输入图像，能够安全保留到后续回调中比较。
// 空图像、位深或通道不匹配时记录错误并返回 nullopt；OpenCV 异常由回调边界统一捕获。
std::optional<std::vector<Distribution>> BuildDistributions(const cv::Mat& roi_image, const Parameters& parameters);

// 返回颜色质量及命中数量的最大变化比例，范围 [0, 1]。
// 0 表示所有范围的颜色计数完全相同，1 表示至少一组完全不同；范围组数不同也返回 1。
double DistributionDistance(const std::vector<Distribution>& baseline, const std::vector<Distribution>& current);

struct Observation
{
    // 当前采样通过相似度检查，且距本轮基准建立已达到所需时长，才为 true。
    bool stable = false;
    // 本轮稳定窗口已累计的毫秒数；首次观测或本次触发重置时为零。
    std::chrono::milliseconds elapsed {};
    // 当前帧相对本次比较基准的相似度；发生重置时仍保留触发重置的观测值。
    double similarity = 1;
};

// 窗口内固定首帧为基准，只有相似度低于阈值才换基准并重新计时。
// 不逐帧推进基准，避免相邻帧变化很小的缓慢动画在累计变化很大时被提前判稳。
class StableWindow
{
public:
    // 采用单调时钟，不受系统时间校准影响；now 由调用方传入以便测试精确覆盖边界。
    using Clock = std::chrono::steady_clock;

    // 消费一份当前分布；只有建立或替换基准时才移动到成员中，其余帧保持原基准不变。
    // elapsed 表示采样点之间的持续时间；无法据此判断未采样时刻是否曾发生变化。
    Observation observe(std::vector<Distribution> current, std::chrono::milliseconds time, double threshold, Clock::time_point now);

private:
    // 当前窗口最初观测到的各范围分布，窗口有效期间不会随每一帧推进。
    std::vector<Distribution> baseline_;
    // 未赋值表示尚未收到有效样本，避免把默认时间点误当作已经开始计时。
    std::optional<Clock::time_point> since_;
};

// Recognition 拥有任务/节点隔离的纯数据状态，不保留跨回调的 MaaFramework 句柄。
class Recognition
{
public:
    // MaaFramework Custom 识别入口，每次只处理框架提供的这一帧。
    // roi 已由框架解析为实际区域；context / image 等句柄仅在本次回调内借用。
    // 常规注册时 trans_arg 传 nullptr，内部自动持有状态并接入生命周期监听；也可显式传入 Recognition 实例。
    // 未命中时不写出结果框和详情；显式实例由调用方持有，并负责接入该实例的生命周期事件。
    static MaaBool MAA_CALL
        Run(MaaContext* context,
            MaaTaskId task_id,
            const char* node_name,
            const char* custom_recognition_name,
            const char* custom_recognition_param,
            const MaaImageBuffer* image,
            const MaaRect* roi,
            void* trans_arg,
            MaaRect* out_box,
            MaaStringBuffer* out_detail);

    // 任务开始/结束或流程进入动作时清理窗口，避免再次进入同一节点时继承旧计时。
    // 同时接收 Tasker 和 Context 事件，只在动作事件中将 handle 解释为 MaaContext。
    // trans_arg 为 nullptr 时使用内部默认状态，否则只清理传入的 Recognition 实例。
    static void MAA_CALL OnEvent(void* handle, const char* message, const char* details_json, void* trans_arg);

private:
    struct State
    {
        // 参数、解析后的 ROI 或截图规格改变后，旧基准与计时不再适用。
        std::string signature;
        StableWindow window;
    };

    // 参数错误或原生未命中仅失效当前节点，不影响同一任务里其他节点的观察。
    void reset(MaaTaskId task_id, const std::string& node_name);

    std::mutex mutex_;
    // 不同任务和节点独立观察；互斥保护识别回调与生命周期事件对窗口的并发访问。
    std::map<std::pair<MaaTaskId, std::string>, State> states_;
};

} // namespace colorfreeze
