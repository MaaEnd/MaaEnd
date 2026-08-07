#pragma once

#include "GridTypes.h"

namespace iconrecognition::detail
{

struct StructureMaps
{
    cv::Mat vertical;
    cv::Mat horizontal;
    cv::Mat signed_x;
    cv::Mat signed_y;
    cv::Mat diagonal_penalty;
};

StructureMaps BuildStructureMaps(const cv::Mat& image, int cell_size);
double Percentile(std::vector<float> values, double percentile);
std::vector<float> RobustProjection(const cv::Mat& values, bool x_axis);
std::vector<float> AggregateSigned(const cv::Mat& values, bool x_axis);
std::vector<float> MedianProjection(const cv::Mat& values, bool x_axis);
double GridOccupancyScore(const cv::Mat& image, const GridLayout& layout);

} // namespace iconrecognition::detail
