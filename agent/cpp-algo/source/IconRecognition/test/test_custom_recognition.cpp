#include <algorithm>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <string>
#include <string_view>
#include <vector>

#include <MaaFramework/Utility/MaaBuffer.h>
#include <MaaUtils/Logger.h>
#include <MaaUtils/NoWarningCV.hpp>
#include <meojson/json.hpp>

#include "../../utils.h"
#include "IconRecognitionRecognition.h"
#include "IconRecognizer.h"
#include "detail/RecognitionDiagnostics.h"

namespace
{

class ImageBuffer
{
public:
    ImageBuffer()
        : handle_(MaaImageBufferCreate())
    {
        if (handle_ == nullptr) {
            throw std::runtime_error("failed to create MaaImageBuffer");
        }
    }

    ~ImageBuffer() { MaaImageBufferDestroy(handle_); }

    ImageBuffer(const ImageBuffer&) = delete;
    ImageBuffer& operator=(const ImageBuffer&) = delete;

    MaaImageBuffer* get() const { return handle_; }

    void set(const cv::Mat& image) const
    {
        if (!MaaImageBufferSetRawData(handle_, image.data, image.cols, image.rows, image.type())) {
            throw std::runtime_error("failed to populate MaaImageBuffer");
        }
    }

private:
    MaaImageBuffer* handle_ = nullptr;
};

class StringBuffer
{
public:
    StringBuffer()
        : handle_(MaaStringBufferCreate())
    {
        if (handle_ == nullptr) {
            throw std::runtime_error("failed to create MaaStringBuffer");
        }
    }

    ~StringBuffer() { MaaStringBufferDestroy(handle_); }

    StringBuffer(const StringBuffer&) = delete;
    StringBuffer& operator=(const StringBuffer&) = delete;

    MaaStringBuffer* get() const { return handle_; }

    json::object detail() const
    {
        const char* text = MaaStringBufferGet(handle_);
        if (text == nullptr || *text == '\0') {
            throw std::runtime_error("recognition detail is empty");
        }
        const auto parsed = json::parse(text);
        if (!parsed || !parsed->is_object()) {
            throw std::runtime_error("recognition detail is not a JSON object");
        }
        return parsed->as_object();
    }

private:
    MaaStringBuffer* handle_ = nullptr;
};

void Require(bool condition, std::string_view message)
{
    if (!condition) {
        throw std::runtime_error(std::string(message));
    }
}

std::string ErrorCode(const json::object& detail)
{
    Require(detail.contains("error") && detail.at("error").is_object(), "failure detail must contain error");
    const auto& error = detail.at("error").as_object();
    Require(error.contains("code") && error.at("code").is_string(), "failure detail must contain error.code");
    return error.at("code").as_string();
}

std::string ErrorMessage(const json::object& detail)
{
    Require(detail.contains("error") && detail.at("error").is_object(), "failure detail must contain error");
    const auto& error = detail.at("error").as_object();
    Require(error.contains("message") && error.at("message").is_string(), "failure detail must contain error.message");
    return error.at("message").as_string();
}

constexpr MaaRect kValidRoi { 0, 0, 54, 54 };
json::object RunFailure(const MaaImageBuffer* image, const char* param, MaaRect& out_box, const MaaRect* roi = &kValidRoi)
{
    // 这些调用专门验证失败契约，ERROR 属于预期结果，不应污染 CI 控制台。
    MAA_LOG_NS::Logger::get_instance().set_stdout_level(MAA_LOG_NS::level::off);
    OnScopeLeave([]() { MAA_LOG_NS::Logger::get_instance().set_stdout_level(MAA_LOG_NS::level::error); });
    StringBuffer detail;
    const MaaBool matched = iconrecognition::IconRecognitionRun(
        nullptr,
        0,
        "IconRecognitionTest",
        "IconRecognition",
        param,
        image,
        roi,
        nullptr,
        &out_box,
        detail.get());
    Require(!matched, "invalid recognition request must fail");
    return detail.detail();
}

void RequireUntouched(const MaaRect& box)
{
    Require(box.x == 101 && box.y == 202 && box.width == 303 && box.height == 404, "failed recognition must not write out_box");
}

// 参数校验只需要有效图像，直接读取真实截图；不再生成纯色画布。
void TestEmptyImageWritesInvalidImageDetail()
{
    ImageBuffer image;
    MaaRect out_box { 101, 202, 303, 404 };
    const auto detail = RunFailure(image.get(), R"({"grid_type":"valuables"})", out_box);
    Require(ErrorCode(detail) == "invalid_image", "empty image must use invalid_image error code");
    Require(!detail.contains("grid_type"), "empty image failure must omit grid_type before parameter parsing");
    RequireUntouched(out_box);
}

void TestUnknownGridTypeIsRejected()
{
    ImageBuffer image;
    const cv::Mat pixels = cv::imread("tests/MaaEndTestset/Win32/Official_CN/IconRecognition/transfer/25.png");
    Require(!pixels.empty(), "real contract screenshot must be readable");
    image.set(pixels);
    MaaRect out_box { 101, 202, 303, 404 };
    const auto detail = RunFailure(image.get(), R"({"grid_type":"unknown"})", out_box);
    Require(ErrorMessage(detail).find("grid_type") != std::string::npos, "unknown grid type error must identify grid_type");
    RequireUntouched(out_box);
}

void TestRequiredParametersAreRejected()
{
    ImageBuffer image;
    const cv::Mat pixels = cv::imread("tests/MaaEndTestset/Win32/Official_CN/IconRecognition/transfer/25.png");
    Require(!pixels.empty(), "real contract screenshot must be readable");
    image.set(pixels);
    for (const auto& [param, field] : {
             std::pair { R"({})", "grid_type" },
         }) {
        MaaRect out_box { 101, 202, 303, 404 };
        const auto detail = RunFailure(image.get(), param, out_box);
        Require(ErrorMessage(detail).find(field) != std::string::npos, "missing required parameter must identify its field");
        RequireUntouched(out_box);
    }
}

void TestInvalidNativeRoiIsRejected()
{
    ImageBuffer image;
    const cv::Mat pixels = cv::imread("tests/MaaEndTestset/Win32/Official_CN/IconRecognition/transfer/25.png");
    Require(!pixels.empty(), "real contract screenshot must be readable");
    image.set(pixels);
    const MaaRect zero_width { 0, 0, 0, 54 };
    const MaaRect zero_height { 0, 0, 54, 0 };
    for (const MaaRect* roi : {
             static_cast<const MaaRect*>(nullptr),
             &zero_width,
             &zero_height,
         }) {
        MaaRect out_box { 101, 202, 303, 404 };
        const auto detail = RunFailure(image.get(), R"({"grid_type":"single_roi"})", out_box, roi);
        Require(ErrorMessage(detail).find("roi") != std::string::npos, "invalid roi error must identify roi");
        Require(
            detail.contains("grid_type") && detail.at("grid_type").as_string() == "single_roi",
            "single_roi failure must preserve grid_type");
        RequireUntouched(out_box);
    }
}

void TestMalformedCandidateListsAreRejected()
{
    ImageBuffer image;
    const cv::Mat pixels = cv::imread("tests/MaaEndTestset/Win32/Official_CN/IconRecognition/transfer/25.png");
    Require(!pixels.empty(), "real contract screenshot must be readable");
    image.set(pixels);
    for (const auto& [param, field] : {
             std::pair { R"({"grid_type":"single_roi","item_ids":"bad"})", "item_ids" },
             std::pair { R"({"grid_type":"single_roi","item_filters":[1]})", "item_filters" },
             std::pair { R"({"grid_type":"single_roi","additional_item_filters":[1]})", "additional_item_filters" },
             std::pair { R"({"grid_type":"single_roi","excluded_item_ids":[1]})", "excluded_item_ids" },
             std::pair { R"({"grid_type":"single_roi","item_recheck_filters":[1]})", "item_recheck_filters" },
         }) {
        MaaRect out_box { 101, 202, 303, 404 };
        const auto detail = RunFailure(image.get(), param, out_box);
        Require(ErrorMessage(detail).find(field) != std::string::npos, "candidate error must identify its field");
        Require(
            detail.contains("grid_type") && detail.at("grid_type").as_string() == "single_roi",
            "single_roi failure must preserve grid_type");
        RequireUntouched(out_box);
    }
}

void TestMalformedScalarParametersAreRejected()
{
    ImageBuffer image;
    const cv::Mat pixels = cv::imread("tests/MaaEndTestset/Win32/Official_CN/IconRecognition/transfer/25.png");
    Require(!pixels.empty(), "real contract screenshot must be readable");
    image.set(pixels);
    for (const auto& [param, field] : {
             std::pair { R"({"grid_type":1})", "grid_type" },
             std::pair { R"({"grid_type":"single_roi","threshold":"bad"})", "threshold" },
             std::pair { R"({"grid_type":"single_roi","subpixel_threshold":"bad"})", "subpixel_threshold" },
             std::pair { R"({"grid_type":"single_roi","debug":"bad"})", "debug" },
             std::pair { R"({"grid_type":"transfer","deduplicate":"bad"})", "deduplicate" },
             std::pair { R"({"grid_type":"single_roi","recognize_region_unavailable":"bad"})", "recognize_region_unavailable" },
         }) {
        MaaRect out_box { 101, 202, 303, 404 };
        const auto detail = RunFailure(image.get(), param, out_box);
        Require(ErrorMessage(detail).find(field) != std::string::npos, "scalar parameter error must identify its field");
        RequireUntouched(out_box);
    }
}

void TestRemovedGridScaleParameterIsRejected()
{
    ImageBuffer image;
    const cv::Mat pixels = cv::imread("tests/MaaEndTestset/Win32/Official_CN/IconRecognition/transfer/25.png");
    Require(!pixels.empty(), "real contract screenshot must be readable");
    image.set(pixels);
    MaaRect out_box { 101, 202, 303, 404 };
    for (const char* param : {
             R"({"grid_type":"single_roi","grid_scale":1.25})",
             R"({"grid_type":"single_roi","grid_scale":"bad"})",
         }) {
        const auto detail = RunFailure(image.get(), param, out_box);
        const std::string message = ErrorMessage(detail);
        Require(message.find("grid_scale") != std::string::npos, "removed parameter error must identify grid_scale");
        Require(message.find("not supported") != std::string::npos, "removed grid_scale parameter must be rejected explicitly");
        RequireUntouched(out_box);
    }
}

void TestGridDiagnosticsSerializeSelectionEvidence()
{
    iconrecognition::detail::RecognitionDiagnostics diagnostics;
    diagnostics.grids.push_back(iconrecognition::detail::GridSelectionDiagnostics {
        .origin = cv::Point2d(161.0, 217.0),
        .pitch = cv::Point2d(69.0, 69.0),
        .rows = 2,
        .columns = 1,
        .best_score = 0.91,
        .second_score = 0.42,
        .score_margin = 0.49,
        .structure_score = 0.72,
        .rarity_score = 0.95,
        .consistency_score = 1.0,
        .trusted_rarity_cells = { 0, 1, 0, 0, 0, 0 },
        .fallback_used = false,
        .rejected_reasons = { "legacy-conflict" },
    });

    const auto object = diagnostics.to_json().as_object();
    Require(!object.contains("performance"), "normal diagnostics must not serialize debug performance data");
    Require(object.contains("grids") && object.at("grids").as_array().size() == 1, "diagnostics must serialize grid evidence");
    const auto& grid = object.at("grids").as_array().at(0).as_object();
    Require(grid.at("origin").as_object().at("x").as_double() == 161.0, "grid diagnostics must preserve origin");
    Require(grid.at("pitch").as_object().at("y").as_double() == 69.0, "grid diagnostics must preserve pitch");
    Require(grid.at("trusted_rarity_cells").as_array().at(1).as_integer() == 1, "grid diagnostics must preserve six-color evidence");
    Require(grid.at("rejected_reasons").as_array().size() == 1, "grid diagnostics must preserve rejection reasons");
}

void TestRecognizerPreloadsEveryRequestedTemplateSize()
{
    iconrecognition::IconRecognizer recognizer(get_exe_dir() / ".." / "data" / "IconRecognition");
    Require(recognizer.initialize(), "preload recognizer must initialize");

    iconrecognition::RecognitionRequest transfer;
    transfer.grid_type = iconrecognition::GridType::Transfer;
    iconrecognition::RecognitionRequest credit;
    credit.grid_type = iconrecognition::GridType::CreditTrade;
    iconrecognition::RecognitionRequest single;
    single.grid_type = iconrecognition::GridType::SingleRoi;
    single.roi = cv::Rect(0, 0, 54, 54);

    Require(recognizer.preload({ transfer, credit, single }), "recognizer must preload fixed and single-ROI template sizes");
}

} // namespace

int main()
{
    try {
        TestEmptyImageWritesInvalidImageDetail();
        TestUnknownGridTypeIsRejected();
        TestRequiredParametersAreRejected();
        TestInvalidNativeRoiIsRejected();
        TestMalformedCandidateListsAreRejected();
        TestMalformedScalarParametersAreRejected();
        TestRemovedGridScaleParameterIsRejected();
        TestGridDiagnosticsSerializeSelectionEvidence();
        TestRecognizerPreloadsEveryRequestedTemplateSize();
        std::cout << "IconRecognition custom recognition tests passed\n";
        return 0;
    }
    catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}
