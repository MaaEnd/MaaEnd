cmake_minimum_required(VERSION 3.24)

project(maaend-cloud-shell LANGUAGES CXX RC)

if(NOT WIN32)
    message(FATAL_ERROR "CloudShell only supports Windows")
endif()

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/bin")
# 静态 CRT：不依赖目标机器上的 VC++ 运行库，产物是单个 exe
set(CMAKE_MSVC_RUNTIME_LIBRARY "MultiThreaded$<$<CONFIG:Debug>:Debug>")

include(cmake/WebView2Sdk.cmake)

# 资源脚本里用绝对路径引用页面脚本
set(CLOUD_SHELL_SCRIPTS_DIR "${CMAKE_CURRENT_SOURCE_DIR}/scripts")
configure_file(src/resources.rc.in "${CMAKE_CURRENT_BINARY_DIR}/resources.rc" @ONLY)

add_executable(cloud-shell WIN32
    src/main.cpp
    src/ShellWindow.cpp
    src/InputBridge.cpp
    src/KeyMap.cpp
    src/Log.cpp
    src/Resources.cpp
    "${CMAKE_CURRENT_BINARY_DIR}/resources.rc"
)

target_include_directories(cloud-shell PRIVATE src)
target_link_libraries(cloud-shell PRIVATE WebView2::LoaderStatic)
target_compile_definitions(cloud-shell PRIVATE UNICODE _UNICODE WIN32_LEAN_AND_MEAN NOMINMAX)
target_compile_options(cloud-shell PRIVATE /utf-8 /W4 /WX /permissive-)

# RCDATA 引用的文件不在资源编译的依赖里
set_property(SOURCE "${CMAKE_CURRENT_BINARY_DIR}/resources.rc" APPEND PROPERTY OBJECT_DEPENDS
    "${CLOUD_SHELL_SCRIPTS_DIR}/cloud_policy.js")

install(TARGETS cloud-shell RUNTIME DESTINATION .)
