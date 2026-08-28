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

add_executable(icon-recognition-small-tests test_small_algorithms.cpp)
target_link_libraries(icon-recognition-small-tests PRIVATE icon-recognition-core)
target_compile_definitions(icon-recognition-small-tests PRIVATE ICON_RECOGNITION_TEST_MAIN)
configure_icon_recognition_test(icon-recognition-small-tests)

add_executable(
    icon-recognition-custom-tests
    test_custom_recognition.cpp
    ../IconRecognitionRecognition.cpp
)
target_link_libraries(
    icon-recognition-custom-tests
    PRIVATE
        icon-recognition-core
        MaaFramework::MaaFramework
)
set(ICON_RECOGNITION_TEST_RUNTIME "${CMAKE_CURRENT_BINARY_DIR}/runtime")
set_target_properties(
    icon-recognition-custom-tests
    PROPERTIES
        RUNTIME_OUTPUT_DIRECTORY "${ICON_RECOGNITION_TEST_RUNTIME}/agent"
        RUNTIME_OUTPUT_DIRECTORY_DEBUG "${ICON_RECOGNITION_TEST_RUNTIME}/agent"
        RUNTIME_OUTPUT_DIRECTORY_RELEASE "${ICON_RECOGNITION_TEST_RUNTIME}/agent"
        RUNTIME_OUTPUT_DIRECTORY_RELWITHDEBINFO "${ICON_RECOGNITION_TEST_RUNTIME}/agent"
)
add_custom_command(
    TARGET icon-recognition-custom-tests
    POST_BUILD
    COMMAND
        ${CMAKE_COMMAND} -E make_directory
        "${ICON_RECOGNITION_TEST_RUNTIME}/data/IconRecognition"
    COMMAND
        ${CMAKE_COMMAND} -E copy_if_different
        "${CMAKE_CURRENT_LIST_DIR}/../../../../../assets/data/IconRecognition/recognition_items.json"
        "${ICON_RECOGNITION_TEST_RUNTIME}/data/IconRecognition/recognition_items.json"
    COMMAND
        ${CMAKE_COMMAND} -E copy_directory
        "${CMAKE_CURRENT_LIST_DIR}/../../../../../assets/resource/image/IconRecognition"
        "${ICON_RECOGNITION_TEST_RUNTIME}/resource/image/IconRecognition"
    VERBATIM
)
configure_icon_recognition_test(icon-recognition-custom-tests)

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
        icon-recognition-small-tests
        icon-recognition-custom-tests
        icon-recognition-debug-tests
        icon-recognition-expected-tests
        icon-recognition-manual-runner
)
