#pragma once

#include <array>
#include <chrono>
#include <cstdint>
#include <map>
#include <mutex>
#include <optional>
#include <string>
#include <tuple>
#include <vector>

#include <meojson/json.hpp>

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/NoWarningCV.hpp>

namespace colorfreeze
{

// ColorFreeze 在原生 ColorMatch 之外附加时间条件：当前帧先满足原生匹配，
// 再检查各颜色范围的联合分布是否相对固定基准保持稳定，持续达到 time 才命中。
// 不要求预先知道动画最终颜色，也不要求同一种颜色停留在相同像素位置。

// 颜色幅度按各通道最大有效距离归一化；默认允许 2% 偏差，1 要求颜色值严格一致。
inline constexpr double kDefaultColorThreshold = 0.98;
// 默认要求至少 98% 的命中计数能匹配；1 不允许任何计数未匹配，仍受颜色幅度阈值约束。
inline constexpr double kDefaultDistributionThreshold = 0.98;

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
    // 每个通道的颜色值相似度都必须达到此值，范围 [0, 1]；HSV/HLS 色相使用环形距离。
    double color_threshold = kDefaultColorThreshold;
    // 每组分布都必须达到此匹配比例，范围 (0, 1]；等于阈值时仍视为稳定。
    double distribution_threshold = kDefaultDistributionThreshold;
    // 仅保存传给原生 ColorMatch 的参数，不包含本识别器的 time 与两个稳定阈值。
    json::value native;
};

// 解析 ColorMatch 参数及 time / color_threshold / distribution_threshold；无效配置记录错误并返回 nullopt。
// 此处检查包装层需要的类型和范围形状，原生匹配字段仍由 ColorMatch 校验并执行。
std::optional<Parameters> ParseParameters(const std::string& text);

struct ColorBin
{
    std::uint32_t key;
    // 颜色值本身只有 8 位；紧凑保存可降低全屏排序和匹配时的缓存带宽，计算差值时再提升为 int。
    std::array<std::uint8_t, 4> color;
    std::size_t count;
};

struct Distribution
{
    // 使用完整的联合颜色值，不分箱、不记录像素位置；最多四个 8 位通道。
    // 各通道共同组成键，避免通道各自的直方图相同、实际颜色组合却不同的误判。
    // 按完整颜色键递增保存计数；紧凑键基数排序后合并，不需要哈希表或重复排序。
    // 分布建成后保持不可变，固定基准在后续帧中直接复用。
    std::vector<ColorBin> colors;
    // 同时保留命中总数，防止只比较归一化颜色比例而忽略目标面积变化。
    std::size_t count = 0;
};

struct ColorTolerance
{
    int linear = 0;
    int hue = 0;
    int hue_period = 0;
};

// 所有快速路径和联合匹配共享同一整数容差，保证浮点边界和 HSV/HLS 环绕语义一致。
ColorTolerance GetColorTolerance(const Parameters& parameters);

// 保留先算未匹配比例、再求相似度的求值顺序，避免阈值边界因浮点舍入改变。
inline double CountSimilarity(std::size_t matched, std::size_t total)
{
    return total == 0 ? 1 : 1 - static_cast<double>(total - matched) / static_cast<double>(total);
}

struct SimilarityBounds
{
    // 真正的最大匹配相似度一定落在闭区间内；仅以可证明的下界通过、可证明的上界排除。
    double lower = 0;
    double upper = 1;
};

// 统计仍只针对范围内的像素；一维投影只用于给出联合匹配的上界，不能据此认定多通道稳定。
using ChannelHistograms = std::array<std::array<std::size_t, 256>, 4>;

struct RangeSample
{
    cv::Mat mask;
    std::size_t count = 0;
    std::optional<ChannelHistograms> channels;
    std::optional<Distribution> distribution;
};

struct FrameSample
{
    // cvtColor 创建的自有缓冲区，不借用框架截图；所有范围共享同一转换结果。
    // 先保留颜色图和范围掩码，只有快速证明不足时才生成直方图及匹配索引。
    cv::Mat colors;
    std::vector<RangeSample> ranges;
};

// 仅转换颜色、建立范围掩码和命中数；非法输入返回 nullopt，库异常由 C 回调边界捕获。
std::optional<FrameSample> PrepareSample(const cv::Mat& roi_image, const Parameters& parameters);

// 按需统计完整联合颜色，并缓存在该样本中；每个范围仍独立覆盖整个 ROI。
const Distribution& BuildDistribution(FrameSample& sample, std::size_t range);

// 仅构造一份合法配对，不求最大值；适合为位置见证剩下的少量计数补充通过下界。
// 未达到 required 不表示失败，调用方仍需使用完整分布给出最终判定。
std::size_t FindCompatiblePairs(
    const Distribution& baseline,
    const Distribution& current,
    const Parameters& parameters,
    std::size_t channels,
    std::size_t required);

// 联合匹配达到 distribution_threshold 即可结束；每份计数只用一次，必要时沿反向边重新分配。
// 返回的区间一定足以确定是否达标，无需为日志继续计算精确最大匹配比例。
SimilarityBounds
    CompareDistributions(const Distribution& baseline, const Distribution& current, const Parameters& parameters, std::size_t channels);

// 先按命中数、合法位置配对及通道投影给出上下界，仍不确定时才进入联合颜色匹配。
// 位置配对只提供下界，不足时不判失败，因此颜色位置交换仍遵循分布语义。
SimilarityBounds CompareSamples(FrameSample& baseline, FrameSample& current, const Parameters& parameters);

struct Observation
{
    // 当前采样通过相似度检查，且距本轮基准建立已达到所需时长，才为 true。
    bool stable = false;
    // 本轮稳定窗口已累计的毫秒数；首次观测或本次触发重置时为零。
    std::chrono::milliseconds elapsed {};
    // 当前帧相对本次比较基准的相似度上下界；重置后仍保留触发重置的观测值。
    SimilarityBounds similarity { 1, 1 };
};

// 窗口内固定首帧为基准，只有相似度低于阈值才换基准并重新计时。
// 不逐帧推进基准，避免相邻帧变化很小的缓慢动画在累计变化很大时被提前判稳。
class StableWindow
{
public:
    // 采用单调时钟，不受系统时间校准影响；now 由调用方传入以便测试精确覆盖边界。
    using Clock = std::chrono::steady_clock;

    // 消费一份当前样本；只有建立或替换基准时才移动到成员中，其余帧保持原基准不变。
    // elapsed 表示采样点之间的持续时间；无法据此判断未采样时刻是否曾发生变化。
    Observation observe(FrameSample current, const Parameters& parameters, Clock::time_point now);

private:
    // 当前窗口最初的颜色图及按需统计缓存；窗口内不推进基准，重置时一起替换。
    std::optional<FrameSample> baseline_;
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

    // 任务、Pipeline 观察阶段的边界或动作开始时清理窗口，避免超时重入时继承旧计时。
    // 识别事件只维护调用范围，不清理窗口；Context 事件的 handle 仅在本次回调中借用。
    // trans_arg 为 nullptr 时使用内部默认状态，否则只清理传入的 Recognition 实例。
    static void MAA_CALL OnEvent(void* handle, const char* message, const char* details_json, void* trans_arg);

private:
    // 同一任务中，外层识别路径、子识别名称及同名调用的序号共同标识观察者。
    // And / Or 的匿名 Custom 都叫 "Custom"，不能只靠名称，也不能把可变配置当成身份。
    using StateKey = std::tuple<MaaTaskId, std::vector<std::string>, std::string, std::size_t>;

    struct RecognitionScope
    {
        MaaRecoId reco_id;
        std::string name;
        // 每轮外层识别重新计数；同一 And 内两个匿名子识别各有窗口，不会交替重置彼此。
        std::map<std::string, std::size_t> occurrences;
    };

    struct State
    {
        // 参数、解析后的 ROI 或截图规格改变后，旧基准与计时不再适用。
        std::string signature;
        StableWindow window;
    };

    // 参数错误或原生未命中仅失效当前观察者，不影响同名的其他子识别。
    void reset(const StateKey& key);

    std::mutex mutex_;
    // 互斥保护窗口和识别调用栈；只保存名称、ID 和序号，不跨回调保存 Context 句柄。
    std::map<StateKey, State> states_;
    std::map<MaaTaskId, std::vector<RecognitionScope>> scopes_;
};

} // namespace colorfreeze
