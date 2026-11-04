#include "NcnnModel.h"

#ifdef MAAEND_WITH_NCNN

#include <algorithm>
#include <cmath>
#include <cstring>
#include <limits>
#include <string>

#include <ncnn/net.h>

#include <MaaUtils/Logger.h>
#include <MaaUtils/Platform.h>

namespace maplocator
{

struct NcnnModel::Impl
{
    ncnn::Net net;
    std::string input_name;
    std::string output_name;
};

NcnnModel::NcnnModel()
    : impl_(std::make_unique<Impl>())
{
}

NcnnModel::~NcnnModel() = default;

std::unique_ptr<NcnnModel> NcnnModel::load(const std::filesystem::path& param_path, int threads, const char* tag)
{
    auto bin_path = param_path;
    bin_path.replace_extension(".bin");
    const std::string param = MAA_NS::path_to_utf8_string(param_path);
    const std::string bin = MAA_NS::path_to_utf8_string(bin_path);

    std::unique_ptr<NcnnModel> model(new NcnnModel());
    ncnn::Option& opt = model->impl_->net.opt;
    opt.use_vulkan_compute = false;
    // fp32：概率输出要与 onnxruntime 对齐（下游有置信度阈值与次峰门限）。
    opt.use_fp16_packed = false;
    opt.use_fp16_storage = false;
    opt.use_fp16_arithmetic = false;
    opt.use_bf16_storage = false;
    opt.num_threads = std::max(1, threads);

    if (model->impl_->net.load_param(param.c_str()) != 0 || model->impl_->net.load_model(bin.c_str()) != 0) {
        LogError << "ncnn model is incomplete or failed to load" << VAR(tag) << VAR(param) << VAR(bin);
        return nullptr;
    }

    const auto& inputs = model->impl_->net.input_names();
    const auto& outputs = model->impl_->net.output_names();
    if (inputs.size() != 1 || outputs.size() != 1) {
        LogError << "ncnn model must have exactly one input and one output" << VAR(tag) << VAR(inputs.size()) << VAR(outputs.size());
        return nullptr;
    }
    model->impl_->input_name = inputs.front();
    model->impl_->output_name = outputs.front();

    LogInfo << "ncnn model loaded" << VAR(tag) << VAR(param) << VAR(opt.num_threads);
    return model;
}

bool NcnnModel::run(const float* chw, int channels, int height, int width, size_t expected_output_size, std::vector<float>& out) const
{
    out.clear();
    if (chw == nullptr || channels <= 0 || height <= 0 || width <= 0 || expected_output_size == 0
        || expected_output_size > static_cast<size_t>(std::numeric_limits<int>::max())) {
        LogError << "ncnn inference input or output contract is invalid" << VAR(channels) << VAR(height) << VAR(width)
                 << VAR(expected_output_size);
        return false;
    }

    // ncnn::Mat 的通道按 cstep 对齐，逐通道拷贝而不是直接包装外部内存。
    ncnn::Mat input(width, height, channels);
    if (input.empty()) {
        LogError << "ncnn input allocation failed";
        return false;
    }
    const size_t plane = static_cast<size_t>(width) * static_cast<size_t>(height);
    for (int c = 0; c < channels; ++c) {
        std::memcpy(input.channel(c), chw + static_cast<size_t>(c) * plane, plane * sizeof(float));
    }

    ncnn::Extractor extractor = impl_->net.create_extractor();
    if (extractor.input(impl_->input_name.c_str(), input) != 0) {
        LogError << "ncnn failed to set input";
        return false;
    }
    ncnn::Mat output;
    // 默认 type=0 会解包并转回 float32；在语义解码之前仍校验实际表示与单样本向量形状。
    if (extractor.extract(impl_->output_name.c_str(), output) != 0 || output.empty()) {
        LogError << "ncnn failed to extract output";
        return false;
    }
    if (output.elempack != 1 || output.elemsize != sizeof(float) || output.w != static_cast<int>(expected_output_size) || output.h != 1
        || output.d != 1 || output.c != 1) {
        LogError << "ncnn output does not match the classification vector contract" << VAR(expected_output_size) << VAR(output.w)
                 << VAR(output.h) << VAR(output.d) << VAR(output.c) << VAR(output.elempack) << VAR(output.elemsize);
        return false;
    }

    const ncnn::Mat flat = output.reshape(static_cast<int>(expected_output_size));
    if (flat.empty()) {
        LogError << "ncnn output reshape failed";
        return false;
    }
    const float* data = flat;
    if (std::any_of(data, data + expected_output_size, [](float value) { return !std::isfinite(value) || value < 0.0f; })) {
        LogError << "ncnn classification output contains invalid probabilities";
        return false;
    }
    out.assign(data, data + expected_output_size);
    return true;
}

} // namespace maplocator

#endif
