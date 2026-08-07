#define NOMINMAX
#include <windows.h>

#include <cstring>
#include <filesystem>
#include <fstream>
#include <iomanip>
#include <iostream>
#include <map>
#include <set>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>

#include <MaaUtils/NoWarningCV.hpp>
#include <meojson/json.hpp>

#include "../IconRecognizer.h"
#include "../detail/RecognitionDiagnostics.h"

namespace
{

json::value ReadJson(const std::filesystem::path& path)
{
    const auto parsed = json::open(path.string());
    if (!parsed) {
        throw std::runtime_error("unable to read JSON: " + path.string());
    }
    return *parsed;
}

cv::Rect ReadRoi(const json::value& value)
{
    if (!value.is_array() || value.as_array().size() != 4) {
        throw std::runtime_error("roi must use Maa [x,y,width,height] format");
    }
    const auto& values = value.as_array();
    const cv::Rect roi(values.at(0).as_integer(), values.at(1).as_integer(), values.at(2).as_integer(), values.at(3).as_integer());
    if (roi.width <= 0 || roi.height <= 0) {
        throw std::runtime_error("roi width and height must be positive");
    }
    return roi;
}

std::vector<std::string> ReadStrings(const json::object& object, const char* key)
{
    std::vector<std::string> result;
    if (!object.contains(key)) {
        return result;
    }
    for (const auto& value : object.at(key).as_array()) {
        result.push_back(value.as_string());
    }
    return result;
}

std::string ImageName(const json::object& object)
{
    return std::filesystem::path(object.at("image").as_string()).filename().string();
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
    int rarity = 0;
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
                    .rarity = object.at("rarity").as_integer(),
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

    cv::Mat loadIcon(const AuditItem& item, const std::string& icon_id, int size) const
    {
        const auto path = image_root_ / std::to_string(item.rarity) / (icon_id + ".png");
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
        PasteIcon(image, catalog.loadIcon(audit, audit.icon_id, icon_box.width), icon_box);
        if (!audit.fluid_icon_id.empty()) {
            const cv::Rect fluid_box(tile.x + 74, tile.y + 48, 32, 32);
            PasteIcon(image, catalog.loadIcon(audit, audit.fluid_icon_id, fluid_box.width), fluid_box);
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

iconrecognition::RecognitionResult RunCase(iconrecognition::IconRecognizer& recognizer, const cv::Mat& image, const json::object& object)
{
    const auto grid_type = iconrecognition::ParseGridType(object.at("grid_type").as_string());
    if (!grid_type) {
        throw std::runtime_error("unknown grid_type: " + object.at("grid_type").as_string());
    }
    iconrecognition::RecognitionRequest request;
    request.grid_type = *grid_type;
    request.roi = ReadRoi(object.at("roi"));
    request.candidates.item_ids = ReadStrings(object, "item_ids");
    request.candidates.item_filters = ReadStrings(object, "item_filters");
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

void AppendCases(json::array& output, const json::value& manifest, const char* key)
{
    for (const auto& value : manifest.as_object().at(key).as_array()) {
        output.emplace_back(value);
    }
}

} // namespace

int main(int argc, char** argv)
{
    try {
        if (argc < 2) {
            throw std::runtime_error("usage: icon-recognition-manual-runner <manifest.json> [image.png ...]");
        }
        const std::filesystem::path manifest_path = argv[1];
        std::set<std::string> requested;
        for (int index = 2; index < argc; ++index) {
            requested.emplace(std::filesystem::path(argv[index]).filename().string());
        }

        const auto manifest = ReadJson(manifest_path);
        json::array cases;
        AppendCases(cases, manifest, "cases");

        const std::filesystem::path input_root = ICON_RECOGNITION_TEST_INPUT_DIR;
        const std::filesystem::path output_root = ICON_RECOGNITION_TEST_OUTPUT_DIR;
        const auto annotated_root = output_root / "annotated";
        const auto detail_root = output_root / "detail";
        std::filesystem::create_directories(annotated_root);
        std::filesystem::create_directories(detail_root);

        iconrecognition::IconRecognizer recognizer(ICON_RECOGNITION_TEST_DATA_ROOT);
        if (!recognizer.initialize()) {
            throw std::runtime_error("recognizer initialization failed");
        }
        const AuditCatalog audit_catalog(ICON_RECOGNITION_TEST_DATA_ROOT);

        json::array reports;
        std::set<std::string> completed_images;
        std::map<std::string, int> output_name_counts;
        std::size_t skipped = 0;
        std::size_t failed = 0;
        for (const auto& value : cases) {
            const auto& object = value.as_object();
            const std::string image_name = ImageName(object);
            if (!requested.empty() && !requested.contains(image_name)) {
                continue;
            }
            const auto image_path = input_root / image_name;
            if (!std::filesystem::is_regular_file(image_path)) {
                if (requested.contains(image_name)) {
                    throw std::runtime_error("requested input image is missing: " + image_path.string());
                }
                ++skipped;
                continue;
            }
            const cv::Mat image = cv::imread(image_path.string(), cv::IMREAD_COLOR);
            if (image.empty()) {
                throw std::runtime_error("unable to decode input image: " + image_path.string());
            }
            const auto result = RunCase(recognizer, image, object);
            cv::Mat annotated = DrawResult(image, result, audit_catalog);
            const std::string output_stem = std::filesystem::path(image_name).stem().string() + "-" + object.at("grid_type").as_string();
            const int output_index = ++output_name_counts[output_stem];
            const std::string output_name = output_stem + (output_index == 1 ? "" : "-" + std::to_string(output_index));
            const auto annotated_path = annotated_root / (output_name + ".png");
            const auto detail_path = detail_root / (output_name + ".json");
            if (!cv::imwrite(annotated_path.string(), annotated)) {
                throw std::runtime_error("unable to write annotated image: " + annotated_path.string());
            }
            WriteDetail(detail_path, result);
            const bool case_failed = result.error_code == "exception";
            failed += case_failed ? 1 : 0;
            completed_images.insert(image_name);
            reports.emplace_back(json::object {
                { "image", image_name },
                { "grid_type", object.at("grid_type").as_string() },
                { "matched", result.matched },
                { "match_count", static_cast<unsigned long long>(result.matches.size()) },
                { "failed", case_failed },
                { "annotated", annotated_path.string() },
                { "detail", detail_path.string() },
            });
            std::cout << image_name << " -> " << annotated_path.string() << '\n';
        }
        for (const auto& image_name : requested) {
            if (!completed_images.contains(image_name)) {
                throw std::runtime_error("unknown or unprocessed image: " + image_name);
            }
        }

        const auto report_path = output_root / "report.json";
        std::ofstream report(report_path, std::ios::binary | std::ios::trunc);
        report << json::value(json::object {
                                  { "case_count", static_cast<unsigned long long>(reports.size()) },
                                  { "skipped_count", static_cast<unsigned long long>(skipped) },
                                  { "failure_count", static_cast<unsigned long long>(failed) },
                                  { "cases", std::move(reports) },
                              })
                      .dumps(4)
               << '\n';
        return failed == 0 ? 0 : 1;
    }
    catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 2;
    }
}
