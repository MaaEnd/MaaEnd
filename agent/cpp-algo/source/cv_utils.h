#pragma once

#include <algorithm>
#include <stdexcept>
#include <string>
#include <vector>

#include <MaaUtils/Logger.h>
#include <MaaUtils/NoWarningCV.hpp>

inline cv::Scalar parse_hex_color(const std::string& hex)
{
    std::string h = hex;
    if (!h.empty() && h[0] == '#') {
        h = h.substr(1);
    }
    if (h.size() != 6) {
        LogWarn << "Invalid hex color:" << hex;
        return { 0, 0, 0 };
    }

    try {
        int r = std::stoi(h.substr(0, 2), nullptr, 16);
        int g = std::stoi(h.substr(2, 2), nullptr, 16);
        int b = std::stoi(h.substr(4, 2), nullptr, 16);
        return { static_cast<double>(b), static_cast<double>(g), static_cast<double>(r) };
    }
    catch (const std::exception& e) {
        LogWarn << "Failed to parse hex color:" << hex << e.what();
        return { 0, 0, 0 };
    }
}

inline void mask_colors_as_background(
    cv::Mat& img,
    const std::vector<std::string>& bg_colors,
    int tolerance,
    const cv::Scalar& bg_fill)
{
    for (const auto& hex : bg_colors) {
        cv::Scalar target = parse_hex_color(hex);
        cv::Scalar lower(
            std::max(0.0, target[0] - tolerance),
            std::max(0.0, target[1] - tolerance),
            std::max(0.0, target[2] - tolerance));
        cv::Scalar upper(
            std::min(255.0, target[0] + tolerance),
            std::min(255.0, target[1] + tolerance),
            std::min(255.0, target[2] + tolerance));

        cv::Mat mask;
        cv::inRange(img, lower, upper, mask);
        img.setTo(bg_fill, mask);
    }
}
