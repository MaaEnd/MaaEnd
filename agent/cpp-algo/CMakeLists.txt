cmake_minimum_required(VERSION 3.28)

project(maaend-cpp-agent)

include(MaaUtils/MaaUtils.cmake)

add_library(HeaderOnlyLibraries INTERFACE)

add_subdirectory(source)
