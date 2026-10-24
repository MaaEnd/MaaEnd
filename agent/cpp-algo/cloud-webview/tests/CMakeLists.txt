cmake_minimum_required(VERSION 3.28)

# Build portable policy tests without MaaFramework, WebView2 or a desktop host.
if(CMAKE_SOURCE_DIR STREQUAL CMAKE_CURRENT_SOURCE_DIR)
    project(maaend-cloud-policy-tests LANGUAGES CXX)
    enable_testing()
endif()

add_executable(cloud-input-test cloud_input_test.cpp)
target_compile_features(cloud-input-test PRIVATE cxx_std_20)
target_include_directories(cloud-input-test PRIVATE ../transport)
add_test(NAME cloud-input COMMAND cloud-input-test)
set_tests_properties(cloud-input PROPERTIES LABELS "cloud-policy")

add_executable(cloud-startup-notice-test startup_notice_policy_test.cpp)
target_compile_features(cloud-startup-notice-test PRIVATE cxx_std_20)
target_include_directories(cloud-startup-notice-test PRIVATE ..)
add_test(NAME cloud-startup-notice COMMAND cloud-startup-notice-test)
set_tests_properties(cloud-startup-notice PROPERTIES LABELS "cloud-policy")
