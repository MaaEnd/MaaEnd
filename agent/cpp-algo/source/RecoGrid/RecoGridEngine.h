#pragma once

#include "RecoGridSession.h"

#include <filesystem>
#include <string>
#include <unordered_map>
#include <vector>

namespace recogrid
{

class RecoGridEngine
{
public:
    void LoadTemplatesFromDirectory(
        const std::filesystem::path& directory,
        const TemplateLoadOptions& options = {});
    void SetTemplates(std::vector<GridClassifyTemplate> templates);
    void ResetSession(const std::string& sessionId);
    void ClearSessions();

    [[nodiscard]] const std::vector<GridClassifyTemplate>& Templates() const noexcept;
    [[nodiscard]] GridScanResult Scan(
        const std::string& sessionId,
        const cv::Mat& image,
        const GridScanOptions& options = {});

private:
    std::vector<GridClassifyTemplate> templates_;
    std::unordered_map<std::string, SessionState> sessions_;
};

} // namespace recogrid
