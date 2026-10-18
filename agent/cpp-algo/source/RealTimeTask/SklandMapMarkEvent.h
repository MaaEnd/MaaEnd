#pragma once

#include <memory>

#include <MaaFramework/MaaAPI.h>

#include "Common/WebView2.h"

namespace realtimetask
{

// SetSklandMapWebView 注册当前活动的森空岛地图悬浮窗，供 Pipeline 的事件节点注入 JS。
// 传入空的 shared_ptr 表示注销。注册只在 RealTimeTaskAction 的循环期间有效：
// 循环退出后由 RealTimeTaskAction 负责清空，避免留下悬空引用。
void SetSklandMapWebView(std::shared_ptr<WebView2> webview);

// GetSklandMapWebView 取当前注册的悬浮窗；没有注册或已销毁时返回 nullptr。
std::shared_ptr<WebView2> GetSklandMapWebView();

// SklandMapMarkEventRun 把 Pipeline 识别到的收集事件推给地图页面里的 auto-mark.js
// （调用 window.__maaMapMark.onEvent）。custom_action_param 形如 {"type":"chest"}。
// type 的取值与 auto-mark.js 的 TYPE_RULES 一致：chest / essence / crystal / archive，
// 另外 "open" 和 "gain" 用于表达宝箱的「打开提示」与「获得提示」两个时刻。
MaaBool MAA_CALL SklandMapMarkEventRun(
    MaaContext* context,
    MaaTaskId task_id,
    const char* node_name,
    const char* custom_action_name,
    const char* custom_action_param,
    MaaRecoId reco_id,
    const MaaRect* box,
    void* trans_arg);

} // namespace realtimetask
