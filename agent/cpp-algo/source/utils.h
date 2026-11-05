#pragma once

#include <MaaFramework/MaaAPI.h>
#include <MaaUtils/NoWarningCV.hpp>

inline cv::Mat to_mat(const MaaImageBuffer* buffer)
{
    return cv::Mat(MaaImageBufferHeight(buffer), MaaImageBufferWidth(buffer), MaaImageBufferType(buffer), MaaImageBufferGetRawData(buffer));
}

// RAII owners for the C-API Maa*Buffer handles. The framework hands these out via *Create and requires a
// matching *Destroy; these wrap that lifetime so callers never leak on an early return. Shared here because
// every screencap/recognition caller needs the exact same boilerplate (previously copy-pasted into ~5 TUs).
class ScopedImageBuffer
{
public:
    ScopedImageBuffer()
        : buffer_(MaaImageBufferCreate())
    {
    }

    ~ScopedImageBuffer()
    {
        if (buffer_ != nullptr) {
            MaaImageBufferDestroy(buffer_);
        }
    }

    ScopedImageBuffer(const ScopedImageBuffer&) = delete;
    ScopedImageBuffer& operator=(const ScopedImageBuffer&) = delete;

    MaaImageBuffer* Get() const { return buffer_; }

private:
    MaaImageBuffer* buffer_ = nullptr;
};

class ScopedStringBuffer
{
public:
    ScopedStringBuffer()
        : buffer_(MaaStringBufferCreate())
    {
    }

    ~ScopedStringBuffer()
    {
        if (buffer_ != nullptr) {
            MaaStringBufferDestroy(buffer_);
        }
    }

    ScopedStringBuffer(const ScopedStringBuffer&) = delete;
    ScopedStringBuffer& operator=(const ScopedStringBuffer&) = delete;

    MaaStringBuffer* Get() const { return buffer_; }

private:
    MaaStringBuffer* buffer_ = nullptr;
};

class ScopedStringListBuffer
{
public:
    ScopedStringListBuffer()
        : buffer_(MaaStringListBufferCreate())
    {
    }

    ~ScopedStringListBuffer()
    {
        if (buffer_ != nullptr) {
            MaaStringListBufferDestroy(buffer_);
        }
    }

    ScopedStringListBuffer(const ScopedStringListBuffer&) = delete;
    ScopedStringListBuffer& operator=(const ScopedStringListBuffer&) = delete;

    MaaStringListBuffer* Get() const { return buffer_; }

private:
    MaaStringListBuffer* buffer_ = nullptr;
};
