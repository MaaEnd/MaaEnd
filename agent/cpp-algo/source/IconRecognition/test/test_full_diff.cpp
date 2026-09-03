#define NOMINMAX
#include <windows.h>

#include <chrono>
#include <cstddef>
#include <cstring>
#include <filesystem>
#include <fstream>
#include <iomanip>
#include <iostream>
#include <map>
#include <optional>
#include <set>
#include <sstream>
#include <stdexcept>
#include <string>
#include <thread>
#include <vector>

#include <MaaUtils/NoWarningCV.hpp>
#include <meojson/json.hpp>

#include "../IconRecognizer.h"
#include "../detail/RecognitionDiagnostics.h"
#include "../detail/TemplateCatalog.h"
#include "Common/JsoncFile.h"
#include "expected_results.h"
#include "manual_runner_config.h"
#include "manual_runner_executor.h"

namespace
{

using RunnerClock = std::chrono::steady_clock;

double ElapsedMilliseconds(RunnerClock::time_point started_at)
{
    return std::chrono::duration<double, std::milli>(RunnerClock::now() - started_at).count();
}

struct RunnerCasePerformance
{
    double total_ms = 0.0;
    double image_decode_ms = 0.0;
    double recognition_ms = 0.0;
    double annotation_ms = 0.0;
    double annotated_write_ms = 0.0;
    double detail_write_ms = 0.0;

    json::value to_json() const
    {
        return json::object {
            { "total_ms", total_ms },           { "image_decode_ms", image_decode_ms },       { "recognition_ms", recognition_ms },
            { "annotation_ms", annotation_ms }, { "annotated_write_ms", annotated_write_ms }, { "detail_write_ms", detail_write_ms },
        };
    }
};

json::value ReadJson(const std::filesystem::path& path)
{
    const auto parsed = common::OpenJsoncFile(path);
    if (!parsed) {
        throw std::runtime_error("unable to read JSON: " + path.string());
    }
    return *parsed;
}

std::string ScoreText(double score)
{
    std::ostringstream stream;
    stream << std::fixed << std::setprecision(3) << score;
    return stream.str();
}

struct AuditItem
{
    std::string display_name;
    std::string icon_id;
    std::string fluid_icon_id;
};

struct TextLabel
{
    std::string text;
    cv::Point origin;
    int height = 18;
    cv::Scalar color { 240, 240, 240 };
};

std::wstring Utf8ToWide(const std::string& text)
{
    const int length = MultiByteToWideChar(CP_UTF8, 0, text.data(), static_cast<int>(text.size()), nullptr, 0);
    if (length <= 0) {
        return {};
    }
    std::wstring result(static_cast<std::size_t>(length), L'\0');
    MultiByteToWideChar(CP_UTF8, 0, text.data(), static_cast<int>(text.size()), result.data(), length);
    return result;
}

void DrawUtf8Labels(cv::Mat& image, const std::vector<TextLabel>& labels)
{
    if (labels.empty()) {
        return;
    }
    cv::Mat bgra;
    cv::cvtColor(image, bgra, cv::COLOR_BGR2BGRA);
    BITMAPINFO info {};
    info.bmiHeader.biSize = sizeof(BITMAPINFOHEADER);
    info.bmiHeader.biWidth = bgra.cols;
    info.bmiHeader.biHeight = -bgra.rows;
    info.bmiHeader.biPlanes = 1;
    info.bmiHeader.biBitCount = 32;
    info.bmiHeader.biCompression = BI_RGB;

    void* pixels = nullptr;
    HDC dc = CreateCompatibleDC(nullptr);
    HBITMAP bitmap = CreateDIBSection(dc, &info, DIB_RGB_COLORS, &pixels, nullptr, 0);
    if (dc == nullptr || bitmap == nullptr || pixels == nullptr) {
        if (bitmap != nullptr) {
            DeleteObject(bitmap);
        }
        if (dc != nullptr) {
            DeleteDC(dc);
        }
        throw std::runtime_error("unable to initialize UTF-8 text renderer");
    }
    std::memcpy(pixels, bgra.data, bgra.total() * bgra.elemSize());
    const HGDIOBJ previous_bitmap = SelectObject(dc, bitmap);
    SetBkMode(dc, TRANSPARENT);
    for (const auto& label : labels) {
        const std::wstring text = Utf8ToWide(label.text);
        HFONT font = CreateFontW(
            -label.height,
            0,
            0,
            0,
            FW_NORMAL,
            FALSE,
            FALSE,
            FALSE,
            DEFAULT_CHARSET,
            OUT_DEFAULT_PRECIS,
            CLIP_DEFAULT_PRECIS,
            CLEARTYPE_QUALITY,
            DEFAULT_PITCH | FF_DONTCARE,
            L"Microsoft YaHei UI");
        const HGDIOBJ previous_font = SelectObject(dc, font);
        SetTextColor(dc, RGB(label.color[2], label.color[1], label.color[0]));
        TextOutW(dc, label.origin.x, label.origin.y, text.data(), static_cast<int>(text.size()));
        SelectObject(dc, previous_font);
        DeleteObject(font);
    }
    std::memcpy(bgra.data, pixels, bgra.total() * bgra.elemSize());
    SelectObject(dc, previous_bitmap);
    DeleteObject(bitmap);
    DeleteDC(dc);
    cv::cvtColor(bgra, image, cv::COLOR_BGRA2BGR);
}

class AuditCatalog
{
public:
    explicit AuditCatalog(const std::filesystem::path& data_root)
        : image_root_(data_root.parent_path().parent_path() / "resource" / "image" / "IconRecognition")
    {
        const auto catalog = ReadJson(data_root / "recognition_items.json").as_object();
        const auto locale = ReadJson(data_root.parent_path().parent_path() / "locales" / "interface" / "zh_cn.json").as_object();
        for (const auto& [item_id, value] : catalog) {
            const auto& object = value.as_object();
            const std::string name_key = "iconRecognition.name." + item_id;
            const std::string display_name =
                locale.contains(name_key) && locale.at(name_key).is_string() ? locale.at(name_key).as_string() : item_id;
            items_.emplace(
                item_id,
                AuditItem {
                    .display_name = display_name,
                    .icon_id = object.at("iconId").as_string(),
                    .fluid_icon_id = object.at("fluidIconId").as_string(),
                });
        }
    }

    const AuditItem& at(const std::string& item_id) const
    {
        const auto found = items_.find(item_id);
        if (found == items_.end()) {
            throw std::runtime_error("audit catalog does not contain item_id: " + item_id);
        }
        return found->second;
    }

    cv::Mat loadIcon(const std::string& icon_id, int size) const
    {
        const auto path = iconrecognition::detail::ResolveIconPath(image_root_, icon_id);
        const cv::Mat source = cv::imread(path.string(), cv::IMREAD_UNCHANGED);
        if (source.empty()) {
            throw std::runtime_error("unable to load audit icon: " + path.string());
        }
        cv::Mat resized;
        cv::resize(source, resized, cv::Size(size, size), 0.0, 0.0, cv::INTER_AREA);
        return resized;
    }

private:
    std::filesystem::path image_root_;
    std::map<std::string, AuditItem> items_;
};

void PasteIcon(cv::Mat& canvas, const cv::Mat& icon, const cv::Rect& target)
{
    cv::Mat destination = canvas(target);
    if (icon.channels() == 4) {
        for (int y = 0; y < icon.rows; ++y) {
            for (int x = 0; x < icon.cols; ++x) {
                const auto source = icon.at<cv::Vec4b>(y, x);
                const double alpha = source[3] / 255.0;
                auto& output = destination.at<cv::Vec3b>(y, x);
                for (int channel = 0; channel < 3; ++channel) {
                    output[channel] = cv::saturate_cast<uchar>(source[channel] * alpha + output[channel] * (1.0 - alpha));
                }
            }
        }
        return;
    }
    icon.copyTo(destination);
}

cv::Mat DrawResult(const cv::Mat& source, const iconrecognition::RecognitionResult& result, const AuditCatalog& catalog)
{
    constexpr int kColumns = 3;
    constexpr int kTileHeight = 92;
    const int audit_rows = static_cast<int>((result.matches.size() + kColumns - 1) / kColumns);
    cv::Mat image(source.rows + audit_rows * kTileHeight, source.cols, CV_8UC3, cv::Scalar(28, 30, 34));
    source.copyTo(image(cv::Rect(0, 0, source.cols, source.rows)));
    cv::rectangle(image, result.roi, cv::Scalar(0, 255, 255), 2);
    if (result.diagnostics) {
        for (const auto& cell : result.diagnostics->cells) {
            if (!cell.rejected_reason) {
                continue;
            }
            cv::rectangle(image, cell.cell_box, cv::Scalar(0, 0, 255), 1);
        }
    }
    std::vector<TextLabel> labels;
    for (std::size_t index = 0; index < result.matches.size(); ++index) {
        const auto& match = result.matches[index];
        cv::rectangle(image, match.cell_box, cv::Scalar(0, 255, 0), 2);
        cv::rectangle(image, match.item_box, cv::Scalar(255, 128, 0), 1);
        const std::string number = std::to_string(index + 1);
        cv::putText(
            image,
            number,
            match.cell_box.tl() + cv::Point(3, 16),
            cv::FONT_HERSHEY_SIMPLEX,
            0.48,
            cv::Scalar(0, 0, 0),
            4,
            cv::LINE_AA);
        cv::putText(
            image,
            number,
            match.cell_box.tl() + cv::Point(3, 16),
            cv::FONT_HERSHEY_SIMPLEX,
            0.48,
            cv::Scalar(255, 255, 255),
            2,
            cv::LINE_AA);

        const int column = static_cast<int>(index % kColumns);
        const int row = static_cast<int>(index / kColumns);
        const int tile_width = source.cols / kColumns;
        const cv::Rect tile(column * tile_width, source.rows + row * kTileHeight, tile_width, kTileHeight);
        cv::rectangle(image, tile, cv::Scalar(62, 66, 74), 1);
        cv::putText(image, number, tile.tl() + cv::Point(8, 25), cv::FONT_HERSHEY_SIMPLEX, 0.55, cv::Scalar(255, 255, 255), 1, cv::LINE_AA);

        const auto& audit = catalog.at(match.item.item_id);
        const cv::Rect icon_box(tile.x + 34, tile.y + 10, 64, 64);
        PasteIcon(image, catalog.loadIcon(audit.icon_id, icon_box.width), icon_box);
        if (!audit.fluid_icon_id.empty()) {
            const cv::Rect fluid_box(tile.x + 74, tile.y + 48, 32, 32);
            PasteIcon(image, catalog.loadIcon(audit.fluid_icon_id, fluid_box.width), fluid_box);
            cv::rectangle(image, fluid_box, cv::Scalar(210, 210, 210), 1);
        }
        labels.push_back(TextLabel { .text = audit.display_name, .origin = cv::Point(tile.x + 108, tile.y + 8), .height = 18 });
        cv::putText(
            image,
            match.item.item_id,
            cv::Point(tile.x + 108, tile.y + 49),
            cv::FONT_HERSHEY_SIMPLEX,
            0.34,
            cv::Scalar(210, 215, 220),
            1,
            cv::LINE_AA);
        const std::string position = match.row && match.column ? "score=" + ScoreText(match.score) + " row=" + std::to_string(*match.row)
                                                                     + " col=" + std::to_string(*match.column)
                                                               : "score=" + ScoreText(match.score) + " single_roi";
        cv::putText(
            image,
            position,
            cv::Point(tile.x + 108, tile.y + 70),
            cv::FONT_HERSHEY_SIMPLEX,
            0.34,
            cv::Scalar(160, 220, 170),
            1,
            cv::LINE_AA);
    }
    DrawUtf8Labels(image, labels);
    return image;
}

iconrecognition::RecognitionResult RunCase(
    iconrecognition::IconRecognizer& recognizer,
    const cv::Mat& image,
    const iconrecognition::test::ManualRunnerCase& test_case,
    bool debug,
    bool recognize_region_unavailable)
{
    iconrecognition::RecognitionRequest request;
    request.grid_type = test_case.grid_type;
    request.roi = test_case.roi;
    request.candidates = test_case.candidates;
    request.debug = debug;
    request.recognize_region_unavailable = recognize_region_unavailable;
    if ((request.roi & cv::Rect(0, 0, image.cols, image.rows)) != request.roi) {
        throw std::runtime_error("roi must be fully inside the input image");
    }
    return recognizer.recognize(image, request);
}

void WriteDetail(const std::filesystem::path& path, const iconrecognition::RecognitionResult& result)
{
    json::object detail = json::value(result).as_object();
    if (result.diagnostics) {
        detail["diagnostics"] = *result.diagnostics;
    }
    std::ofstream stream(path, std::ios::binary | std::ios::trunc);
    stream << json::value(std::move(detail)).dumps(4) << '\n';
}

std::string RunLabel(const iconrecognition::test::ManualRunnerOptions& options)
{
    const std::string dataset = options.dataset == iconrecognition::test::TestDataset::Win32 ? "win32-"
                                : options.dataset == iconrecognition::test::TestDataset::Adb ? "adb-"
                                                                                             : "";
    if (options.all_images) {
        return dataset + "all";
    }
    if (options.grid_type) {
        return dataset + std::string(iconrecognition::GridTypeName(*options.grid_type));
    }
    if (options.image_name) {
        return dataset + std::filesystem::path(*options.image_name).stem().string();
    }
    return "manual";
}

struct CompletedCase
{
    json::object report;
    std::string console_line;
    bool failed = false;
};

std::size_t PhysicalCoreCount()
{
    DWORD bytes = 0;
    GetLogicalProcessorInformationEx(RelationProcessorCore, nullptr, &bytes);
    if (bytes > 0) {
        std::vector<std::byte> buffer(bytes);
        if (GetLogicalProcessorInformationEx(
                RelationProcessorCore,
                reinterpret_cast<PSYSTEM_LOGICAL_PROCESSOR_INFORMATION_EX>(buffer.data()),
                &bytes)) {
            std::size_t count = 0;
            DWORD offset = 0;
            while (offset < bytes) {
                const auto* entry = reinterpret_cast<const SYSTEM_LOGICAL_PROCESSOR_INFORMATION_EX*>(buffer.data() + offset);
                if (entry->Relationship == RelationProcessorCore) {
                    ++count;
                }
                offset += entry->Size;
            }
            if (count > 0) {
                return count;
            }
        }
    }
    return std::max(1U, std::thread::hardware_concurrency());
}

std::vector<iconrecognition::RecognitionRequest>
    PreloadRequests(const std::vector<iconrecognition::test::ManualRunnerCase>& cases, bool recognize_region_unavailable)
{
    std::vector<iconrecognition::RecognitionRequest> requests;
    requests.reserve(cases.size());
    for (const auto& test_case : cases) {
        iconrecognition::RecognitionRequest request;
        request.grid_type = test_case.grid_type;
        request.roi = test_case.roi;
        request.candidates = test_case.candidates;
        request.recognize_region_unavailable = recognize_region_unavailable;
        requests.push_back(std::move(request));
    }
    return requests;
}

std::filesystem::path CreateRunRoot(const std::filesystem::path& output_root, const std::string& label)
{
    const auto now = std::chrono::system_clock::now();
    const std::time_t seconds = std::chrono::system_clock::to_time_t(now);
    std::tm local_time {};
    localtime_s(&local_time, &seconds);
    const auto milliseconds = std::chrono::duration_cast<std::chrono::milliseconds>(now.time_since_epoch()).count() % 1000;
    std::ostringstream name;
    name << std::put_time(&local_time, "%Y%m%d-%H%M%S") << '-' << std::setfill('0') << std::setw(3) << milliseconds << '-' << label;
    std::filesystem::path run_root = output_root / name.str();
    for (int duplicate = 1; std::filesystem::exists(run_root); ++duplicate) {
        run_root = output_root / (name.str() + "-" + std::to_string(duplicate));
    }
    std::filesystem::create_directories(run_root);
    return run_root;
}

} // namespace

int main(int argc, char** argv)
{
    try {
        std::vector<std::string> arguments;
        arguments.reserve(static_cast<std::size_t>(argc - 1));
        for (int index = 1; index < argc; ++index) {
            arguments.emplace_back(argv[index]);
        }
        const auto options = iconrecognition::test::ParseManualRunnerOptions(arguments);
        if (options.show_help) {
            std::cout << iconrecognition::test::ManualRunnerUsage();
            return 0;
        }

        const std::filesystem::path input_root = ICON_RECOGNITION_TEST_INPUT_DIR;
        const std::filesystem::path rois_path = iconrecognition::test::ResolveManualRunnerRoisPath(
            options,
            ICON_RECOGNITION_TEST_WIN32_ROIS_PATH,
            ICON_RECOGNITION_TEST_ADB_ROIS_PATH);
        const auto cases = iconrecognition::test::DiscoverManualRunnerCases(input_root, rois_path, options);
        std::optional<iconrecognition::test::ExpectedResults> expected_results;
        if (!options.expected_path.empty()) {
            expected_results.emplace(iconrecognition::test::LoadExpectedResults(options.expected_path));
        }
        const auto run_root = CreateRunRoot(ICON_RECOGNITION_TEST_OUTPUT_DIR, RunLabel(options));
        const auto annotated_root = run_root / "annotated";
        const auto detail_root = run_root / "detail";
        std::filesystem::create_directories(annotated_root);
        std::filesystem::create_directories(detail_root);

        const auto initialize_started = RunnerClock::now();
        iconrecognition::IconRecognizer recognizer(ICON_RECOGNITION_TEST_DATA_ROOT);
        if (!recognizer.initialize()) {
            throw std::runtime_error("recognizer initialization failed");
        }
        const double initialize_ms = ElapsedMilliseconds(initialize_started);
        const auto preload_started = RunnerClock::now();
        if (!recognizer.preload(PreloadRequests(cases, options.recognize_region_unavailable))) {
            throw std::runtime_error("recognizer template preload failed");
        }
        const double preload_ms = ElapsedMilliseconds(preload_started);
        const std::size_t jobs = iconrecognition::test::ResolveManualRunnerJobs(options, PhysicalCoreCount(), cases.size());
        if (jobs > 1) {
            cv::setNumThreads(1);
        }
        const auto audit_catalog_started = RunnerClock::now();
        const AuditCatalog audit_catalog(ICON_RECOGNITION_TEST_DATA_ROOT);
        const double audit_catalog_ms = ElapsedMilliseconds(audit_catalog_started);
        const json::object startup_performance {
            { "initialize_ms", initialize_ms },
            { "preload_ms", preload_ms },
            { "audit_catalog_ms", audit_catalog_ms },
        };
        if (options.debug) {
            std::cout << "IconRecognition runner startup performance " << json::value(startup_performance).dumps() << '\n';
        }

        const auto started_at = std::chrono::steady_clock::now();
        std::vector<std::optional<CompletedCase>> completed(cases.size());
        const auto errors = iconrecognition::test::ExecuteManualRunnerCases(cases.size(), jobs, [&](std::size_t index) {
            const auto& test_case = cases[index];
            const auto case_started = RunnerClock::now();
            RunnerCasePerformance case_performance;
            const auto decode_started = RunnerClock::now();
            const cv::Mat image = cv::imread(test_case.image_path.string(), cv::IMREAD_COLOR);
            case_performance.image_decode_ms = ElapsedMilliseconds(decode_started);
            if (image.empty()) {
                throw std::runtime_error("unable to decode input image: " + test_case.image_path.string());
            }
            const auto recognition_started = RunnerClock::now();
            const auto result = RunCase(recognizer, image, test_case, options.debug, options.recognize_region_unavailable);
            case_performance.recognition_ms = ElapsedMilliseconds(recognition_started);
            const std::string image_name = test_case.image_path.lexically_relative(input_root).generic_string();
            const bool expected_checked =
                expected_results && expected_results->find(image_name, test_case.grid_type, test_case.roi_name) != nullptr;
            const auto expected_error = expected_results ? iconrecognition::test::CompareExpectedCase(
                                            *expected_results,
                                            image_name,
                                            test_case.grid_type,
                                            test_case.roi_name,
                                            result,
                                            iconrecognition::test::IsSupplementalLocalImage(image_name))
                                                         : std::nullopt;
            const auto annotation_started = RunnerClock::now();
            const cv::Mat annotated = DrawResult(image, result, audit_catalog);
            case_performance.annotation_ms = ElapsedMilliseconds(annotation_started);
            const std::string output_name = std::string(iconrecognition::GridTypeName(test_case.grid_type)) + "-" + test_case.roi_name + "-"
                                            + test_case.image_path.stem().string();
            const auto annotated_path = annotated_root / (output_name + ".png");
            const auto detail_path = detail_root / (output_name + ".json");
            const auto annotated_write_started = RunnerClock::now();
            if (!cv::imwrite(annotated_path.string(), annotated)) {
                throw std::runtime_error("unable to write annotated image: " + annotated_path.string());
            }
            case_performance.annotated_write_ms = ElapsedMilliseconds(annotated_write_started);
            const auto detail_write_started = RunnerClock::now();
            WriteDetail(detail_path, result);
            case_performance.detail_write_ms = ElapsedMilliseconds(detail_write_started);
            case_performance.total_ms = ElapsedMilliseconds(case_started);
            const bool failed = result.error_code == "exception" || expected_error.has_value();
            json::object case_report {
                { "image", image_name },
                { "grid_type", std::string(iconrecognition::GridTypeName(test_case.grid_type)) },
                { "roi_name", test_case.roi_name },
                { "roi", iconrecognition::RectToJson(test_case.roi) },
                { "matched", result.matched },
                { "match_count", static_cast<unsigned long long>(result.matches.size()) },
                { "expected_checked", expected_checked },
                { "failed", failed },
                { "annotated", annotated_path.string() },
                { "detail", detail_path.string() },
            };
            if (options.debug) {
                case_report["runner_performance"] = case_performance;
            }
            if (expected_error) {
                case_report["expected_error"] = *expected_error;
            }
            completed[index] = CompletedCase {
                .report = std::move(case_report),
                .console_line = image_name + " [" + test_case.roi_name + "] -> " + annotated_path.string()
                                + (expected_error ? " EXPECTED MISMATCH: " + *expected_error : "")
                                + (options.debug ? " performance=" + json::value(case_performance).dumps() : ""),
                .failed = failed,
            };
        });

        json::array reports;
        std::size_t failed = 0;
        for (std::size_t index = 0; index < cases.size(); ++index) {
            if (errors[index]) {
                std::string message = "unknown case failure";
                try {
                    std::rethrow_exception(errors[index]);
                }
                catch (const std::exception& error) {
                    message = error.what();
                }
                const auto& test_case = cases[index];
                const std::string image_name = test_case.image_path.lexically_relative(input_root).generic_string();
                reports.emplace_back(json::object {
                    { "image", image_name },
                    { "grid_type", std::string(iconrecognition::GridTypeName(test_case.grid_type)) },
                    { "roi_name", test_case.roi_name },
                    { "roi", iconrecognition::RectToJson(test_case.roi) },
                    { "matched", false },
                    { "match_count", 0 },
                    { "failed", true },
                    { "case_error", message },
                });
                std::cout << image_name << " [" << test_case.roi_name << "] -> ERROR: " << message << '\n';
                ++failed;
                continue;
            }
            if (!completed[index]) {
                throw std::runtime_error("manual runner case completed without a result slot");
            }
            reports.emplace_back(std::move(completed[index]->report));
            std::cout << completed[index]->console_line << '\n';
            failed += completed[index]->failed ? 1 : 0;
        }
        const auto elapsed = std::chrono::duration<double>(std::chrono::steady_clock::now() - started_at).count();

        const auto report_path = run_root / "report.json";
        std::ofstream report(report_path, std::ios::binary | std::ios::trunc);
        json::object report_object {
            { "case_count", static_cast<unsigned long long>(reports.size()) },
            { "failure_count", static_cast<unsigned long long>(failed) },
            { "jobs", static_cast<unsigned long long>(jobs) },
            { "opencv_threads", cv::getNumThreads() },
            { "elapsed_seconds", elapsed },
            { "cases_per_second", elapsed > 0.0 ? reports.size() / elapsed : 0.0 },
            { "cases", std::move(reports) },
        };
        report_object["dataset"] = options.dataset == iconrecognition::test::TestDataset::Win32 ? "win32"
                                   : options.dataset == iconrecognition::test::TestDataset::Adb ? "adb"
                                                                                                : "unspecified";
        if (!options.expected_path.empty()) {
            report_object["expected"] = options.expected_path.string();
        }
        if (options.debug) {
            report_object["startup_performance"] = startup_performance;
        }
        report << json::value(std::move(report_object)).dumps(4) << '\n';
        return failed == 0 ? 0 : 1;
    }
    catch (const std::invalid_argument& error) {
        std::cerr << error.what() << "\n\n" << iconrecognition::test::ManualRunnerUsage();
        return 2;
    }
    catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 2;
    }
}
