#pragma once

#include <optional>

#include "GridTypes.h"

namespace iconrecognition::detail
{

std::optional<int> FitDirectedCellPhase(const std::vector<float>& signed_boundary, int cell_size, int pitch);
int CellPhaseDistance(int left, int right, int pitch);
bool ShouldUseDirectedCellPhase(bool transfer, int column_count, int coarse_phase, int directed_phase, int pitch);
GridDetection DetectGrid(const cv::Mat& image, GridType type, const cv::Rect& roi);

} // namespace iconrecognition::detail
