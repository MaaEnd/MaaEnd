cmake_minimum_required(VERSION 3.28)

project(maaend-cpp-agent)

include(MaaUtils/MaaUtils.cmake)
add_library(HeaderOnlyLibraries INTERFACE)

set(DEPS_DIR ${CMAKE_CURRENT_SOURCE_DIR}/../../deps)

if(NOT EXISTS ${DEPS_DIR})
    message(FATAL_ERROR "Dependencies directory not found: ${DEPS_DIR}")
endif()

# MaaFramework only ships RelWithDebInfo config, map all other configs to it
set(CMAKE_MAP_IMPORTED_CONFIG_RELWITHDEBINFO RelWithDebInfo)
set(CMAKE_MAP_IMPORTED_CONFIG_MINSIZEREL RelWithDebInfo)
set(CMAKE_MAP_IMPORTED_CONFIG_RELEASE RelWithDebInfo)
set(CMAKE_MAP_IMPORTED_CONFIG_DEBUG RelWithDebInfo)

set(MaaFramework_DIR ${DEPS_DIR}/share/cmake/MaaFramework)
find_package(MaaFramework REQUIRED)

set(MaaAgentServer_DIR ${DEPS_DIR}/share/cmake/MaaAgentServer)
find_package(MaaAgentServer REQUIRED)

add_subdirectory(source)
