#include "DebugUtils.h"
#include <filesystem>
#include <iostream>

namespace mapservice {

static cv::Mat Colorize(const cv::Mat& src) {
    cv::Mat norm, color;
    cv::normalize(src, norm, 0, 255, cv::NORM_MINMAX, CV_8U);
    cv::applyColorMap(norm, color, cv::COLORMAP_JET);
    return color;
}

void DebugUtils::SaveDiagnosticImage(const std::string& name, 
                                   const cv::Mat& templ, 
                                   const cv::Mat& targetROI) {
    if (templ.empty() || targetROI.empty()) return;

    cv::Mat templHSV, targetHSV;
    cv::cvtColor(templ, templHSV, cv::COLOR_BGR2HSV);
    cv::cvtColor(targetROI, targetHSV, cv::COLOR_BGR2HSV);

    std::vector<cv::Mat> tBGR, tHSV;
    std::vector<cv::Mat> rBGR, rHSV;
    cv::split(templ, tBGR);
    cv::split(templHSV, tHSV);
    cv::split(targetROI, rBGR);
    cv::split(targetROI, rHSV);

    cv::Mat absDiff;
    cv::absdiff(templ, targetROI, absDiff);
    cv::Mat diffGray;
    cv::cvtColor(absDiff, diffGray, cv::COLOR_BGR2GRAY);
    cv::Mat diffHeat = Colorize(diffGray);
    
    cv::Mat satT = Colorize(tHSV[1]);
    cv::Mat satR = Colorize(rHSV[1]);
    cv::Mat blueT = Colorize(tBGR[0]);
    cv::Mat blueR = Colorize(rBGR[0]);
    cv::Mat greenT = Colorize(tBGR[1]);
    cv::Mat greenR = Colorize(rBGR[1]);
    cv::Mat redT = Colorize(tBGR[2]);
    cv::Mat redR = Colorize(rBGR[2]);
    cv::Mat hueT = Colorize(tHSV[0]);
    cv::Mat hueR = Colorize(rHSV[0]);

    // 3x4 grid: 每行4张, 3行共12张通道对比图
    int w = templ.cols;
    int h = templ.rows;
    cv::Mat canvas = cv::Mat::zeros(h * 3, w * 4, CV_8UC3);

    auto place = [&](const cv::Mat& img, int r, int c, const std::string& label) {
        cv::Rect roiRect(c * w, r * h, w, h);
        cv::Mat roi = canvas(roiRect);
        
        cv::Mat resizedImg;
        if (img.size() != roi.size())
             cv::resize(img, resizedImg, roi.size());
        else
             resizedImg = img;

        if (resizedImg.channels() == 1)
            cv::cvtColor(resizedImg, roi, cv::COLOR_GRAY2BGR);
        else
            resizedImg.copyTo(roi);
        
        cv::putText(roi, label, cv::Point(5, 15), cv::FONT_HERSHEY_SIMPLEX, 0.4, cv::Scalar(0,255,255), 1);
    };

    place(templ, 0, 0, "Template(BGR)");
    place(targetROI, 0, 1, "Target(BGR)");
    place(diffHeat, 0, 2, "Diff Heatmap");
    place(satT, 0, 3, "Sat(Templ)"); 

    place(blueT, 1, 0, "Blue(Templ)");
    place(blueR, 1, 1, "Blue(Target)");
    place(greenT, 1, 2, "Green(Templ)");
    place(greenR, 1, 3, "Green(Target)");

    place(redT, 2, 0, "Red(Templ)");
    place(redR, 2, 1, "Red(Target)");
    place(hueT, 2, 2, "Hue(Templ)"); 
    place(satR, 2, 3, "Sat(Target)");

    std::string filename = "f:\\MaaEnd\\install\\agent\\debug\\debug_diag_" + name + ".png";
    std::filesystem::create_directories("f:\\MaaEnd\\install\\agent\\debug");
    
    cv::imwrite(filename, canvas);
    std::cout << "[DebugUtils] Saved diagnostic: " << filename << std::endl;
}

}
