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
#include "detail/DisabledIcon.h"
#include "detail/RecognitionDiagnostics.h"
#include "detail/TemplateTypes.h"

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

class ImageListBuffer
{
public:
    ImageListBuffer()
        : handle_(MaaImageListBufferCreate())
    {
        if (handle_ == nullptr) {
            throw std::runtime_error("failed to create MaaImageListBuffer");
        }
    }

    ~ImageListBuffer() { MaaImageListBufferDestroy(handle_); }

    MaaImageListBuffer* get() const { return handle_; }

private:
    MaaImageListBuffer* handle_ = nullptr;
};

class FrameworkFixture
{
public:
    FrameworkFixture()
        : resource_(MaaResourceCreate())
        , tasker_(MaaTaskerCreate())
    {
        if (resource_ == nullptr || tasker_ == nullptr) {
            throw std::runtime_error("failed to create MaaFramework fixture");
        }
        if (!MaaResourceRegisterCustomRecognition(resource_, "IconRecognition", iconrecognition::IconRecognitionRun, nullptr)) {
            throw std::runtime_error("failed to register IconRecognition on MaaResource");
        }
        if (!MaaTaskerBindResource(tasker_, resource_)) {
            throw std::runtime_error("failed to bind MaaResource to MaaTasker");
        }
    }

    ~FrameworkFixture()
    {
        MaaTaskerDestroy(tasker_);
        MaaResourceDestroy(resource_);
    }

    FrameworkFixture(const FrameworkFixture&) = delete;
    FrameworkFixture& operator=(const FrameworkFixture&) = delete;

    MaaTasker* tasker() const { return tasker_; }

private:
    MaaResource* resource_ = nullptr;
    MaaTasker* tasker_ = nullptr;
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
constexpr int kSyntheticRoiX = 7;
constexpr int kSyntheticRoiY = 5;
constexpr int kSyntheticRoiSize = 54;
constexpr int kSyntheticAlphaThreshold = 230;
constexpr int kTransferSyntheticWidth = 420;
constexpr int kTransferSyntheticHeight = 320;
constexpr int kTransferSyntheticPitch = 69;
constexpr int kTransferSyntheticCellSize = 64;

struct SingleRoiFixture
{
    cv::Mat pixels;
    MaaRect roi { kSyntheticRoiX, kSyntheticRoiY, kSyntheticRoiSize, kSyntheticRoiSize };
};

SingleRoiFixture MakeSingleRoiFixture(std::string_view icon_id = "item_copper_ore", int rarity = 1)
{
    const auto template_path = get_exe_dir() / ".." / "resource" / "image" / "IconRecognition" / std::to_string(rarity)
                               / (std::string(icon_id) + ".png");
    const iconrecognition::detail::TemplateRecord record { .item_id = std::string(icon_id) };
    const auto prepared = iconrecognition::detail::PrepareStandardTemplate(
        record,
        iconrecognition::detail::DecodeBgra(template_path),
        kSyntheticRoiSize,
        kSyntheticAlphaThreshold);
    const cv::Size canvas_size { kSyntheticRoiX + kSyntheticRoiSize + 5, kSyntheticRoiY + kSyntheticRoiSize + 5 };
    cv::Mat pixels = cv::Mat::zeros(canvas_size, CV_8UC3);
    prepared.image.copyTo(pixels(cv::Rect(kSyntheticRoiX, kSyntheticRoiY, kSyntheticRoiSize, kSyntheticRoiSize)));
    return { std::move(pixels) };
}

struct RecheckDeduplicateFixture
{
    cv::Mat pixels;
    cv::Rect roi { 0, 0, kTransferSyntheticWidth, kTransferSyntheticHeight };
};

struct RegionRestrictedFixture
{
    std::filesystem::path data_root;
    cv::Mat normal_pixels;
    cv::Mat disabled_pixels;
    cv::Rect roi;
};

RegionRestrictedFixture MakeRegionRestrictedFixture(std::string_view fixture_name, bool write_overlays)
{
    const auto root = std::filesystem::path("agent/cpp-algo/source/IconRecognition/test/build") / fixture_name;
    std::filesystem::remove_all(root);
    const auto data_root = root / "data" / "IconRecognition";
    const auto image_root = root / "resource" / "image" / "IconRecognition";
    std::filesystem::create_directories(data_root);
    std::filesystem::create_directories(image_root / "1");
    std::ofstream(data_root / "recognition_items.json", std::ios::binary | std::ios::trunc)
        << R"({"restricted":{"name":"受限物品","category":"test","storageKind":"Normal","categoryType":"Ore","rarity":1,"iconId":"restricted","fluidIconId":"","sortId1":-80,"sortId2":2,"regionRestricted":true},"restricted_alias":{"name":"受限物品别名","category":"test","storageKind":"Normal","categoryType":"Ore","rarity":1,"iconId":"restricted","fluidIconId":"","sortId1":-80,"sortId2":1,"regionRestricted":true}})";

    const cv::Mat source =
        iconrecognition::detail::DecodeBgra(get_exe_dir() / ".." / "resource" / "image" / "IconRecognition" / "1" / "item_copper_ore.png");
    Require(cv::imwrite((image_root / "1" / "restricted.png").string(), source), "unable to write restricted icon fixture");
    const cv::Mat dark_band(28, 120, CV_8UC4, cv::Scalar(0, 0, 0, 255));
    const cv::Mat white_mark(24, 24, CV_8UC4, cv::Scalar(255, 255, 255, 255));
    if (write_overlays) {
        std::filesystem::create_directories(image_root / "Overlay");
        Require(
            cv::imwrite((image_root / "Overlay" / "icon_placement_disabled_bg.png").string(), dark_band),
            "unable to write restricted background fixture");
        Require(
            cv::imwrite((image_root / "Overlay" / "icon_placement_disabled.png").string(), white_mark),
            "unable to write restricted mark fixture");
    }

    const iconrecognition::detail::TemplateRecord record { .item_id = "restricted", .region_restricted = true };
    const auto normal =
        iconrecognition::detail::PrepareStandardTemplate(record, source, kTransferSyntheticCellSize, kSyntheticAlphaThreshold);
    const auto disabled = iconrecognition::detail::BuildRegionUnavailableTemplate(normal, dark_band, white_mark, kSyntheticAlphaThreshold);
    const cv::Rect roi { 0, 0, kTransferSyntheticWidth, kTransferSyntheticHeight };
    const cv::Point cell_origin { 2, 2 };
    cv::Mat grid_background = cv::Mat::zeros(roi.size(), CV_8UC3);
    for (int x = 0; x < roi.width; x += kTransferSyntheticPitch) {
        grid_background.colRange(x, std::min(x + 2, roi.width)).setTo(cv::Scalar(245, 245, 245));
    }
    for (int y = 0; y < roi.height; y += kTransferSyntheticPitch) {
        grid_background.rowRange(y, std::min(y + 2, roi.height)).setTo(cv::Scalar(245, 245, 245));
    }
    const cv::Size canvas_size = roi.size();
    cv::Mat normal_pixels = cv::Mat::zeros(canvas_size, CV_8UC3);
    cv::Mat disabled_pixels = cv::Mat::zeros(canvas_size, CV_8UC3);
    grid_background.copyTo(normal_pixels);
    grid_background.copyTo(disabled_pixels);
    normal.image.copyTo(normal_pixels(cv::Rect(cell_origin, normal.image.size())));
    disabled.image.copyTo(disabled_pixels(cv::Rect(cell_origin, disabled.image.size())));
    return { data_root, std::move(normal_pixels), std::move(disabled_pixels), roi };
}

RecheckDeduplicateFixture MakeRecheckDeduplicateFixture()
{
    cv::Mat pixels(kTransferSyntheticHeight, kTransferSyntheticWidth, CV_8UC3, cv::Scalar(18, 18, 18));
    for (int x = 0; x < kTransferSyntheticWidth; x += kTransferSyntheticPitch) {
        pixels.colRange(x, std::min(x + 2, kTransferSyntheticWidth)).setTo(cv::Scalar(245, 245, 245));
    }
    for (int y = 0; y < kTransferSyntheticHeight; y += kTransferSyntheticPitch) {
        pixels.rowRange(y, std::min(y + 2, kTransferSyntheticHeight)).setTo(cv::Scalar(245, 245, 245));
    }

    const auto template_path = get_exe_dir() / ".." / "resource" / "image" / "IconRecognition" / "1" / "item_copper_ore.png";
    const iconrecognition::detail::TemplateRecord record { .item_id = "item_copper_ore" };
    const auto prepared = iconrecognition::detail::PrepareStandardTemplate(
        record,
        iconrecognition::detail::DecodeBgra(template_path),
        kTransferSyntheticCellSize,
        kSyntheticAlphaThreshold);
    for (const cv::Point origin : { cv::Point { 2, 2 }, cv::Point { 71, 2 } }) {
        prepared.image.copyTo(pixels(cv::Rect(origin.x, origin.y, kTransferSyntheticCellSize, kTransferSyntheticCellSize)));
    }
    return { std::move(pixels) };
}

RecheckDeduplicateFixture MakeRecheckAdditionalFilterFixture()
{
    auto fixture = MakeRecheckDeduplicateFixture();
    const auto template_path = get_exe_dir() / ".." / "resource" / "image" / "IconRecognition" / "2" / "item_carbon_mtl.png";
    const iconrecognition::detail::TemplateRecord record { .item_id = "item_carbon_mtl" };
    const auto prepared = iconrecognition::detail::PrepareStandardTemplate(
        record,
        iconrecognition::detail::DecodeBgra(template_path),
        kTransferSyntheticCellSize,
        kSyntheticAlphaThreshold);
    prepared.image.copyTo(fixture.pixels(cv::Rect(71, 2, kTransferSyntheticCellSize, kTransferSyntheticCellSize)));
    return fixture;
}

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
    const cv::Mat pixels(64, 64, CV_8UC3, cv::Scalar(0, 0, 0));
    image.set(pixels);
    MaaRect out_box { 101, 202, 303, 404 };
    const auto detail = RunFailure(image.get(), R"({"grid_type":"unknown"})", out_box);
    Require(ErrorMessage(detail).find("grid_type") != std::string::npos, "unknown grid type error must identify grid_type");
    RequireUntouched(out_box);
}

void TestRequiredParametersAreRejected()
{
    ImageBuffer image;
    const cv::Mat pixels(720, 1280, CV_8UC3, cv::Scalar(0, 0, 0));
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
    const cv::Mat pixels(64, 64, CV_8UC3, cv::Scalar(0, 0, 0));
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
    const cv::Mat pixels(64, 64, CV_8UC3, cv::Scalar(0, 0, 0));
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
    const cv::Mat pixels(64, 64, CV_8UC3, cv::Scalar(0, 0, 0));
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
    const cv::Mat pixels(64, 64, CV_8UC3, cv::Scalar(0, 0, 0));
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

void TestGridDetectionFailureIsStructured()
{
    ImageBuffer image;
    const cv::Mat pixels(720, 1280, CV_8UC3, cv::Scalar(0, 0, 0));
    image.set(pixels);
    const MaaRect roi { 154, 202, 585, 291 };
    MaaRect out_box { 101, 202, 303, 404 };

    const auto detail = RunFailure(image.get(), R"({"grid_type":"transfer"})", out_box, &roi);
    Require(ErrorCode(detail) == "grid_detection_failed", "grid detection miss must use the structured error code");
    RequireUntouched(out_box);
}

void TestSuccessfulSingleRoiUsesPrimaryCellBox()
{
    const auto fixture = MakeSingleRoiFixture();
    ImageBuffer image;
    image.set(fixture.pixels);
    StringBuffer detail_buffer;
    MaaRect out_box { 101, 202, 303, 404 };
    const MaaBool matched = iconrecognition::IconRecognitionRun(
        nullptr,
        0,
        "IconRecognitionTest",
        "IconRecognition",
        R"({"grid_type":"single_roi","item_filters":["Normal:Ore"]})",
        image.get(),
        &fixture.roi,
        nullptr,
        &out_box,
        detail_buffer.get());
    Require(matched, "representative single ROI request must match");
    const auto detail = detail_buffer.detail();
    Require(
        detail.contains("matched") && detail.at("matched").is_boolean() && detail.at("matched").as_boolean(),
        "successful detail must be matched");
    Require(
        detail.contains("grid_type") && detail.at("grid_type").as_string() == "single_roi",
        "single_roi success must preserve grid_type");
    Require(
        detail.contains("matches") && detail.at("matches").is_array() && detail.at("matches").as_array().size() == 1,
        "single_roi success must contain one match");
    const auto& match = detail.at("matches").as_array().at(0).as_object();
    Require(match.contains("cell_box") && match.at("cell_box").is_array(), "successful match must contain array cell_box");
    Require(match.contains("item_box") && match.at("item_box").is_array(), "successful match must contain array item_box");
    Require(match.contains("score") && match.at("score").is_number(), "successful match must contain score");
    Require(!match.contains("region_unavailable"), "normal successful match must omit region_unavailable");
    const auto& cell_box = match.at("cell_box").as_array();
    Require(cell_box.size() == 4, "cell_box must contain four components");
    Require(out_box.x == cell_box.at(0).as_integer(), "out_box.x must equal primary cell_box.x");
    Require(out_box.y == cell_box.at(1).as_integer(), "out_box.y must equal primary cell_box.y");
    Require(out_box.width == cell_box.at(2).as_integer(), "out_box.width must equal primary cell_box.width");
    Require(out_box.height == cell_box.at(3).as_integer(), "out_box.height must equal primary cell_box.height");
}

void TestItemRecheckFiltersValidateCandidates()
{
    const auto fixture = MakeSingleRoiFixture();
    ImageBuffer image;
    image.set(fixture.pixels);
    const char* success_param =
        R"({"grid_type":"single_roi","item_ids":["item_copper_ore"],"item_filters":["Normal:Ore"],"item_recheck_filters":["Normal:Ore"]})";
    MaaRect out_box { 101, 202, 303, 404 };
    StringBuffer success_detail;
    Require(
        iconrecognition::IconRecognitionRun(
            nullptr,
            0,
            "IconRecognitionTest",
            "IconRecognition",
            success_param,
            image.get(),
            &fixture.roi,
            nullptr,
            &out_box,
            success_detail.get()),
        "matching item_recheck_filters must preserve the candidate");
    Require(success_detail.detail().at("matches").as_array().size() == 1, "successful reverse check must keep one match");

    const char* failure_param =
        R"({"grid_type":"single_roi","item_ids":["item_copper_ore"],"item_filters":["Normal:Ore"],"item_recheck_filters":["Normal:Product"]})";
    out_box = MaaRect { 101, 202, 303, 404 };
    const auto failure = RunFailure(image.get(), failure_param, out_box, &fixture.roi);
    Require(ErrorCode(failure) == "no_match", "mismatching reverse check must reject the candidate");
    RequireUntouched(out_box);

    const char* invalid_param =
        R"({"grid_type":"single_roi","item_ids":["item_copper_ore"],"item_filters":["Normal:Ore"],"item_recheck_filters":["invalid"]})";
    out_box = MaaRect { 101, 202, 303, 404 };
    const auto invalid = RunFailure(image.get(), invalid_param, out_box, &fixture.roi);
    Require(ErrorCode(invalid) == "invalid_argument", "malformed reverse-check filter must be rejected as invalid input");
    Require(ErrorMessage(invalid).find("item_recheck_filters") != std::string::npos, "invalid filter error must identify its field");
    RequireUntouched(out_box);
}

void TestItemRecheckAcceptsAliasWithSameIconIdentity()
{
    const auto fixture = MakeSingleRoiFixture("item_weekraid_collection_1_1", 2);
    iconrecognition::IconRecognizer recognizer(get_exe_dir() / ".." / "data" / "IconRecognition");
    Require(recognizer.initialize(), "alias recheck recognizer must initialize");

    iconrecognition::RecognitionRequest request;
    request.grid_type = iconrecognition::GridType::SingleRoi;
    request.roi = cv::Rect(fixture.roi.x, fixture.roi.y, fixture.roi.width, fixture.roi.height);
    request.candidates.item_ids = { "item_weekraid_collection_1_1" };
    request.candidates.item_filters = { "Normal:Ore" };
    request.candidates.item_recheck_filters = { "ValuableDepot:CommercialItem" };

    const auto result = recognizer.recognize(fixture.pixels, request);
    Require(result.matched && result.matches.size() == 1, "recheck alias with the same icon identity must match");
    Require(
        result.matches.front().item.item_id == "item_weekraid_collection_1_1",
        "alias recheck must preserve the originally requested item id");
}

void TestItemRecheckFiltersRespectDeduplication()
{
    const auto fixture = MakeRecheckDeduplicateFixture();
    iconrecognition::IconRecognizer recognizer(get_exe_dir() / ".." / "data" / "IconRecognition");
    Require(recognizer.initialize(), "deduplication recognizer must initialize");

    iconrecognition::RecognitionRequest request;
    request.grid_type = iconrecognition::GridType::Transfer;
    request.roi = fixture.roi;
    request.candidates.item_ids = { "item_copper_ore" };
    request.candidates.item_filters = { "Normal:Ore" };
    request.candidates.item_recheck_filters = { "Normal:Ore" };

    const auto without_deduplicate = recognizer.recognize(fixture.pixels, request);
    Require(without_deduplicate.matched, "both rechecked duplicate candidates must match without deduplication");
    Require(without_deduplicate.matches.size() == 2, "deduplicate=false must keep both candidates with the same item_id");
    Require(
        std::ranges::all_of(without_deduplicate.matches, [](const auto& match) { return match.item.item_id == "item_copper_ore"; }),
        "deduplicate=false candidates must share the requested item_id");

    request.deduplicate = true;
    const auto with_deduplicate = recognizer.recognize(fixture.pixels, request);
    Require(with_deduplicate.matched, "one rechecked duplicate candidate must match with deduplication");
    Require(with_deduplicate.matches.size() == 1, "deduplicate=true must keep only one candidate with the same item_id");
}

void TestItemRecheckFiltersIgnoreAdditionalFilterMatches()
{
    const auto fixture = MakeRecheckAdditionalFilterFixture();
    iconrecognition::IconRecognizer recognizer(get_exe_dir() / ".." / "data" / "IconRecognition");
    Require(recognizer.initialize(), "additional-filter recheck recognizer must initialize");

    iconrecognition::RecognitionRequest request;
    request.grid_type = iconrecognition::GridType::Transfer;
    request.roi = fixture.roi;
    request.candidates.item_ids = { "item_copper_ore" };
    request.candidates.item_filters = { "Normal:Ore" };
    request.candidates.additional_item_filters = { "Normal:Product" };
    request.candidates.item_recheck_filters = { "Normal:Ore" };

    const auto result = recognizer.recognize(fixture.pixels, request);
    Require(result.matched, "original ids and additional-filter matches must both be recognized");
    Require(result.matches.size() == 2, "item_recheck_filters must not reject matches added by additional_item_filters");
    Require(
        std::ranges::any_of(result.matches, [](const auto& match) { return match.item.item_id == "item_copper_ore"; }),
        "rechecked original item id must remain in the result");
    Require(
        std::ranges::any_of(result.matches, [](const auto& match) { return match.item.item_id == "item_carbon_mtl"; }),
        "additional filter match must bypass item-id recheck");
}

void TestRecognizerPreservesInternalDiagnostics()
{
    const auto fixture = MakeSingleRoiFixture();
    iconrecognition::IconRecognizer recognizer(get_exe_dir() / ".." / "data" / "IconRecognition");
    Require(recognizer.initialize(), "diagnostics recognizer must initialize");
    iconrecognition::RecognitionRequest request;
    request.grid_type = iconrecognition::GridType::SingleRoi;
    request.roi = cv::Rect(fixture.roi.x, fixture.roi.y, fixture.roi.width, fixture.roi.height);
    request.candidates.item_filters = { "Normal:Ore" };
    request.debug = true;
    const auto result = recognizer.recognize(fixture.pixels, request);
    Require(result.matched && result.matches.size() == 1, "diagnostics fixture must match one item");
    Require(result.diagnostics && result.diagnostics->cells.size() == 1, "fixed ROI recognition must preserve one internal diagnostic");
    const auto& diagnostic = result.diagnostics->cells.front();
    Require(diagnostic.best_candidate_id == result.matches.front().item.item_id, "diagnostic candidate id must match the public item");
    Require(diagnostic.candidate_box == result.matches.front().item_box, "diagnostic candidate box must match the public item box");
    Require(diagnostic.score == result.matches.front().score, "diagnostic final score must match the public score");
    Require(diagnostic.candidate_count > 1, "diagnostics must preserve the ranked candidate count");
    Require(diagnostic.mask_kind == "lower_extended", "fixed ROI must report the lower_extended mask");
    Require(result.diagnostics->performance.has_value(), "debug recognition must preserve performance diagnostics");
    const auto& performance = *result.diagnostics->performance;
    Require(performance.cell_count == 1, "performance diagnostics must preserve the recognized cell count");
    Require(performance.ranking.baseline_candidates > 1, "performance diagnostics must count baseline candidates");
    Require(performance.matcher.score_calls >= performance.ranking.baseline_candidates, "matcher diagnostics must count score calls");
    Require(performance.total_ms >= performance.ranking.total_ms, "total recognition time must cover ranking time");
    Require(!json::value(result).as_object().contains("diagnostics"), "public detail must not serialize internal diagnostics");
}

void TestRegionRestrictedFallbackRunsOnlyAfterNormalRejection()
{
    const auto normal_fixture = MakeRegionRestrictedFixture("generated-region-restricted-normal", true);
    iconrecognition::IconRecognizer normal_recognizer(normal_fixture.data_root);
    Require(normal_recognizer.initialize(), "normal restricted recognizer must initialize");
    iconrecognition::RecognitionRequest request;
    request.grid_type = iconrecognition::GridType::Transfer;
    request.roi = normal_fixture.roi;
    request.candidates.item_ids = { "restricted" };
    request.candidates.item_filters = { "Normal:Ore" };
    request.recognize_region_unavailable = true;
    request.threshold = 0.98;

    const auto normal_result = normal_recognizer.recognize(normal_fixture.normal_pixels, request);
    Require(normal_result.matched && normal_result.matches.size() == 1, "normal template must match before disabled fallback");
    Require(
        !normal_result.matches.front().region_unavailable,
        "normal template match must not be marked unavailable in the current region");
    Require(
        normal_result.matches.front().item.aliases.size() == 1
            && normal_result.matches.front().item.aliases.front().item_id == "restricted_alias",
        "normal representative must retain its shared-icon alias");
    Require(
        normal_result.diagnostics && !normal_result.diagnostics->cells.front().region_unavailable_fallback_used,
        "accepted normal match must not run disabled fallback");

    auto alias_request = request;
    alias_request.candidates.item_ids = { "restricted_alias" };
    const auto alias_result = normal_recognizer.recognize(normal_fixture.normal_pixels, alias_request);
    Require(alias_result.matched && alias_result.matches.size() == 1, "exact alias request must recognize one item");
    Require(alias_result.matches.front().item.item_id == "restricted_alias", "exact alias request must return its requested id");
    Require(
        alias_result.matches.front().item.aliases.size() == 1
            && alias_result.matches.front().item.aliases.front().item_id == "restricted",
        "exact alias request must retain the other shared-icon item as an alias");

    const auto disabled_fixture = MakeRegionRestrictedFixture("generated-region-restricted-disabled", true);
    iconrecognition::IconRecognizer disabled_recognizer(disabled_fixture.data_root);
    Require(disabled_recognizer.initialize(), "disabled restricted recognizer must initialize");
    request.roi = disabled_fixture.roi;
    request.recognize_region_unavailable = false;
    const auto disabled_by_default = disabled_recognizer.recognize(disabled_fixture.disabled_pixels, request);
    Require(!disabled_by_default.matched, "disabled template must not match when fallback remains disabled");

    request.recognize_region_unavailable = true;
    const auto disabled_result = disabled_recognizer.recognize(disabled_fixture.disabled_pixels, request);
    Require(disabled_result.matched && disabled_result.matches.size() == 1, "enabled fallback must recognize the disabled template");
    Require(disabled_result.matches.front().item.item_id == "restricted", "disabled fallback must preserve the original item id");
    Require(disabled_result.matches.front().region_unavailable, "region-unavailable fallback match must preserve its state");
    Require(
        disabled_result.matches.front().item.aliases.size() == 1
            && disabled_result.matches.front().item.aliases.front().item_id == "restricted_alias",
        "region-unavailable fallback must retain the representative aliases");
    Require(
        disabled_result.diagnostics && disabled_result.diagnostics->cells.front().region_unavailable_fallback_used,
        "disabled fallback diagnostics must record that the fallback was used");

    request.candidates.item_recheck_filters = { "Normal:Ore" };
    const auto rechecked_disabled_result = disabled_recognizer.recognize(disabled_fixture.disabled_pixels, request);
    Require(
        rechecked_disabled_result.matched && rechecked_disabled_result.matches.size() == 1,
        "item recheck must preserve a disabled fallback match");
    Require(rechecked_disabled_result.matches.front().region_unavailable, "rechecked fallback must retain region_unavailable=true");

    // 地区禁用后备不应随着开关误应用到交易、奖励或单 ROI 等其他界面。
    request.grid_type = iconrecognition::GridType::SingleRoi;
    request.roi = cv::Rect { 2, 2, kTransferSyntheticCellSize, kTransferSyntheticCellSize };
    const auto unsupported_grid_result = disabled_recognizer.recognize(disabled_fixture.disabled_pixels, request);
    Require(!unsupported_grid_result.matched, "unsupported grid types must not use region-restricted fallback");
}

void TestRegionRestrictedPreloadHonorsEnabledRequest()
{
    const auto fixture = MakeRegionRestrictedFixture("generated-region-restricted-preload", true);
    iconrecognition::IconRecognizer recognizer(fixture.data_root);
    Require(recognizer.initialize(), "preload recognizer must initialize without loading region-restricted overlays");

    iconrecognition::RecognitionRequest enabled_request;
    enabled_request.grid_type = iconrecognition::GridType::Transfer;
    enabled_request.roi = fixture.roi;
    enabled_request.candidates.item_ids = { "restricted" };
    enabled_request.candidates.item_filters = { "Normal:Ore" };
    enabled_request.recognize_region_unavailable = true;
    enabled_request.threshold = 0.98;
    Require(recognizer.preload({ enabled_request }), "enabled preload must prepare region-unavailable variants");

    Require(
        std::filesystem::remove(
            fixture.data_root.parent_path().parent_path() / "resource" / "image" / "IconRecognition" / "Overlay"
            / "icon_placement_disabled.png"),
        "preload fixture must remove one overlay asset after warm-up");
    const auto result = recognizer.recognize(fixture.disabled_pixels, enabled_request);
    Require(
        result.matched && result.matches.size() == 1 && result.matches.front().region_unavailable,
        "recognition after removing the asset must use the preloaded region-unavailable template");
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

struct FrameworkRecognitionResult
{
    bool hit = false;
    MaaRect box {};
    json::object detail;
};

FrameworkRecognitionResult RunFrameworkRecognition(FrameworkFixture& fixture, const cv::Mat& pixels, const char* param)
{
    ImageBuffer image;
    image.set(pixels);
    const MaaTaskId task_id = MaaTaskerPostRecognition(fixture.tasker(), "Custom", param, image.get());
    Require(task_id != 0, "MaaTaskerPostRecognition must return a task id");
    Require(MaaTaskerWait(fixture.tasker(), task_id) == MaaStatus_Succeeded, "MaaFramework recognition task must succeed");

    StringBuffer entry;
    MaaStatus task_status = MaaStatus_Invalid;
    MaaSize node_count = 0;
    Require(
        MaaTaskerGetTaskDetail(fixture.tasker(), task_id, entry.get(), nullptr, &node_count, &task_status),
        "MaaTaskerGetTaskDetail must report node count");
    Require(node_count > 0, "MaaFramework recognition task must contain a node");
    std::vector<MaaNodeId> node_ids(node_count);
    Require(
        MaaTaskerGetTaskDetail(fixture.tasker(), task_id, entry.get(), node_ids.data(), &node_count, &task_status),
        "MaaTaskerGetTaskDetail must return node ids");
    StringBuffer node_detail_name;
    MaaRecoId reco_id = 0;
    MaaActId action_id = 0;
    MaaBool completed = 0;
    Require(
        MaaTaskerGetNodeDetail(fixture.tasker(), node_ids.front(), node_detail_name.get(), &reco_id, &action_id, &completed),
        "MaaTaskerGetNodeDetail must return reco id");
    Require(reco_id != 0, "MaaFramework recognition node must expose reco id");

    StringBuffer node_name;
    StringBuffer algorithm;
    StringBuffer detail_buffer;
    ImageBuffer raw;
    ImageListBuffer draws;
    MaaBool hit = 0;
    MaaRect box {};
    Require(
        MaaTaskerGetRecognitionDetail(
            fixture.tasker(),
            reco_id,
            node_name.get(),
            algorithm.get(),
            &hit,
            &box,
            detail_buffer.get(),
            raw.get(),
            draws.get()),
        "MaaTaskerGetRecognitionDetail must succeed");
    Require(
        MaaImageBufferWidth(raw.get()) == pixels.cols && MaaImageBufferHeight(raw.get()) == pixels.rows,
        "MaaFramework raw must preserve the full input size");
    Require(
        cv::norm(
            cv::Mat(pixels),
            cv::Mat(
                MaaImageBufferHeight(raw.get()),
                MaaImageBufferWidth(raw.get()),
                MaaImageBufferType(raw.get()),
                MaaImageBufferGetRawData(raw.get())),
            cv::NORM_INF)
            == 0.0,
        "MaaFramework raw must remain unannotated");
    return FrameworkRecognitionResult { hit != 0, box, detail_buffer.detail() };
}

void TestMaaFrameworkWrapsCustomDetail()
{
    MaaBool debug_mode = 1;
    Require(MaaGlobalSetOption(MaaGlobalOption_DebugMode, &debug_mode, sizeof(debug_mode)), "MaaFramework debug mode must enable");
    FrameworkFixture fixture;
    const auto fixture_image = MakeSingleRoiFixture();

    const auto success = RunFrameworkRecognition(
        fixture,
        fixture_image.pixels,
        R"({"custom_recognition":"IconRecognition","roi":[7,5,54,54],"custom_recognition_param":{"grid_type":"single_roi","item_filters":["Normal:Ore"]}})");
    Require(success.hit, "MaaFramework success result must hit");
    Require(success.detail.at("all").as_array().size() == 1, "MaaFramework all must contain the custom result");
    Require(success.detail.at("filtered").as_array().size() == 1, "MaaFramework filtered must contain the hit");
    Require(success.detail.at("best").is_object(), "MaaFramework best must contain the hit");
    const auto& best = success.detail.at("best").as_object();
    const auto& best_box = best.at("box").as_array();
    Require(best_box.size() == 4, "MaaFramework best.box must contain four components");
    Require(success.box.x == best_box.at(0).as_integer(), "MaaFramework best.box.x must match callback out_box");
    Require(success.box.y == best_box.at(1).as_integer(), "MaaFramework best.box.y must match callback out_box");
    Require(success.box.width == best_box.at(2).as_integer(), "MaaFramework best.box.width must match callback out_box");
    Require(success.box.height == best_box.at(3).as_integer(), "MaaFramework best.box.height must match callback out_box");
    Require(best.at("detail").as_object().at("matches").as_array().size() == 1, "MaaFramework best.detail must preserve complete matches");

    const cv::Mat rejected_pixels = cv::Mat::zeros(fixture_image.pixels.size(), fixture_image.pixels.type());
    const auto failure = RunFrameworkRecognition(
        fixture,
        rejected_pixels,
        R"({"custom_recognition":"IconRecognition","roi":[7,5,54,54],"custom_recognition_param":{"grid_type":"single_roi","item_filters":["Normal:Ore"]}})");
    Require(!failure.hit, "MaaFramework rejected result must not hit");
    Require(failure.detail.at("all").as_array().size() == 1, "MaaFramework all must retain rejected detail");
    Require(failure.detail.at("filtered").as_array().empty(), "MaaFramework filtered must be empty on rejection");
    Require(failure.detail.at("best").is_null(), "MaaFramework best must be null on rejection");

    debug_mode = 0;
    Require(MaaGlobalSetOption(MaaGlobalOption_DebugMode, &debug_mode, sizeof(debug_mode)), "MaaFramework debug mode must restore");
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
        TestGridDetectionFailureIsStructured();
        TestSuccessfulSingleRoiUsesPrimaryCellBox();
        TestItemRecheckFiltersValidateCandidates();
        TestItemRecheckAcceptsAliasWithSameIconIdentity();
        TestItemRecheckFiltersRespectDeduplication();
        TestItemRecheckFiltersIgnoreAdditionalFilterMatches();
        TestRecognizerPreservesInternalDiagnostics();
        TestRegionRestrictedFallbackRunsOnlyAfterNormalRejection();
        TestRegionRestrictedPreloadHonorsEnabledRequest();
        TestGridDiagnosticsSerializeSelectionEvidence();
        TestRecognizerPreloadsEveryRequestedTemplateSize();
        TestMaaFrameworkWrapsCustomDetail();
        std::cout << "IconRecognition custom recognition tests passed\n";
        return 0;
    }
    catch (const std::exception& error) {
        std::cerr << error.what() << '\n';
        return 1;
    }
}
