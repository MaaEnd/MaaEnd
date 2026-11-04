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
        if (key != "time" && key != "color_threshold" && key != "distribution_threshold" && key != "method" && key != "lower"
            && key != "upper" && key != "count" && key != "connected" && key != "order_by" && key != "index") {
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
    if (object.contains("color_threshold")) {
        // 幅度阈值允许为零，此时不限制颜色差；原生范围、数量门槛及分布匹配比例仍然生效。
        const auto threshold = parsed->find<double>("color_threshold");
        if (!threshold || !std::isfinite(*threshold) || *threshold < 0 || *threshold > 1) {
            LogError << "ColorFreezeRecognition color_threshold must be in [0, 1]";
            return std::nullopt;
        }
        parameters.color_threshold = *threshold;
    }
    if (object.contains("distribution_threshold")) {
        // 分布阈值不接受零，避免允许完全没有匹配计数的两帧继续累计稳定时间。
        const auto threshold = parsed->find<double>("distribution_threshold");
        if (!threshold || !std::isfinite(*threshold) || *threshold <= 0 || *threshold > 1) {
            LogError << "ColorFreezeRecognition distribution_threshold must be in (0, 1]";
            return std::nullopt;
        }
        parameters.distribution_threshold = *threshold;
    }
    if (object.contains("method")) {
        // 此处先保证是整数代码；具体转换是否合法、输出几通道由 PrepareSample 验证。
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
    object.erase("color_threshold");
    object.erase("distribution_threshold");
    (*parsed)["method"] = parameters.method;
    parameters.native = std::move(*parsed);
    return parameters;
}

std::optional<FrameSample> PrepareSample(const cv::Mat& roi_image, const Parameters& parameters)
{
    if (roi_image.empty()) {
        LogError << "ColorFreezeRecognition ROI image is empty";
        return std::nullopt;
    }
    FrameSample sample;
    // 转换结果拥有独立缓冲区，可安全作为基准跨回调保存，不持有框架借用句柄。
    cv::cvtColor(roi_image, sample.colors, parameters.method);
    if (sample.colors.depth() != CV_8U || sample.colors.channels() > static_cast<int>(kMaxChannels)) {
        LogError << "ColorFreezeRecognition color conversion must produce 8-bit colors with at most four channels";
        return std::nullopt;
    }
    for (const auto& range : parameters.ranges) {
        if (range.lower.size() != static_cast<std::size_t>(sample.colors.channels())) {
            LogError << "ColorFreezeRecognition color bounds channel count does not match method";
            return std::nullopt;
        }
        RangeSample selected;
        cv::inRange(sample.colors, to_scalar(range.lower), to_scalar(range.upper), selected.mask);
        selected.count = static_cast<std::size_t>(cv::countNonZero(selected.mask));
        sample.ranges.push_back(std::move(selected));
    }
    return sample;
}

Observation StableWindow::observe(FrameSample current, const Parameters& parameters, Clock::time_point now)
{
    // 比较器仅在下界足以通过、或上界足以排除时结束，不用近似分数猜测是否稳定。
    const auto similarity = since_ ? CompareSamples(*baseline_, current, parameters) : SimilarityBounds { 1, 1 };
    if (!since_ || similarity.lower < parameters.distribution_threshold || now < *since_) {
        // 首帧、确定超限或时间倒退时重新建立固定基准，缓存和计时一起替换。
        baseline_ = std::move(current);
        since_ = now;
    }
    const auto elapsed = std::chrono::duration_cast<std::chrono::milliseconds>(now - *since_);
    return { elapsed >= parameters.time, elapsed, similarity };
}

void Recognition::reset(const StateKey& key)
{
    // 与 Run / OnEvent 共用同一把锁；即使当前节点尚未建立窗口，erase 也可安全调用。
    const std::lock_guard lock(mutex_);
    states_.erase(key);
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
    std::optional<StateKey> state_key;
    // 可预期的校验和框架调用失败直接返回；所有错误都必须清理本节点窗口，不能延续旧计时。
    // 统一处理日志与清理，避免新增早退路径时遗漏状态失效。
    const auto fail = [&](const char* reason) -> MaaBool {
        if (recognition != nullptr && state_key) {
            recognition->reset(*state_key);
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
        {
            const std::lock_guard lock(recognition->mutex_);
            // 必须在运行内部 ColorMatch 前确定身份；它会产生一组嵌套识别事件，暂时改变调用栈。
            // 默认实例首次按需接入时可能错过本轮 Starting，独立命名节点以自身名称作为安全回退。
            std::vector<std::string> path { node_name };
            std::size_t occurrence = 0;
            const auto scopes = recognition->scopes_.find(task_id);
            if (scopes != recognition->scopes_.end() && !scopes->second.empty()) {
                path.clear();
                for (const auto& scope : scopes->second) {
                    path.push_back(scope.name);
                }
                occurrence = scopes->second.back().occurrences[node_name]++;
            }
            state_key.emplace(task_id, std::move(path), node_name, occurrence);
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
            recognition->reset(*state_key);
            return false;
        }
        auto& parameters = *parsed_parameters;
        // 先在本回调的异常边界内验证转换及通道数，避免非法 OpenCV 参数进入原生匹配。
        auto sample = PrepareSample(pixels(region), parameters);
        if (!sample) {
            // 解析和统计函数已记录失败原因，此处仅使旧窗口失效，避免重复打印同一错误。
            recognition->reset(*state_key);
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
            recognition->reset(*state_key);
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
            // 颜色转换和原生识别在锁外完成；窗口比较及按需缓存与生命周期清理共用锁，保持基准一致。
            const std::lock_guard lock(recognition->mutex_);
            auto& state = recognition->states_[*state_key];
            if (state.signature != signature) {
                // 用空窗口整体替换旧状态，基准和计时必须一起失效，不能只更新签名。
                state = State { signature, {} };
            }
            observation = state.window.observe(std::move(*sample), parameters, sample_time);
        }
        LogDebug << "ColorFreezeRecognition" << VAR(task_id) << VAR(node_name) << VAR(observation.similarity.lower)
                 << VAR(observation.similarity.upper) << VAR(parameters.color_threshold) << VAR(parameters.distribution_threshold)
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
                    { "similarity_lower", observation.similarity.lower },
                    { "similarity_upper", observation.similarity.upper },
                    { "color_threshold", parameters.color_threshold },
                    { "distribution_threshold", parameters.distribution_threshold },
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
    const bool task_boundary =
        event == MaaMsg_Tasker_Task_Starting || event == MaaMsg_Tasker_Task_Succeeded || event == MaaMsg_Tasker_Task_Failed;
    const bool pipeline_boundary = event == MaaMsg_Node_PipelineNode_Starting || event == MaaMsg_Node_PipelineNode_Succeeded
                                   || event == MaaMsg_Node_PipelineNode_Failed;
    const bool recognition_start = event == MaaMsg_Node_Recognition_Starting;
    const bool recognition_end = event == MaaMsg_Node_Recognition_Succeeded || event == MaaMsg_Node_Recognition_Failed;
    // PipelineNode 包围整轮截图重试，Recognition 则每帧触发；不能在每帧边界清空稳定窗口。
    if (!task_boundary && !pipeline_boundary && !recognition_start && !recognition_end && event != MaaMsg_Node_Action_Starting) {
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
        // Tasker 事件从载荷取任务 ID；所有 Context 事件都使用所属 Context 的 ID。
        auto task_id = detail ? detail->find<MaaTaskId>("task_id") : std::nullopt;
        if (!task_boundary && handle != nullptr) {
            // RunTask / RunRecognition / RunAction 的载荷可能是子任务 ID，识别回调使用的却是根 Context ID。
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
        if (recognition_start || recognition_end) {
            const auto reco_id = detail ? detail->find<MaaRecoId>("reco_id") : std::nullopt;
            const auto name = detail ? detail->find<std::string>("name") : std::nullopt;
            if (!reco_id || !name) {
                LogWarn << "ColorFreezeRecognition event missing recognition identity" << VAR(message);
                return;
            }
            if (recognition_start) {
                recognition->scopes_[*task_id].push_back({ *reco_id, *name, {} });
            }
            else if (auto scopes = recognition->scopes_.find(*task_id); scopes != recognition->scopes_.end()) {
                // ID 配对恢复外层范围；不能简单清空，否则内部 ColorMatch 返回后会丢掉外层的子识别序号。
                auto& stack = scopes->second;
                const auto found = std::ranges::find(stack, *reco_id, &RecognitionScope::reco_id);
                if (found != stack.end()) {
                    stack.erase(found, stack.end());
                }
                if (stack.empty()) {
                    recognition->scopes_.erase(scopes);
                }
            }
            return;
        }
        // Pipeline 等待超时也没有动作，但本轮观察已经结束；开始/结束两端均清理，防止无动作重入。
        std::erase_if(recognition->states_, [&](const auto& entry) { return std::get<0>(entry.first) == *task_id; });
        if (task_boundary) {
            recognition->scopes_.erase(*task_id);
        }
    }
    catch (const std::exception& error) {
        // 事件接收器同样不能把异常传回 C API；载荷异常保留错误日志供排查。
        LogError << "ColorFreezeRecognition event failed" << VAR(message) << VAR(error.what());
    }
}

} // namespace colorfreeze
