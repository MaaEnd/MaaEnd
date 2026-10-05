#pragma once

#ifdef __APPLE__

#include <functional>
#include <memory>
#include <string>

class WebKitWindow
{
public:
    using RequestCallback = std::function<void(std::string request_id, std::string url)>;
    using ResponseCallback = std::function<void(std::string request_id, std::string url, std::string body)>;

    WebKitWindow();
    ~WebKitWindow();

    WebKitWindow(const WebKitWindow&) = delete;
    WebKitWindow& operator=(const WebKitWindow&) = delete;

    void SetURL(std::string url);
    void SetSize(int width, int height);
    void SetClearWebData(bool enabled);

    void SubscribeResponses(std::string path_fragment, RequestCallback on_request, ResponseCallback on_response);

    bool Open();
    void Close();
    bool IsOpened() const;

private:
    struct Impl;
    std::unique_ptr<Impl> impl_;
};

#endif // __APPLE__
