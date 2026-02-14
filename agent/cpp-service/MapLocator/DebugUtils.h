#pragma once
#include <opencv2/opencv.hpp>
#include <string>
#include <vector>

namespace mapservice {

class DebugUtils {
public:
    // name: "Rank1_Wuling" / "Rank2_Valley"
    static void SaveDiagnosticImage(const std::string& name, 
                                  const cv::Mat& templ, 
                                  const cv::Mat& targetROI);
};

}
