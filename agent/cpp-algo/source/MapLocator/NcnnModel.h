#pragma once

#ifdef MAAEND_WITH_NCNN

#include <cstddef>
#include <filesystem>
#include <memory>
#include <vector>

namespace maplocator
{

// Android 地图模型的单输入单输出 ncnn 推理（CPU、fp32）。
// 后端在构建时固定，缺模型或加载失败直接报错，不回退 onnxruntime。
class NcnnModel
{
public:
    // param_path 指向 <stem>.ncnn.param；权重位于同目录的 <stem>.ncnn.bin。
    static std::unique_ptr<NcnnModel> load(const std::filesystem::path& param_path, int threads, const char* tag);

    ~NcnnModel();

    // chw：channels × height × width 连续 float32。
    // 只接受长度为 expected_output_size 的单样本 float32 向量，成功后复制到 out。
    bool run(const float* chw, int channels, int height, int width, size_t expected_output_size, std::vector<float>& out) const;

private:
    NcnnModel();

    struct Impl;
    std::unique_ptr<Impl> impl_;
};

} // namespace maplocator

#endif
