#include "DebugCapture.h"

#include <algorithm>
#include <atomic>
#include <chrono>
#include <fstream>

#include <meojson/json.hpp>

#include "RecognitionDiagnostics.h"

namespace iconrecognition::detail
{
namespace
{

constexpr std::size_t kMaxCaptureGroups = 20;

std::string Stamp(std::uint64_t reco_id)
{
    static std::atomic<std::uint64_t> sequence = 0;
    const auto now = std::chrono::system_clock::now();
    const auto milliseconds = std::chrono::duration_cast<std::chrono::milliseconds>(now.time_since_epoch()).count();
    return std::to_string(milliseconds) + "_reco-" + std::to_string(reco_id) + "_" + std::to_string(sequence.fetch_add(1));
}

void RemoveGroup(
    const std::filesystem::path& raw_dir,
    const std::filesystem::path& annotated_dir,
    const std::filesystem::path& detail_dir,
    const std::string& stem)
{
    std::error_code ec;
    std::filesystem::remove(raw_dir / (stem + ".png"), ec);
    ec.clear();
    std::filesystem::remove(annotated_dir / (stem + ".png"), ec);
    ec.clear();
    std::filesystem::remove(detail_dir / (stem + ".json"), ec);
}

void TrimGroups(const std::filesystem::path& raw_dir, const std::filesystem::path& annotated_dir, const std::filesystem::path& detail_dir)
{
    std::vector<std::filesystem::directory_entry> groups;
    for (const auto& entry : std::filesystem::directory_iterator(raw_dir)) {
        if (entry.is_regular_file() && entry.path().extension() == ".png") {
            groups.push_back(entry);
        }
    }
    std::ranges::sort(groups, [](const auto& left, const auto& right) {
        if (left.last_write_time() != right.last_write_time()) {
            return left.last_write_time() < right.last_write_time();
        }
        return left.path().filename() < right.path().filename();
    });
    while (groups.size() > kMaxCaptureGroups) {
        RemoveGroup(raw_dir, annotated_dir, detail_dir, groups.front().path().stem().string());
        groups.erase(groups.begin());
    }
}

void DrawDiagnostics(cv::Mat& annotated, const RecognitionResult& result)
{
    cv::rectangle(annotated, result.roi, cv::Scalar(0, 255, 255), 2);
    if (!result.diagnostics) {
        return;
    }
    for (const auto& cell : result.diagnostics->cells) {
        const bool accepted = !cell.rejected_reason.has_value();
        const cv::Scalar color = accepted ? cv::Scalar(0, 255, 0) : cv::Scalar(255, 128, 0);
        cv::rectangle(annotated, cell.cell_box, color, 2);
        if (cell.candidate_box.area() > 0) {
            cv::rectangle(annotated, cell.candidate_box, cv::Scalar(255, 128, 0), 1);
        }
        std::string label = cell.best_candidate_id + " " + std::to_string(cell.score);
        if (cell.row && cell.column) {
            label += " r" + std::to_string(*cell.row) + "c" + std::to_string(*cell.column);
        }
        if (!cell.mask_kind.empty()) {
            label += " " + cell.mask_kind;
        }
        cv::putText(
            annotated,
            label,
            cell.cell_box.tl() + cv::Point(0, 14),
            cv::FONT_HERSHEY_SIMPLEX,
            0.35,
            cv::Scalar(255, 255, 255),
            1,
            cv::LINE_AA);
    }
}

} // namespace

void SaveDebugCapture(const std::filesystem::path& root, const cv::Mat& image, const RecognitionResult& result, std::uint64_t reco_id)
{
    if (image.empty()) {
        return;
    }
    const auto raw_dir = root / "raw";
    const auto annotated_dir = root / "annotated";
    const auto detail_dir = root / "detail";
    std::filesystem::create_directories(raw_dir);
    std::filesystem::create_directories(annotated_dir);
    std::filesystem::create_directories(detail_dir);
    const std::string stamp = Stamp(reco_id);
    const cv::Mat raw = image.clone();
    cv::Mat annotated = image.clone();
    DrawDiagnostics(annotated, result);
    cv::imwrite((raw_dir / (stamp + ".png")).string(), raw);
    cv::imwrite((annotated_dir / (stamp + ".png")).string(), annotated);
    json::object detail = json::value(result).as_object();
    if (result.diagnostics) {
        detail["diagnostics"] = *result.diagnostics;
    }
    std::ofstream stream(detail_dir / (stamp + ".json"), std::ios::binary);
    stream << json::value(std::move(detail)).dumps(4);
    stream.close();
    TrimGroups(raw_dir, annotated_dir, detail_dir);
}

} // namespace iconrecognition::detail
