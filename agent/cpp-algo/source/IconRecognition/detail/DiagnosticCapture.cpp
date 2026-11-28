#include "DiagnosticCapture.h"

#include <algorithm>
#include <bit>
#include <chrono>
#include <deque>
#include <filesystem>
#include <mutex>
#include <stdexcept>
#include <vector>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>
#include <MaaUtils/Time.hpp>

#include "../../utils.h"
#include "ForegroundTexture.h"
#include "RecognitionDiagnostics.h"

namespace iconrecognition::detail
{
namespace
{

// 使用独立保存动作节点名，文件名沿用框架错误截图的“时间戳_节点名”格式。
constexpr const char* kCaptureNode = "IconRecognitionDiagnosticScreencap";

// 色带覆盖率沿用稀有度分类的 0.8 标定；只把彩色色带作为内容证据，灰色可能来自空格背景。
constexpr double kContentRarityCoverage = 0.8;
// 去重仅缓存最近 1024 个样本特征；淘汰后允许再次保存，不会删除已保存图片。
constexpr std::size_t kRecentSignatures = 1024;
// 9x8 灰度图产生 64 位差分特征；低分辨率用于忽略轻微压缩、游标及背景动画。
constexpr int kHashWidth = 9;
constexpr int kHashHeight = 8;
// 最多容忍 4 位特征差异与 8/255 的平均颜色差；增大减少重复，也更易合并相似页面。
constexpr int kHashDistance = 4;
constexpr double kMeanColorDistance = 8.0;

struct Signature
{
    GridType grid_type;
    cv::Rect roi;
    std::string reason;
    std::uint64_t hash = 0;
    cv::Scalar mean;
};

Signature make_signature(const cv::Mat& image, const cv::Rect& roi, GridType grid_type, const std::string& reason)
{
    cv::Mat small;
    cv::resize(image(roi), small, cv::Size(kHashWidth, kHashHeight), 0, 0, cv::INTER_AREA);
    Signature signature { .grid_type = grid_type, .roi = roi, .reason = reason, .mean = cv::mean(small) };
    cv::Mat gray;
    if (small.channels() == 4) {
        cv::cvtColor(small, gray, cv::COLOR_BGRA2GRAY);
    }
    else if (small.channels() == 3) {
        cv::cvtColor(small, gray, cv::COLOR_BGR2GRAY);
    }
    else {
        gray = small;
    }
    for (int row = 0; row < kHashHeight; ++row) {
        for (int column = 0; column < kHashWidth - 1; ++column) {
            signature.hash =
                (signature.hash << 1) | static_cast<std::uint64_t>(gray.at<uchar>(row, column) > gray.at<uchar>(row, column + 1));
        }
    }
    return signature;
}

bool same_page(const Signature& left, const Signature& right)
{
    if (left.grid_type != right.grid_type || left.roi != right.roi || left.reason != right.reason
        || std::popcount(left.hash ^ right.hash) > kHashDistance) {
        return false;
    }
    // Alpha 不承载物品内容；BGRA 与 BGR 的颜色差按前三个通道比较。
    for (int channel = 0; channel < 3; ++channel) {
        if (std::abs(left.mean[channel] - right.mean[channel]) > kMeanColorDistance) {
            return false;
        }
    }
    return true;
}

struct CaptureDecision
{
    std::string reason;
    std::vector<cv::Rect> suspicious_cells;
};

struct CaptureJob
{
    cv::Size source_size;
    Signature signature;
    RecognitionRequest request;
    RecognitionResult result;
    CaptureDecision decision;
    std::string node_name;
    std::string controller_type;
    std::uint64_t task_id = 0;
    std::int64_t captured_at_ms = 0;
};

std::filesystem::path save_screencap(MaaContext* context, const std::string& stem)
{
    MaaTasker* tasker = MaaContextGetTasker(context);
    ScopedStringBuffer name;
    ScopedStringBuffer action;
    ScopedStringBuffer detail;
    if (tasker == nullptr || name.Get() == nullptr || action.Get() == nullptr || detail.Get() == nullptr) {
        throw std::runtime_error("Unable to prepare diagnostic Screencap");
    }
    const json::value override = json::object {
        { kCaptureNode,
          json::object {
              { "action",
                json::object {
                    { "type", "Screencap" },
                    { "param", json::object { { "filename", "../on_error/" + stem }, { "format", "png" } } },
                } },
              // 保存紧接本次识别执行；禁用画面静止等待，避免为保存图片刷新缓存。
              { "pre_delay", 0 },
              { "post_delay", 0 },
              { "pre_wait_freezes", 0 },
              { "post_wait_freezes", 0 },
              { "repeat", 1 },
          } },
    };
    const MaaRect box {};
    const auto action_id = MaaContextRunAction(context, kCaptureNode, override.to_string().c_str(), &box, "");
    MaaBool success = false;
    MaaRect action_box {};
    if (action_id == MaaInvalidId
        || !MaaTaskerGetActionDetail(tasker, action_id, name.Get(), action.Get(), &action_box, &success, detail.Get()) || !success) {
        throw std::runtime_error("Diagnostic Screencap failed");
    }
    const char* text = MaaStringBufferGet(detail.Get());
    const auto parsed = text == nullptr ? std::nullopt : json::parse(text);
    const auto filepath = parsed ? parsed->find<std::string>("filepath") : std::nullopt;
    if (!filepath || filepath->empty()) {
        throw std::runtime_error("Diagnostic Screencap did not return a file path");
    }
    return MAA_NS::path(*filepath);
}

bool save_capture(MaaContext* context, const CaptureJob& job)
{
    const auto stem = MAA_NS::format_now_for_filename() + "_" + kCaptureNode;
    const auto image_path = save_screencap(context, stem);
    try {
        json::array suspicious;
        for (const auto& box : job.decision.suspicious_cells) {
            suspicious.emplace_back(RectToJson(box));
        }
        const auto& candidates = job.request.candidates;
        json::object metadata {
            { "image", MAA_NS::path_to_utf8_string(image_path.filename()) },
            { "image_scope", "controller_cache" },
            { "reason", job.decision.reason },
            { "grid_type", GridTypeName(job.request.grid_type) },
            { "controller_type", job.controller_type },
            { "task_id", std::to_string(job.task_id) },
            { "node_name", job.node_name },
            { "captured_at_ms", job.captured_at_ms },
            { "source_roi", RectToJson(job.signature.roi) },
            { "source_size", json::array { job.source_size.width, job.source_size.height } },
            { "suspicious_cells", std::move(suspicious) },
            { "result", job.result },
            { "request",
              json::object {
                  { "item_ids", candidates.item_ids },
                  { "item_filters", candidates.item_filters },
                  { "additional_item_filters", candidates.additional_item_filters },
                  { "excluded_item_ids", candidates.excluded_item_ids },
                  { "item_recheck_filters", candidates.item_recheck_filters },
                  { "threshold", job.request.threshold },
                  { "subpixel_threshold", job.request.subpixel_threshold },
                  { "deduplicate", job.request.deduplicate },
                  { "recognize_region_unavailable", job.request.recognize_region_unavailable },
              } },
        };
        if (job.result.diagnostics) {
            metadata["diagnostics"] = *job.result.diagnostics;
        }
        const json::value diagnostic(std::move(metadata));
        LogInfo << "IconRecognition diagnostic capture" << VAR(image_path) << VAR(diagnostic);
    }
    catch (...) {
        LogWarn << "IconRecognition diagnostic logging failed; PNG retained" << VAR(image_path);
    }
    // 图片已经保存成功，日志失败不应导致同一页面重复采图。
    return true;
}

// 零命中只作为诊断样本；正常翻页搜索仍按原识别结果继续。
CaptureDecision evaluate_capture(const RecognitionRequest& request, const RecognitionResult& result)
{
    if (result.error_code == "grid_detection_failed" && request.grid_type != GridType::SingleRoi) {
        return { .reason = "grid_detection_failed" };
    }
    if (!result.diagnostics) {
        return {};
    }
    if (result.error_code == "no_match" && !result.diagnostics->cells.empty()) {
        CaptureDecision decision { .reason = "no_match" };
        // 单候选和多候选都保留最终零命中的画面，包括反查后没有结果的情况。
        for (const auto& cell : result.diagnostics->cells) {
            decision.suspicious_cells.push_back(cell.cell_box);
        }
        return decision;
    }
    if (!result.error_code.empty() || !result.diagnostics->candidate_scope_complete) {
        return {};
    }
    CaptureDecision decision;
    for (const auto& cell : result.diagnostics->cells) {
        if (!cell.rejected_reason || *cell.rejected_reason == "low-foreground-texture") {
            continue;
        }
        // 滚动区底部的半行也有纹理；同一几何行存在正常匹配时，纹理才可单独作为漏项证据。
        const bool row_observed = std::ranges::any_of(result.diagnostics->cells, [&](const auto& other) {
            return !other.rejected_reason && !other.template_matching_skipped && other.cell_box.y == cell.cell_box.y
                   && other.cell_box.height == cell.cell_box.height;
        });
        const bool textured = row_observed && cell.foreground_texture && *cell.foreground_texture >= kDefaultLowTextureThreshold;
        const bool chromatic_strip = cell.rarity && *cell.rarity > 1 && cell.rarity_coverage >= kContentRarityCoverage;
        if (textured || chromatic_strip || request.grid_type == GridType::SingleRoi) {
            decision.suspicious_cells.push_back(cell.cell_box);
            // 保留最终匹配拒绝原因；多个原因的格子仍保存在同一个样本的完整诊断中。
            if (decision.reason.empty()) {
                decision.reason = *cell.rejected_reason;
            }
        }
    }
    return decision;
}

} // namespace

class DiagnosticCapture::Impl
{
public:
    void submit(
        MaaContext* context,
        const cv::Mat& image,
        const RecognitionRequest& request,
        const RecognitionResult& result,
        std::uint64_t task_id,
        const std::string& node_name,
        const std::string& controller_type)
    {
        if (context == nullptr || image.empty() || image.depth() != CV_8U) {
            return;
        }
        auto decision = evaluate_capture(request, result);
        if (decision.reason.empty()) {
            return;
        }
        const auto roi = request.roi & cv::Rect(0, 0, image.cols, image.rows);
        if (roi.empty()) {
            return;
        }
        auto signature = make_signature(image, roi, request.grid_type, decision.reason);
        std::unique_lock lock(mutex_);
        if (std::ranges::any_of(recent_, [&](const auto& prior) { return same_page(prior, signature); })) {
            return;
        }
        const auto captured =
            std::chrono::duration_cast<std::chrono::milliseconds>(std::chrono::system_clock::now().time_since_epoch()).count();
        const CaptureJob job {
            .source_size = image.size(),
            .signature = signature,
            .request = request,
            .result = result,
            .decision = std::move(decision),
            .node_name = node_name,
            .controller_type = controller_type,
            .task_id = task_id,
            .captured_at_ms = captured,
        };
        recent_.push_back(signature);
        if (recent_.size() > kRecentSignatures) {
            recent_.pop_front();
        }
        lock.unlock();
        bool saved = false;
        try {
            // 框架句柄仅在本次回调内有效；同步执行保存，不持去重锁。
            saved = save_capture(context, job);
        }
        catch (const std::exception& error) {
            LogWarn << "IconRecognition diagnostic capture failed" << VAR(error.what());
        }
        catch (...) {
            LogWarn << "IconRecognition diagnostic capture failed";
        }
        if (!saved) {
            lock.lock();
            // 保存失败不永久吞掉同一页面；后续识别可再次尝试。
            std::erase_if(recent_, [&](const auto& prior) { return same_page(prior, signature); });
        }
    }

private:
    std::mutex mutex_;
    std::deque<Signature> recent_;
};

DiagnosticCapture::DiagnosticCapture()
    : impl_(std::make_unique<Impl>())
{
}

DiagnosticCapture::~DiagnosticCapture() = default;

void DiagnosticCapture::submit(
    MaaContext* context,
    const cv::Mat& image,
    const RecognitionRequest& request,
    const RecognitionResult& result,
    std::uint64_t task_id,
    const std::string& node_name,
    const std::string& controller_type) noexcept
{
    try {
        impl_->submit(context, image, request, result, task_id, node_name, controller_type);
    }
    catch (const std::exception& error) {
        LogWarn << "IconRecognition diagnostic submission failed" << VAR(error.what());
    }
    catch (...) {
        LogWarn << "IconRecognition diagnostic submission failed";
    }
}

DiagnosticCapture& GetDiagnosticCapture()
{
    static DiagnosticCapture capture;
    return capture;
}

} // namespace iconrecognition::detail
