#include "ColorFreeze.h"

#include <algorithm>
#include <cmath>
#include <string_view>

#include <MaaAgentServer/MaaAgentServerAPI.h>
#include <MaaFramework/MaaMsg.h>
#include <MaaUtils/Logger.h>

#include "../utils.h"

namespace colorfreeze
{
namespace
{

// OpenCV 的标量颜色范围最多支持四通道，每个通道占八位以无损构造联合颜色键。
constexpr std::size_t kMaxChannels = 4;
// 一个 CV_8U 通道的位宽；最多四通道恰好装入 uint32_t，不丢失颜色组合信息。
constexpr int kChannelBits = 8;
// 初始最多为 256 种颜色预留容量，可覆盖完整的 8 位灰度；多通道颜色按需增长。
// 同时受实际命中数限制，避免为稀疏范围或大面积单色一开始就分配整幅 ROI 大小的表。
constexpr std::size_t kInitialColorCapacity = 256;

// 外部与其他识别器一样只注册原生回调；默认状态及其生命周期监听由模块内部按需创建。
Recognition* get_recognition()
{
    // AgentServer 的事件回调只借用此地址，函数返回后对象仍需存活；各任务和节点的窗口仍分别保存。
    static Recognition recognition;
    // 函数内静态初始化保证并发首次调用也只接入一次，不在每帧重复添加监听。
    // AgentClient 始终转发任务和 Context 事件，因此不要求在连接握手之前安装服务端监听。
    // 首次使用前没有窗口，未收到更早的任务开始事件也不会继承旧计时。
    static const bool ready = [] {
        if (MaaAgentServerAddTaskerSink(Recognition::OnEvent, &recognition) == MaaInvalidId
            || MaaAgentServerAddContextSink(Recognition::OnEvent, &recognition) == MaaInvalidId) {
            LogError << "Failed to initialize ColorFreezeRecognition lifecycle sinks";
            return false;
        }
        return true;
    }();
    // 清理机制不完整时禁止创建识别窗口，避免流程重入时使用已经满足的稳定时间。
    return ready ? &recognition : nullptr;
}

// 仅处理上下界的 JSON 形状；具体通道数要在执行颜色转换后才能核对。
std::optional<std::vector<std::vector<int>>> read_bounds(const json::value& value)
{
    // 将 ColorMatch 的单范围 / 多范围写法归一化，后续逐组统计，保留重叠范围各自的语义。
    if (!value.is_array() || value.as_array().empty()) {
        LogError << "ColorFreezeRecognition lower / upper must be nonempty arrays";
        return std::nullopt;
    }
    if (value.as_array()[0].is_number()) {
        // [200] 或 [R, G, B] 表示一组边界；整数组合校验会拒绝小数和混合类型。
        if (!value.is<std::vector<int>>()) {
            LogError << "ColorFreezeRecognition color bounds must contain integers";
            return std::nullopt;
        }
        return std::vector<std::vector<int>> { value.as<std::vector<int>>() };
    }
    // [[...], [...]] 表示多组边界；空子数组及上下界配对留给 ParseParameters 检查。
    if (!value.is<std::vector<std::vector<int>>>()) {
        LogError << "ColorFreezeRecognition color bounds must be integer arrays or arrays of integer arrays";
        return std::nullopt;
    }
    return value.as<std::vector<std::vector<int>>>();
}

// cv::inRange 通过四通道 Scalar 接收边界；未使用的通道保持默认零值。
// 调用前已验证有效通道数不会超过四，避免越界写入 Scalar。
cv::Scalar to_scalar(const std::vector<int>& values)
{
    cv::Scalar scalar;
    for (std::size_t i = 0; i < values.size(); ++i) {
        scalar[static_cast<int>(i)] = values[i];
    }
    return scalar;
}

// 比较单组颜色范围的分布。只关心每种颜色出现多少次，不对应两帧中的像素位置。
// 例如颜色整体从灰度 220 变成 221，联合颜色桶也全部改变，距离仍为 1。
double distance(const Distribution& baseline, const Distribution& current)
{
    const auto total = std::max(baseline.count, current.count);
    if (total == 0) {
        // 两帧在这一范围都没有命中像素，视为没有变化，同时避免归一化除零。
        // 这不直接产生识别命中：回调还必须通过原生 ColorMatch 的数量等门槛。
        return 0;
    }

    // 设重合数量 I = sum(min(Hb[c], Hc[c]))，则原距离公式中的分子等于 2 * (total - I)。
    // 因此 D = (total - I) / total，无需再遍历另一侧补算新增颜色，仍同时衡量改色和数量变化。
    // 按较小一侧遍历，每个颜色只查找一次；未命中的颜色对重合数量贡献为零。
    const auto* smaller = &baseline.colors;
    const auto* larger = &current.colors;
    if (smaller->size() > larger->size()) {
        std::swap(smaller, larger);
    }
    std::size_t overlap = 0;
    for (const auto& [color, count] : *smaller) {
        const auto it = larger->find(color);
        if (it != larger->end()) {
            overlap += std::min(count, it->second);
        }
    }
    // 先在整数域求未重合数量，再除以 total，保留原 D 的求值含义，避免先求相似度再相减的额外舍入。
    // 用较大的命中数归一化，而非 ROI 面积，避免大 ROI 中的小目标被背景稀释。
    // 等量像素中有 2% 改色时 D=0.02；同色数量从 100 增至 200 时 D=0.5。
    return static_cast<double>(total - overlap) / static_cast<double>(total);
}

} // namespace

std::optional<Parameters> ParseParameters(const std::string& text)
{
    // 必须显式传对象；null 或其他类型不能悄悄使用默认值，掩盖调用配置错误。
    auto parsed = json::parse(text);
    if (!parsed || !parsed->is_object()) {
        LogError << "ColorFreezeRecognition parameters must be a JSON object";
        return std::nullopt;
    }
    auto& object = parsed->as_object();
    // 拒绝未知字段和拼写错误，防止用户配置未生效却误以为已调整了判定条件。
    // roi / roi_offset 属于外层 Custom 参数，应由框架解析，不在这里重复接收。
    for (const auto& [key, value] : object) {
        std::ignore = value;
        if (key != "time" && key != "threshold" && key != "method" && key != "lower" && key != "upper" && key != "count"
            && key != "connected" && key != "order_by" && key != "index") {
            LogError << "unknown ColorFreezeRecognition parameter" << VAR(key);
            return std::nullopt;
        }
    }

    Parameters parameters;
    // 只接受正整数毫秒，避免小数截断以及 time=0 导致首帧直接命中。
    const auto time = parsed->find<int>("time");
    if (!time || *time <= 0) {
        LogError << "ColorFreezeRecognition time must be a positive integer in milliseconds";
        return std::nullopt;
    }
    parameters.time = std::chrono::milliseconds(*time);
    if (object.contains("threshold")) {
        // 保留缺省值 0.98，但显式配置必须合法；0 会允许完全不同的分布，故不接受。
        const auto threshold = parsed->find<double>("threshold");
        if (!threshold || !std::isfinite(*threshold) || *threshold <= 0 || *threshold > 1) {
            LogError << "ColorFreezeRecognition threshold must be in (0, 1]";
            return std::nullopt;
        }
        parameters.threshold = *threshold;
    }
    if (object.contains("method")) {
        // 此处先保证是整数代码；具体转换是否合法、输出几通道由 BuildDistributions 验证。
        const auto method = parsed->find<int>("method");
        if (!method) {
            LogError << "ColorFreezeRecognition method must be an integer OpenCV color conversion code";
            return std::nullopt;
        }
        parameters.method = *method;
    }
    if (!object.contains("lower") || !object.contains("upper")) {
        LogError << "ColorFreezeRecognition lower and upper are required";
        return std::nullopt;
    }
    const auto lower = read_bounds(object.at("lower"));
    const auto upper = read_bounds(object.at("upper"));
    if (!lower || !upper) {
        // 边界解析处已记录具体错误；不能把解析失败当成有效的空范围继续匹配。
        return std::nullopt;
    }
    // 第 i 组 lower 只能与第 i 组 upper 配对，不能靠截断或补零猜测缺失范围。
    if (lower->size() != upper->size()) {
        LogError << "ColorFreezeRecognition lower and upper must contain the same number of ranges";
        return std::nullopt;
    }
    for (std::size_t i = 0; i < lower->size(); ++i) {
        // 当前只支持完整的 8 位联合颜色，范围的上下界必须有相同的 1～4 个通道。
        if ((*lower)[i].empty() || (*lower)[i].size() > kMaxChannels || (*lower)[i].size() != (*upper)[i].size()) {
            LogError << "ColorFreezeRecognition color bounds must have matching channel counts between 1 and 4";
            return std::nullopt;
        }
        parameters.ranges.push_back({ (*lower)[i], (*upper)[i] });
    }

    // 其余字段交给原生 ColorMatch 校验及执行；显式固定 method 使统计和匹配使用同一转换。
    object.erase("time");
    object.erase("threshold");
    (*parsed)["method"] = parameters.method;
    parameters.native = std::move(*parsed);
    return parameters;
}

std::optional<std::vector<Distribution>> BuildDistributions(const cv::Mat& roi_image, const Parameters& parameters)
{
    if (roi_image.empty()) {
        LogError << "ColorFreezeRecognition ROI image is empty";
        return std::nullopt;
    }
    cv::Mat converted;
    // 整个 ROI 只转换一次，各颜色范围共享结果，保证同一帧使用一致的颜色空间。
    cv::cvtColor(roi_image, converted, parameters.method);
    if (converted.depth() != CV_8U || converted.channels() > static_cast<int>(kMaxChannels)) {
        // 颜色键按每通道八位打包，不能将浮点或更高位深的输出截断后继续比较。
        LogError << "ColorFreezeRecognition color conversion must produce 8-bit colors with at most four channels";
        return std::nullopt;
    }

    std::vector<Distribution> distributions;
    for (const auto& range : parameters.ranges) {
        if (range.lower.size() != static_cast<std::size_t>(converted.channels())) {
            // 防止用 RGB 的三通道边界解释灰度 / BGRA 等输出，造成统计与用户意图不同。
            LogError << "ColorFreezeRecognition color bounds channel count does not match method";
            return std::nullopt;
        }
        cv::Mat mask;
        // 与原生 ColorMatch 使用相同转换及上下界；范围外的颜色变化不参与稳定比较。
        cv::inRange(converted, to_scalar(range.lower), to_scalar(range.upper), mask);
        Distribution distribution;
        // 使用掩码的非零数作为命中总数，同时限制预分配；颜色键仍在下面完整逐像素统计。
        // 初始容量只给一个有界提示，不按命中像素总数假定每个像素都具有不同颜色。
        distribution.count = static_cast<std::size_t>(cv::countNonZero(mask));
        distribution.colors.reserve(std::min(distribution.count, kInitialColorCapacity));
        // 按行获取指针，不假设裁出的 ROI 内存连续；mask 与 converted 共用行列坐标。
        for (int y = 0; y < converted.rows; ++y) {
            const auto* pixels = converted.ptr<std::uint8_t>(y);
            const auto* selected = mask.ptr<std::uint8_t>(y);
            for (int x = 0; x < converted.cols; ++x) {
                if (selected[x] == 0) {
                    continue;
                }
                std::uint32_t color = 0;
                // 无损打包完整颜色，不按像素位置比较，也不以各通道均值或命中数代替分布。
                for (int channel = 0; channel < converted.channels(); ++channel) {
                    color = (color << kChannelBits) | pixels[x * converted.channels() + channel];
                }
                ++distribution.colors[color];
            }
        }
        // 即使这一组没有命中也保留空分布，确保前后两帧的范围索引始终对应。
        distributions.push_back(std::move(distribution));
    }
    return distributions;
}

double DistributionDistance(const std::vector<Distribution>& baseline, const std::vector<Distribution>& current)
{
    if (baseline.size() != current.size()) {
        // 范围数量改变后没有逐组比较的前提，以最大距离要求观察窗口重新建立。
        return 1;
    }
    double maximum = 0;
    // 取最不稳定的一组，确保每组范围都通过阈值；大范围不能掩盖小范围的变化。
    for (std::size_t i = 0; i < baseline.size(); ++i) {
        maximum = std::max(maximum, distance(baseline[i], current[i]));
    }
    return maximum;
}

Observation
    StableWindow::observe(std::vector<Distribution> current, std::chrono::milliseconds time, double threshold, Clock::time_point now)
{
    // 相似度为 1 - D；所有范围的最低相似度必须 >= threshold，等于阈值时允许继续计时。
    const double similarity = since_ ? 1 - DistributionDistance(baseline_, current) : 1;
    if (!since_ || similarity < threshold || now < *since_) {
        // 首帧只建基准；变化超限或时间倒退时从当前帧重新开始，不能沿用旧的稳定时长。
        baseline_ = std::move(current);
        since_ = now;
    }
    const auto elapsed = std::chrono::duration_cast<std::chrono::milliseconds>(now - *since_);
    // similarity 保留本次与旧基准的比较结果，重置后不改写成 1，便于日志定位变化原因。
    // 刚重置时 elapsed=0；time 已保证为正，因此当前帧只能建基准，不能立即通过。
    return { elapsed >= time, elapsed, similarity };
}

void Recognition::reset(MaaTaskId task_id, const std::string& node_name)
{
    // 与 Run / OnEvent 共用同一把锁；即使当前节点尚未建立窗口，erase 也可安全调用。
    const std::lock_guard lock(mutex_);
    states_.erase({ task_id, node_name });
}

MaaBool MAA_CALL Recognition::Run(
    MaaContext* context,
    MaaTaskId task_id,
    const char* node_name,
    const char* custom_recognition_name,
    const char* custom_recognition_param,
    const MaaImageBuffer* image,
    const MaaRect* roi,
    void* trans_arg,
    MaaRect* out_box,
    MaaStringBuffer* out_detail)
{
    std::ignore = custom_recognition_name;
    // 常规注册传 nullptr，由模块持有状态；显式传入对象时借用该对象，便于独立实例复用和测试。
    auto* recognition = static_cast<Recognition*>(trans_arg);
    if (node_name == nullptr || *node_name == '\0') {
        // 缺少节点身份时无法可靠定位窗口，只记录错误并返回未命中。
        LogError << "ColorFreezeRecognition missing node name";
        return false;
    }
    // 可预期的校验和框架调用失败直接返回；所有错误都必须清理本节点窗口，不能延续旧计时。
    // 统一处理日志与清理，避免新增早退路径时遗漏状态失效。
    const auto fail = [&](const char* reason) -> MaaBool {
        if (recognition != nullptr) {
            recognition->reset(task_id, node_name);
        }
        LogError << "ColorFreezeRecognition failed" << VAR(task_id) << VAR(node_name) << VAR(reason);
        return false;
    };
    try {
        if (recognition == nullptr) {
            recognition = get_recognition();
            if (recognition == nullptr) {
                return false;
            }
        }
        if (context == nullptr || image == nullptr || roi == nullptr || custom_recognition_param == nullptr) {
            return fail("missing context, image, ROI or parameters");
        }
        // 记录本次处理开始的时间，避免将本帧图像统计 / 原生识别的耗时计入本次观测时间。
        const auto sample_time = StableWindow::Clock::now();
        const cv::Mat pixels = to_mat(image);
        const cv::Rect region(roi->x, roi->y, roi->width, roi->height);
        // 要求实际 ROI 完整位于截图内，不静默裁边，否则比较的像素集合会与配置不一致。
        if (pixels.empty() || region.empty() || (region & cv::Rect({}, pixels.size())) != region) {
            return fail("ROI is empty or outside the current image");
        }
        auto parsed_parameters = ParseParameters(custom_recognition_param);
        if (!parsed_parameters) {
            recognition->reset(task_id, node_name);
            return false;
        }
        auto& parameters = *parsed_parameters;
        // 先在本回调的异常边界内验证转换及通道数，避免非法 OpenCV 参数进入原生匹配。
        auto distributions = BuildDistributions(pixels(region), parameters);
        if (!distributions) {
            // 解析和统计函数已记录失败原因，此处仅使旧窗口失效，避免重复打印同一错误。
            recognition->reset(task_id, node_name);
            return false;
        }
        // 传给 ColorMatch 的仍是完整截图，只用已解析 ROI 限定区域，避免二次裁图或重复偏移。
        parameters.native["roi"] = json::array { region.x, region.y, region.width, region.height };
        auto* tasker = MaaContextGetTasker(context);
        if (tasker == nullptr) {
            return fail("tasker is unavailable");
        }
        const std::string native_json = parameters.native.dumps();
        // 连通域、数量门槛、排序与索引都由原生 ColorMatch 判定，本实现只附加分布稳定条件。
        const MaaRecoId reco_id = MaaContextRunRecognitionDirect(context, "ColorMatch", native_json.c_str(), image);
        MaaBool hit = false;
        MaaRect box {};
        // 必须区分“匹配执行失败”和“成功执行但未命中”；前者记录错误，后者正常重置窗口。
        // 只读取原生门槛结果及当前框，不从连通域框重新统计分布，以免引入候选切换语义。
        if (reco_id == MaaInvalidId
            || !MaaTaskerGetRecognitionDetail(tasker, reco_id, nullptr, nullptr, &hit, &box, nullptr, nullptr, nullptr)) {
            return fail("native ColorMatch failed to run or return details");
        }
        if (!hit) {
            // 稳定窗口要求每个采样点都通过原生匹配，短暂丢失也必须重新累计完整时间。
            recognition->reset(task_id, node_name);
            return false;
        }

        // 参数、解析后的 ROI 和截图尺寸变化均需重新开始观察；绝不缓存 context / image 等借用对象。
        // 原始参数文本也参与签名；即使只是配置表示改变，也保守地开始一次新的完整观察。
        // native 中包含当前解析出的 ROI，可捕捉引用节点或锚点移动，而不只比较配置文本。
        const std::string signature =
            json::value {
                { "parameters", custom_recognition_param },
                { "native", parameters.native },
                { "width", pixels.cols },
                { "height", pixels.rows },
                { "type", pixels.type() },
            }
                .dumps();
        Observation observation;
        {
            // 图像处理与原生识别都在锁外完成；锁只保护窗口读写，避免框架事件回调等待计算。
            const std::lock_guard lock(recognition->mutex_);
            auto& state = recognition->states_[{ task_id, node_name }];
            if (state.signature != signature) {
                // 用空窗口整体替换旧状态，基准和计时必须一起失效，不能只更新签名。
                state = State { signature, {} };
            }
            observation = state.window.observe(std::move(*distributions), parameters.time, parameters.threshold, sample_time);
        }
        LogDebug << "ColorFreezeRecognition" << VAR(task_id) << VAR(node_name) << VAR(observation.similarity) << VAR(parameters.threshold)
                 << VAR(observation.elapsed.count()) << VAR(observation.stable);
        if (!observation.stable) {
            // 不在回调中等待或主动截图；让 Pipeline 的下一轮识别提供新样本。
            return false;
        }
        if (out_detail != nullptr) {
            // 只输出本轮成功观察的指标及原生结果 ID，不复制整个直方图到框架日志中。
            const std::string detail =
                json::value {
                    { "stable", true },
                    { "elapsed", observation.elapsed.count() },
                    { "similarity", observation.similarity },
                    { "threshold", parameters.threshold },
                    { "color_match_reco_id", reco_id },
                }
                    .dumps();
            if (!MaaStringBufferSet(out_detail, detail.c_str())) {
                return fail("failed to write recognition detail");
            }
        }
        if (out_box != nullptr) {
            // 仅在原生匹配与稳定条件同时满足时返回当前帧的原生框，不返回基准帧的旧框。
            *out_box = box;
        }
        return true;
    }
    catch (const std::exception& error) {
        // 只兜底第三方库或标准库抛出的异常，不用异常表达常规校验失败；异常不能穿过 C 回调边界。
        return fail(error.what());
    }
}

void MAA_CALL Recognition::OnEvent(void* handle, const char* message, const char* details_json, void* trans_arg)
{
    auto* recognition = static_cast<Recognition*>(trans_arg);
    if (message == nullptr || details_json == nullptr) {
        return;
    }
    const std::string_view event(message);
    // 识别开始 / 结束事件不会清理，否则每次采样都会丢失上一帧，窗口永远无法累计。
    // 只关注任务边界和动作开始：进入动作后，已有稳定结果不应被后续流程再次使用。
    if (event != MaaMsg_Tasker_Task_Starting && event != MaaMsg_Tasker_Task_Succeeded && event != MaaMsg_Tasker_Task_Failed
        && event != MaaMsg_Node_Action_Starting) {
        return;
    }
    try {
        // 显式绑定实例时只清理该实例；传 nullptr 的原生事件入口与识别入口共用模块状态。
        if (recognition == nullptr) {
            recognition = get_recognition();
            if (recognition == nullptr) {
                return;
            }
        }
        const auto detail = json::parse(details_json);
        // Tasker 事件从载荷取任务 ID；Context 动作事件优先使用所属 Context 的 ID。
        auto task_id = detail ? detail->find<MaaTaskId>("task_id") : std::nullopt;
        if (event == MaaMsg_Node_Action_Starting && handle != nullptr) {
            // RunAction 的子任务 ID 可与识别回调所属的 Context ID 不同，按 Context 清理才会命中对应窗口。
            const auto context_task_id = MaaContextGetTaskId(static_cast<MaaContext*>(handle));
            if (context_task_id != MaaInvalidId) {
                task_id = context_task_id;
            }
        }
        if (!task_id) {
            // 无法定位任务时不猜测，也不清空全部任务，避免影响其他正在观察的流程。
            LogWarn << "ColorFreezeRecognition event missing task_id" << VAR(message);
            return;
        }
        const std::lock_guard lock(recognition->mutex_);
        // 动作意味着流程已离开本轮观察；清理整个所属任务，避免后续重入节点复用已满的计时。
        // 任务开始/结束也清理同一份纯数据状态，不在事件之外保留 handle。
        std::erase_if(recognition->states_, [&](const auto& entry) { return entry.first.first == *task_id; });
    }
    catch (const std::exception& error) {
        // 事件接收器同样不能把异常传回 C API；载荷异常保留错误日志供排查。
        LogError << "ColorFreezeRecognition event failed" << VAR(message) << VAR(error.what());
    }
}

} // namespace colorfreeze
