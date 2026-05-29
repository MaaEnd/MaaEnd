#pragma once

#include <opencv2/core.hpp>

#include <cstddef>
#include <cstdint>
#include <vector>

namespace recogrid
{

using Hash = std::uint64_t;

struct Candidate
{
    std::size_t cellIndex = 0;
    cv::Rect cell;
    Hash hash = 0;
    int distance = 0;
};

Hash ComputeHash(const cv::Mat& image);
int HammingDistance(Hash lhs, Hash rhs);
std::vector<Hash> ComputeCellHashes(const cv::Mat& roi, const std::vector<cv::Rect>& cells);
Hash ComputeHashResizedTo(const cv::Mat& image, cv::Size size);
std::vector<Candidate> FilterCandidates(
    const cv::Mat& roi,
    const std::vector<cv::Rect>& cells,
    Hash targetHash,
    int maxDistance);
std::vector<Candidate> FilterCandidates(
    const cv::Mat& roi,
    const std::vector<cv::Rect>& cells,
    const cv::Mat& target,
    int maxDistance);

} // namespace recogrid
