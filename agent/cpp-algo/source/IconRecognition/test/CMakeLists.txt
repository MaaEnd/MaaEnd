function(configure_icon_recognition_test target)
    target_include_directories(
        ${target}
        PRIVATE
            "${CMAKE_CURRENT_LIST_DIR}/../.."
            "${CMAKE_CURRENT_LIST_DIR}/.."
    )
    if(MSVC)
        target_compile_options(${target} PRIVATE /utf-8 /W4 /WX /wd4251)
    else()
        target_compile_options(${target} PRIVATE -Wall -Wextra -Wpedantic -Werror)
    endif()
endfunction()

add_executable(icon-recognition-types-tests test_types.cpp)
target_link_libraries(icon-recognition-types-tests PRIVATE ${OpenCV_LIBS})
target_compile_definitions(icon-recognition-types-tests PRIVATE ICON_RECOGNITION_TEST_MAIN)
configure_icon_recognition_test(icon-recognition-types-tests)

add_executable(
    icon-recognition-manual-cli-tests
    test_manual_cli.cpp
    manual_runner_executor.cpp
    manual_runner_config.cpp
)
target_link_libraries(icon-recognition-manual-cli-tests PRIVATE ${OpenCV_LIBS})
target_compile_definitions(
    icon-recognition-manual-cli-tests
    PRIVATE ICON_RECOGNITION_TEST_BUILD_DIR="${CMAKE_CURRENT_BINARY_DIR}"
)
configure_icon_recognition_test(icon-recognition-manual-cli-tests)

add_executable(icon-recognition-debug-tests test_debug_capture.cpp)
target_link_libraries(icon-recognition-debug-tests PRIVATE icon-recognition-core)
configure_icon_recognition_test(icon-recognition-debug-tests)

add_executable(icon-recognition-expected-tests test_expected_results.cpp expected_results.cpp)
target_link_libraries(icon-recognition-expected-tests PRIVATE ${OpenCV_LIBS})
configure_icon_recognition_test(icon-recognition-expected-tests)

add_executable(
    icon-recognition-manual-runner
    test_full_diff.cpp
    expected_results.cpp
    manual_runner_executor.cpp
    manual_runner_config.cpp
)
target_link_libraries(icon-recognition-manual-runner PRIVATE icon-recognition-core)
if(WIN32)
    target_link_libraries(icon-recognition-manual-runner PRIVATE gdi32 user32)
endif()
target_compile_definitions(
    icon-recognition-manual-runner
    PRIVATE
        ICON_RECOGNITION_TEST_INPUT_DIR="${CMAKE_CURRENT_BINARY_DIR}/merged-input"
        ICON_RECOGNITION_TEST_OUTPUT_DIR="${CMAKE_CURRENT_LIST_DIR}/output"
        ICON_RECOGNITION_TEST_WIN32_ROIS_PATH="${CMAKE_CURRENT_LIST_DIR}/../../../../../tests/MaaEndTestset/Win32/Official_CN/IconRecognition/rois.json"
        ICON_RECOGNITION_TEST_ADB_ROIS_PATH="${CMAKE_CURRENT_LIST_DIR}/../../../../../tests/MaaEndTestset/ADB/Official_CN/IconRecognition/rois.json"
        ICON_RECOGNITION_TEST_DATA_ROOT="${CMAKE_CURRENT_LIST_DIR}/../../../../../assets/data/IconRecognition"
)
configure_icon_recognition_test(icon-recognition-manual-runner)

add_custom_target(
    icon-recognition-tests
    DEPENDS
        icon-recognition-types-tests
        icon-recognition-manual-cli-tests
        icon-recognition-debug-tests
        icon-recognition-expected-tests
        icon-recognition-manual-runner
)
