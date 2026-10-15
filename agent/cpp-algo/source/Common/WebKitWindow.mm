#include "WebKitWindow.h"

#include <atomic>
#include <utility>

#include <MaaUtils/Logger.h>

#include "MacAppHost.h"

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

namespace
{

NSString* const kMessageHandlerName = @"maaendCapture";

NSString* const kFragmentPlaceholder = @"__MAAEND_FRAGMENT__";

NSString* const kSnifferScript = @R"JS((() => {
  const FRAGMENT = __MAAEND_FRAGMENT__[0];
  const handler = window.webkit && window.webkit.messageHandlers && window.webkit.messageHandlers.maaendCapture;
  if (!handler) return;
  const tag = Math.random().toString(36).slice(2);
  let seq = 0;
  const absolute = (u) => { try { return new URL(String(u), location.href).href; } catch (e) { return String(u); } };
  const post = (message) => { try { handler.postMessage(message); } catch (e) {} };
  const nativeFetch = window.fetch;
  if (typeof nativeFetch === "function") {
    window.fetch = function (input) {
      const promise = nativeFetch.apply(this, arguments);
      const url = absolute(input instanceof Request ? input.url : input);
      if (url.indexOf(FRAGMENT) === -1) return promise;
      const id = tag + ":" + (++seq);
      post({ kind: "begin", id: id, url: url });
      promise.then(
        (response) => response.clone().text().then(
          (body) => post({ kind: "body", id: id, url: url, body: body }),
          () => post({ kind: "fail", id: id, url: url })),
        () => post({ kind: "fail", id: id, url: url }));
      return promise;
    };
  }
  const proto = XMLHttpRequest.prototype;
  const nativeOpen = proto.open;
  const nativeSend = proto.send;
  proto.open = function (method, url) {
    this.__maaendUrl = absolute(url);
    return nativeOpen.apply(this, arguments);
  };
  proto.send = function () {
    const url = this.__maaendUrl || "";
    if (url.indexOf(FRAGMENT) !== -1) {
      const id = tag + ":" + (++seq);
      post({ kind: "begin", id: id, url: url });
      this.addEventListener("loadend", () => {
        let body = "";
        try {
          if (this.responseType === "" || this.responseType === "text") body = this.responseText;
          else if (this.responseType === "json" && this.response != null) body = JSON.stringify(this.response);
        } catch (e) {}
        post(body ? { kind: "body", id: id, url: url, body: body } : { kind: "fail", id: id, url: url });
      });
    }
    return nativeSend.apply(this, arguments);
  };
})();)JS";

std::string ToStdString(id value)
{
    if (![value isKindOfClass:[NSString class]]) {
        return {};
    }
    const char* utf8 = [(NSString*)value UTF8String];
    return utf8 ? std::string(utf8) : std::string {};
}

NSString* ToNSString(const std::string& value)
{
    return [[NSString alloc] initWithBytes:value.data() length:value.size() encoding:NSUTF8StringEncoding];
}

NSString* BuildSnifferScript(const std::string& path_fragment)
{
    NSData* json = [NSJSONSerialization dataWithJSONObject:@[ ToNSString(path_fragment) ] options:0 error:nil];
    NSString* fragment_array = [[NSString alloc] initWithData:json encoding:NSUTF8StringEncoding];
    return [kSnifferScript stringByReplacingOccurrencesOfString:kFragmentPlaceholder withString:fragment_array];
}

void RunOnMainSync(const std::function<void()>& fn)
{
    if ([NSThread isMainThread]) {
        fn();
        return;
    }
    const std::function<void()>* fn_ptr = &fn;
    dispatch_sync(dispatch_get_main_queue(), ^{
        (*fn_ptr)();
    });
}

} // namespace

@interface MaaEndWebKitBridge : NSObject <WKScriptMessageHandler, NSWindowDelegate>
@end

@implementation MaaEndWebKitBridge {
@public
    WebKitWindow::RequestCallback on_request;
    WebKitWindow::ResponseCallback on_response;
    std::shared_ptr<std::atomic_bool> opened;
}

- (void)userContentController:(WKUserContentController*)controller didReceiveScriptMessage:(WKScriptMessage*)message
{
    if (!message.frameInfo.isMainFrame || ![message.body isKindOfClass:[NSDictionary class]]) {
        return;
    }
    NSDictionary* body = (NSDictionary*)message.body;
    const std::string kind = ToStdString(body[@"kind"]);
    std::string request_id = ToStdString(body[@"id"]);
    std::string url = ToStdString(body[@"url"]);
    if (request_id.empty()) {
        return;
    }

    if (kind == "begin") {
        if (on_request) {
            on_request(std::move(request_id), std::move(url));
        }
    }
    else if (kind == "body" || kind == "fail") {
        if (on_response) {
            on_response(std::move(request_id), std::move(url), kind == "body" ? ToStdString(body[@"body"]) : std::string {});
        }
    }
}

- (void)windowWillClose:(NSNotification*)notification
{
    if (opened) {
        opened->store(false);
    }
}

@end

struct WebKitWindow::Impl
{
    std::string url;
    int width = 1280;
    int height = 720;
    bool clear_web_data = false;
    std::string path_fragment;
    RequestCallback on_request;
    ResponseCallback on_response;
    std::shared_ptr<std::atomic_bool> opened = std::make_shared<std::atomic_bool>(false);

    NSWindow* window = nil;
    WKWebView* webview = nil;
    MaaEndWebKitBridge* bridge = nil;
};

WebKitWindow::WebKitWindow()
    : impl_(std::make_unique<Impl>())
{
}

WebKitWindow::~WebKitWindow()
{
    Close();
}

void WebKitWindow::SetURL(std::string url)
{
    impl_->url = std::move(url);
}

void WebKitWindow::SetSize(int width, int height)
{
    impl_->width = width;
    impl_->height = height;
}

void WebKitWindow::SetClearWebData(bool enabled)
{
    impl_->clear_web_data = enabled;
}

void WebKitWindow::SubscribeResponses(std::string path_fragment, RequestCallback on_request, ResponseCallback on_response)
{
    impl_->path_fragment = std::move(path_fragment);
    impl_->on_request = std::move(on_request);
    impl_->on_response = std::move(on_response);
}

bool WebKitWindow::Open()
{
    if (impl_->opened->load()) {
        return true;
    }
    if (!common::macapp::IsMainLoopRunning()) {
        LogError << "WebKitWindow: AppKit main loop is not running";
        return false;
    }

    Impl& impl = *impl_;
    NSURL* ns_url = [NSURL URLWithString:ToNSString(impl.url)];
    if (ns_url == nil) {
        LogError << "WebKitWindow: invalid url" << VAR(impl.url);
        return false;
    }

    RunOnMainSync([&impl, ns_url] {
        MaaEndWebKitBridge* bridge = [[MaaEndWebKitBridge alloc] init];
        bridge->on_request = impl.on_request;
        bridge->on_response = impl.on_response;
        bridge->opened = impl.opened;

        WKWebViewConfiguration* config = [[WKWebViewConfiguration alloc] init];
        config.websiteDataStore = [WKWebsiteDataStore defaultDataStore];
        if (!impl.path_fragment.empty()) {
            WKUserScript* sniffer = [[WKUserScript alloc] initWithSource:BuildSnifferScript(impl.path_fragment)
                                                           injectionTime:WKUserScriptInjectionTimeAtDocumentStart
                                                        forMainFrameOnly:YES];
            [config.userContentController addUserScript:sniffer];
            [config.userContentController addScriptMessageHandler:bridge name:kMessageHandlerName];
        }

        const NSRect frame = NSMakeRect(0, 0, impl.width, impl.height);
        NSWindow* window = [[NSWindow alloc]
            initWithContentRect:frame
                      styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable | NSWindowStyleMaskMiniaturizable
                                | NSWindowStyleMaskResizable
                        backing:NSBackingStoreBuffered
                          defer:NO];
        window.releasedWhenClosed = NO;
        window.title = @"MaaEnd";
        window.delegate = bridge;

        WKWebView* webview = [[WKWebView alloc] initWithFrame:frame configuration:config];
        webview.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
        window.contentView = webview;
        [window center];

        impl.window = window;
        impl.webview = webview;
        impl.bridge = bridge;
        impl.opened->store(true);

        NSSet<NSString*>* types = impl.clear_web_data
                                      ? [WKWebsiteDataStore allWebsiteDataTypes]
                                      : [NSSet setWithObjects:WKWebsiteDataTypeDiskCache, WKWebsiteDataTypeMemoryCache, nil];
        NSURLRequest* request = [NSURLRequest requestWithURL:ns_url];
        __weak WKWebView* weak_webview = webview;
        [config.websiteDataStore removeDataOfTypes:types
                                     modifiedSince:[NSDate distantPast]
                                 completionHandler:^{
                                     [weak_webview loadRequest:request];
                                 }];

        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
        [window makeKeyAndOrderFront:nil];
        [window orderFrontRegardless];
        if (@available(macOS 14.0, *)) {
            [NSApp activate];
        }
        else {
            [NSApp activateIgnoringOtherApps:YES];
        }
    });
    return true;
}

void WebKitWindow::Close()
{
    Impl& impl = *impl_;
    impl.opened->store(false);
    if (![NSThread isMainThread] && !common::macapp::IsMainLoopRunning()) {
        return;
    }
    RunOnMainSync([&impl] {
        if (impl.window == nil) {
            return;
        }
        [impl.webview.configuration.userContentController removeScriptMessageHandlerForName:kMessageHandlerName];
        [impl.webview stopLoading];
        impl.window.delegate = nil;
        [impl.window close];
        impl.window = nil;
        impl.webview = nil;
        impl.bridge = nil;
        [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
    });
}

bool WebKitWindow::IsOpened() const
{
    return impl_->opened->load();
}
