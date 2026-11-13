# 获取 Microsoft.Web.WebView2 NuGet 包，导出 WebView2::LoaderStatic

set(WEBVIEW2_SDK_VERSION "1.0.2210.55")
set(WEBVIEW2_SDK_SHA256 "c0945ddfe38272f10f24673a1cb07b54b018f614eec85d3e830628369039ad3d")
set(WEBVIEW2_SDK_DIR "" CACHE PATH "Extracted Microsoft.Web.WebView2 NuGet package root; leave empty to download")

string(TOLOWER "${CMAKE_CXX_COMPILER_ARCHITECTURE_ID}" _webview2_arch)
if(_webview2_arch STREQUAL "amd64" OR _webview2_arch STREQUAL "x86_64")
    set(_webview2_arch "x64")
endif()

set(_webview2_root "${WEBVIEW2_SDK_DIR}")
if(NOT _webview2_root)
    set(_webview2_root "${CMAKE_BINARY_DIR}/_deps/webview2-${WEBVIEW2_SDK_VERSION}")
endif()
set(_webview2_include "${_webview2_root}/build/native/include")
set(_webview2_lib "${_webview2_root}/build/native/${_webview2_arch}/WebView2LoaderStatic.lib")

# 头文件和库任一缺失都重新解压，解压中途被打断的目录也能自动恢复
if(NOT WEBVIEW2_SDK_DIR AND NOT (EXISTS "${_webview2_include}/WebView2.h" AND EXISTS "${_webview2_lib}"))
    # .nupkg 就是 zip，换成 .zip 后缀交给 file(ARCHIVE_EXTRACT)
    set(_webview2_archive "${CMAKE_BINARY_DIR}/_deps/webview2-${WEBVIEW2_SDK_VERSION}.zip")
    message(STATUS "Downloading WebView2 SDK ${WEBVIEW2_SDK_VERSION}")
    file(DOWNLOAD
        "https://www.nuget.org/api/v2/package/Microsoft.Web.WebView2/${WEBVIEW2_SDK_VERSION}"
        "${_webview2_archive}"
        EXPECTED_HASH SHA256=${WEBVIEW2_SDK_SHA256}
        TLS_VERIFY ON
        STATUS _webview2_status)
    list(GET _webview2_status 0 _webview2_code)
    if(NOT _webview2_code EQUAL 0)
        list(GET _webview2_status 1 _webview2_message)
        message(FATAL_ERROR
            "Failed to download WebView2 SDK: ${_webview2_message}\n"
            "Set HTTPS_PROXY, or pass -DWEBVIEW2_SDK_DIR=<extracted NuGet root>.")
    endif()
    file(REMOVE_RECURSE "${_webview2_root}")
    file(ARCHIVE_EXTRACT INPUT "${_webview2_archive}" DESTINATION "${_webview2_root}")
endif()

foreach(_webview2_file "${_webview2_include}/WebView2.h" "${_webview2_lib}")
    if(NOT EXISTS "${_webview2_file}")
        message(FATAL_ERROR "WebView2 SDK file not found: ${_webview2_file}")
    endif()
endforeach()

add_library(WebView2::LoaderStatic STATIC IMPORTED GLOBAL)
set_target_properties(WebView2::LoaderStatic PROPERTIES
    IMPORTED_LOCATION "${_webview2_lib}"
    INTERFACE_INCLUDE_DIRECTORIES "${_webview2_include}")

message(STATUS "WebView2 SDK (arch=${_webview2_arch}): ${_webview2_root}")
