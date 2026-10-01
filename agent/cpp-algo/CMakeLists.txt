cmake_minimum_required(VERSION 3.28)

project(maaend-cpp-agent)

if(APPLE)
    enable_language(OBJCXX)
endif()

option(MAAEND_BUILD_ICON_RECOGNITION_TESTS "Build IconRecognition tests" OFF)

include(MaaUtils/MaaUtils.cmake)

# NDK 的 libc++ 仍把 std::jthread / std::stop_token 归为实验特性（Navmesh 在用）
if(ANDROID)
    add_compile_options(-fexperimental-library)
    add_link_options(-fexperimental-library)
endif()

add_library(HeaderOnlyLibraries INTERFACE)

set(DEPS_DIR "${CMAKE_CURRENT_SOURCE_DIR}/../../deps" CACHE PATH
    "MaaFramework dependency directory")

if(NOT EXISTS "${DEPS_DIR}")
    message(FATAL_ERROR "Dependencies directory not found: ${DEPS_DIR}")
endif()

# MaaFramework only ships RelWithDebInfo config, map all other configs to it
set(CMAKE_MAP_IMPORTED_CONFIG_RELWITHDEBINFO RelWithDebInfo)
set(CMAKE_MAP_IMPORTED_CONFIG_MINSIZEREL RelWithDebInfo)
set(CMAKE_MAP_IMPORTED_CONFIG_RELEASE RelWithDebInfo)
set(CMAKE_MAP_IMPORTED_CONFIG_DEBUG RelWithDebInfo)

# MaaAgentServer 通过 find_dependency 引入 MaaFramework；Android 工具链将包搜索限定在 sysroot，
# 其 HINTS 会失效，这里显式给出位置（仅定位，不直接链接 MaaFramework/MaaToolkit）
set(MaaFramework_DIR "${DEPS_DIR}/share/cmake/MaaFramework")
set(MaaAgentServer_DIR "${DEPS_DIR}/share/cmake/MaaAgentServer")
find_package(MaaAgentServer REQUIRED)

# WebView2 SDK configuration (Windows-only; on Windows this include may fail
# configuration if the SDK is unavailable or WEBVIEW2_SDK_DIR is not set correctly)
include(${CMAKE_CURRENT_SOURCE_DIR}/cmake/WebView2.cmake)

add_subdirectory(source)
