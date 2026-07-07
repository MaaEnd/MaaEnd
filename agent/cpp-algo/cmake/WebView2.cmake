# WebView2 SDK integration (Windows only).
#
# 该模块在 Windows 平台查找 Microsoft.Web.WebView2 NuGet 包的解压目录，
# 并导出 SHARED IMPORTED 目标 `WebView2::WebView2`，链接动态 Loader：
#   - import lib: WebView2Loader.dll.lib
#   - runtime  : WebView2Loader.dll （需要随 cpp-algo.exe 一同发布到 install/agent/）
#
# SDK 路径按以下顺序解析：
#   1. CMake cache 变量 WEBVIEW2_SDK_DIR
#   2. 同名环境变量 WEBVIEW2_SDK_DIR
#   3. 默认位置 <repo>/agent/cpp-algo/3rdparty/webview2（由 tools/setup_workspace.py 准备）
#
# 期望的目录结构（NuGet 包解压后的根目录）：
#   <root>/build/native/include/WebView2.h
#   <root>/build/native/<arch>/WebView2Loader.dll
#   <root>/build/native/<arch>/WebView2Loader.dll.lib
# 其中 <arch> 取自 VS 生成器平台名：x64 / arm64 / x86
#
# 使用方式：
#   if(TARGET WebView2::WebView2)
#       target_link_libraries(<your-target> PRIVATE WebView2::WebView2)
#       # 运行时还需要把 WebView2Loader.dll 拷贝到可执行文件旁边，
#       # 推荐使用 install(IMPORTED_RUNTIME_ARTIFACTS WebView2::WebView2 ...)
#   endif()
#
# 平台行为：
#   - 非 Windows：no-op，不创建任何目标
#   - Windows + 能定位到 SDK：创建 WebView2::WebView2 IMPORTED 目标
#   - Windows + 三种来源全部失败：FATAL_ERROR 并提示运行 setup_workspace.py

if(NOT WIN32)
    return()
endif()

if(TARGET WebView2::WebView2)
    return()
endif()

if(NOT WEBVIEW2_SDK_DIR)
    set(WEBVIEW2_SDK_DIR "$ENV{WEBVIEW2_SDK_DIR}")
endif()

if(NOT WEBVIEW2_SDK_DIR)
    # 默认位置：cpp-algo 自己的 3rdparty/webview2 子目录，由 tools/setup_workspace.py
    # 在工作区初始化阶段一次性下载并解压。
    # 当前文件位于 <repo>/agent/cpp-algo/cmake/，所以 ../3rdparty 指向 <repo>/agent/cpp-algo/3rdparty。
    set(_webview2_default_dir "${CMAKE_CURRENT_LIST_DIR}/../3rdparty/webview2")
    if(EXISTS "${_webview2_default_dir}/build/native/include/WebView2.h")
        get_filename_component(WEBVIEW2_SDK_DIR "${_webview2_default_dir}" ABSOLUTE)
    endif()
endif()

if(NOT WEBVIEW2_SDK_DIR)
    # cpp-algo 在 Windows 平台把 WebView2 视为强依赖；走到这里说明工作区没初始化好，
    # 必须立刻 FATAL_ERROR，避免静默缺特性导致后续运行期才暴露问题。
    message(FATAL_ERROR
        "WebView2 SDK is required for the Windows build but cannot be located.\n"
        "Choose one of the following:\n"
        "  - Run `python tools/setup_workspace.py` (downloads SDK to agent/cpp-algo/3rdparty/webview2/)\n"
        "  - Run `python tools/dep_3rdparty.py --webview2` to only fetch the SDK\n"
        "  - Pass -DWEBVIEW2_SDK_DIR=<extracted NuGet root> to cmake configure\n"
        "  - Set WEBVIEW2_SDK_DIR environment variable to point at an extracted SDK")
endif()

# Visual Studio 生成器优先使用 CMAKE_VS_PLATFORM_NAME / CMAKE_GENERATOR_PLATFORM；
# 其它生成器（理论上不会进到这里，因为 WIN32 + 其它生成器较少见）回退到指针位宽。
if(CMAKE_VS_PLATFORM_NAME)
    set(_webview2_vs_platform "${CMAKE_VS_PLATFORM_NAME}")
elseif(CMAKE_GENERATOR_PLATFORM)
    set(_webview2_vs_platform "${CMAKE_GENERATOR_PLATFORM}")
else()
    set(_webview2_vs_platform "")
endif()

string(TOLOWER "${_webview2_vs_platform}" _webview2_vs_platform_lower)

if(_webview2_vs_platform_lower STREQUAL "arm64")
    set(_webview2_arch "arm64")
elseif(_webview2_vs_platform_lower STREQUAL "x64")
    set(_webview2_arch "x64")
elseif(_webview2_vs_platform_lower STREQUAL "win32")
    set(_webview2_arch "x86")
elseif(CMAKE_SIZEOF_VOID_P EQUAL 8)
    set(_webview2_arch "x64")
else()
    set(_webview2_arch "x86")
endif()

set(_webview2_native_dir "${WEBVIEW2_SDK_DIR}/build/native")
set(_webview2_include_dir "${_webview2_native_dir}/include")
set(_webview2_loader_dll "${_webview2_native_dir}/${_webview2_arch}/WebView2Loader.dll")
set(_webview2_loader_implib "${_webview2_native_dir}/${_webview2_arch}/WebView2Loader.dll.lib")

if(NOT EXISTS "${_webview2_include_dir}/WebView2.h")
    message(FATAL_ERROR
        "WebView2 SDK header not found.\n"
        "  Expected: ${_webview2_include_dir}/WebView2.h\n"
        "  WEBVIEW2_SDK_DIR=${WEBVIEW2_SDK_DIR}\n"
        "Make sure WEBVIEW2_SDK_DIR points to the extracted Microsoft.Web.WebView2 NuGet package root.")
endif()

if(NOT EXISTS "${_webview2_loader_dll}")
    message(FATAL_ERROR
        "WebView2 dynamic loader DLL not found.\n"
        "  Expected: ${_webview2_loader_dll}\n"
        "  Detected arch: ${_webview2_arch} (from CMAKE_VS_PLATFORM_NAME='${CMAKE_VS_PLATFORM_NAME}')\n"
        "Make sure the SDK contains WebView2Loader.dll for this architecture.")
endif()

if(NOT EXISTS "${_webview2_loader_implib}")
    message(FATAL_ERROR
        "WebView2 import library not found.\n"
        "  Expected: ${_webview2_loader_implib}\n"
        "  Detected arch: ${_webview2_arch}\n"
        "Make sure the SDK contains WebView2Loader.dll.lib for this architecture.")
endif()

add_library(WebView2::WebView2 SHARED IMPORTED GLOBAL)
# 顶层 CMakeLists 通过 CMAKE_MAP_IMPORTED_CONFIG_* 把所有 config 都映射到 RELWITHDEBINFO，
# 因此必须为 RELWITHDEBINFO 后缀显式设置 IMPORTED_LOCATION/IMPLIB；
# 同时保留无后缀属性作为兜底，覆盖未启用 config-mapping 的调用方（例如直接 ninja 单 config）。
set_target_properties(WebView2::WebView2 PROPERTIES
    IMPORTED_CONFIGURATIONS "RELWITHDEBINFO"
    IMPORTED_LOCATION "${_webview2_loader_dll}"
    IMPORTED_IMPLIB "${_webview2_loader_implib}"
    IMPORTED_LOCATION_RELWITHDEBINFO "${_webview2_loader_dll}"
    IMPORTED_IMPLIB_RELWITHDEBINFO "${_webview2_loader_implib}"
    INTERFACE_INCLUDE_DIRECTORIES "${_webview2_include_dir}")

message(STATUS "WebView2 SDK enabled (arch=${_webview2_arch}, dynamic): ${WEBVIEW2_SDK_DIR}")
