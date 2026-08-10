cmake_minimum_required(VERSION 3.28)

project(icon-recognition-tests LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

function(configure_icon_recognition_test target)
    if(MSVC)
        target_compile_options(${target} PRIVATE /utf-8 /W4 /WX /wd4251)
    else()
        target_compile_options(${target} PRIVATE -Wall -Wextra -Wpedantic -Werror)
    endif()
endfunction()

set(VCPKG_ROOT "${CMAKE_CURRENT_LIST_DIR}/../../../MaaUtils/MaaDeps/vcpkg/installed/maa-x64-windows")
list(PREPEND CMAKE_PREFIX_PATH "${VCPKG_ROOT}")
set(OpenCV_DIR "${VCPKG_ROOT}/share/opencv4")
find_package(OpenCV REQUIRED COMPONENTS core imgproc imgcodecs)

add_executable(icon-recognition-types-tests test_types.cpp)
target_include_directories(
    icon-recognition-types-tests
    PRIVATE
        "${CMAKE_CURRENT_LIST_DIR}/../../../MaaUtils/include"
        "${VCPKG_ROOT}/include"
        "${CMAKE_CURRENT_LIST_DIR}/.."
)
target_link_libraries(icon-recognition-types-tests PRIVATE ${OpenCV_LIBS})
target_compile_definitions(icon-recognition-types-tests PRIVATE ICON_RECOGNITION_TEST_MAIN)
configure_icon_recognition_test(icon-recognition-types-tests)

add_executable(
    icon-recognition-manual-cli-tests
    test_manual_cli.cpp
    manual_runner_executor.cpp
    manual_runner_config.cpp
)
target_include_directories(
    icon-recognition-manual-cli-tests
    PRIVATE
        "${CMAKE_CURRENT_LIST_DIR}/../../../MaaUtils/include"
        "${VCPKG_ROOT}/include"
        "${CMAKE_CURRENT_LIST_DIR}/.."
)
target_link_libraries(icon-recognition-manual-cli-tests PRIVATE ${OpenCV_LIBS})
target_compile_definitions(
    icon-recognition-manual-cli-tests
    PRIVATE ICON_RECOGNITION_TEST_BUILD_DIR="${CMAKE_CURRENT_BINARY_DIR}"
)
configure_icon_recognition_test(icon-recognition-manual-cli-tests)

add_executable(
    icon-recognition-small-tests
    test_small_algorithms.cpp
    ../IconRecognizer.cpp
    ../detail/CompositeIcon.cpp
    ../detail/ForegroundTexture.cpp
    ../detail/GridDetector.cpp
    ../detail/GridFeatures.cpp
    ../detail/GridGeometry.cpp
    ../detail/RegularLattice.cpp
    ../detail/GridAnchors.cpp
    ../detail/GridProfiles.cpp
    ../detail/IconMatcher.cpp
    ../detail/MaskPolicy.cpp
    ../detail/RarityCandidates.cpp
    ../detail/RarityClassifier.cpp
    ../detail/TrustedRarity.cpp
    ../detail/RecognitionDiagnostics.cpp
    ../detail/SubpixelMatcher.cpp
    ../detail/TemplateCatalog.cpp
    ../detail/TemplateTypes.cpp
)
target_include_directories(
    icon-recognition-small-tests
    PRIVATE
        "${CMAKE_CURRENT_LIST_DIR}/../../../MaaUtils/include"
        "${VCPKG_ROOT}/include"
        "${CMAKE_CURRENT_LIST_DIR}/.."
)
target_link_libraries(
    icon-recognition-small-tests
    PRIVATE
        ${OpenCV_LIBS}
        "${CMAKE_CURRENT_LIST_DIR}/../../../build/lib/RelWithDebInfo/MaaUtils.lib"
)
target_compile_definitions(icon-recognition-small-tests PRIVATE ICON_RECOGNITION_TEST_MAIN)
configure_icon_recognition_test(icon-recognition-small-tests)

add_executable(
    icon-recognition-custom-tests
    test_custom_recognition.cpp
    ../IconRecognitionRecognition.cpp
    ../IconRecognizer.cpp
    ../detail/CompositeIcon.cpp
    ../detail/DebugCapture.cpp
    ../detail/ForegroundTexture.cpp
    ../detail/GridDetector.cpp
    ../detail/GridFeatures.cpp
    ../detail/GridGeometry.cpp
    ../detail/RegularLattice.cpp
    ../detail/GridAnchors.cpp
    ../detail/GridProfiles.cpp
    ../detail/IconMatcher.cpp
    ../detail/MaskPolicy.cpp
    ../detail/RarityCandidates.cpp
    ../detail/RarityClassifier.cpp
    ../detail/TrustedRarity.cpp
    ../detail/RecognitionDiagnostics.cpp
    ../detail/SubpixelMatcher.cpp
    ../detail/TemplateCatalog.cpp
    ../detail/TemplateTypes.cpp
)
target_include_directories(
    icon-recognition-custom-tests
    PRIVATE
        "${CMAKE_CURRENT_LIST_DIR}/../../../../../deps/include"
        "${CMAKE_CURRENT_LIST_DIR}/../../../MaaUtils/include"
        "${VCPKG_ROOT}/include"
        "${CMAKE_CURRENT_LIST_DIR}/.."
)
target_link_libraries(
    icon-recognition-custom-tests
    PRIVATE
        ${OpenCV_LIBS}
        "${CMAKE_CURRENT_LIST_DIR}/../../../../../deps/lib/MaaFramework.lib"
        "${CMAKE_CURRENT_LIST_DIR}/../../../build/lib/RelWithDebInfo/MaaUtils.lib"
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

add_executable(
    icon-recognition-debug-tests
    test_debug_capture.cpp
    ../detail/DebugCapture.cpp
    ../detail/RecognitionDiagnostics.cpp
)
target_include_directories(
    icon-recognition-debug-tests
    PRIVATE
        "${CMAKE_CURRENT_LIST_DIR}/../../../MaaUtils/include"
        "${VCPKG_ROOT}/include"
        "${CMAKE_CURRENT_LIST_DIR}/.."
)
target_link_libraries(icon-recognition-debug-tests PRIVATE ${OpenCV_LIBS})
configure_icon_recognition_test(icon-recognition-debug-tests)

add_executable(icon-recognition-expected-tests test_expected_results.cpp expected_results.cpp)
target_include_directories(
    icon-recognition-expected-tests
    PRIVATE
        "${CMAKE_CURRENT_LIST_DIR}/../../../MaaUtils/include"
        "${VCPKG_ROOT}/include"
        "${CMAKE_CURRENT_LIST_DIR}/.."
)
target_link_libraries(icon-recognition-expected-tests PRIVATE ${OpenCV_LIBS})
configure_icon_recognition_test(icon-recognition-expected-tests)

add_executable(
    icon-recognition-manual-runner
    test_full_diff.cpp
    expected_results.cpp
    manual_runner_executor.cpp
    manual_runner_config.cpp
    ../IconRecognizer.cpp
    ../detail/CompositeIcon.cpp
    ../detail/ForegroundTexture.cpp
    ../detail/GridDetector.cpp
    ../detail/GridFeatures.cpp
    ../detail/GridGeometry.cpp
    ../detail/RegularLattice.cpp
    ../detail/GridAnchors.cpp
    ../detail/GridProfiles.cpp
    ../detail/IconMatcher.cpp
    ../detail/MaskPolicy.cpp
    ../detail/RarityCandidates.cpp
    ../detail/RarityClassifier.cpp
    ../detail/TrustedRarity.cpp
    ../detail/RecognitionDiagnostics.cpp
    ../detail/SubpixelMatcher.cpp
    ../detail/TemplateCatalog.cpp
    ../detail/TemplateTypes.cpp
)
target_include_directories(
    icon-recognition-manual-runner
    PRIVATE
        "${CMAKE_CURRENT_LIST_DIR}/../../../MaaUtils/include"
        "${VCPKG_ROOT}/include"
        "${CMAKE_CURRENT_LIST_DIR}/.."
)
target_link_libraries(
    icon-recognition-manual-runner
    PRIVATE
        ${OpenCV_LIBS}
        gdi32
        user32
        "${CMAKE_CURRENT_LIST_DIR}/../../../build/lib/RelWithDebInfo/MaaUtils.lib"
)
target_compile_definitions(
    icon-recognition-manual-runner
    PRIVATE
        ICON_RECOGNITION_TEST_INPUT_DIR="${CMAKE_CURRENT_BINARY_DIR}/merged-input"
        ICON_RECOGNITION_TEST_OUTPUT_DIR="${CMAKE_CURRENT_LIST_DIR}/output"
        ICON_RECOGNITION_TEST_ROIS_PATH="${CMAKE_CURRENT_LIST_DIR}/rois.json"
        ICON_RECOGNITION_TEST_DATA_ROOT="${CMAKE_CURRENT_LIST_DIR}/../../../../../assets/data/IconRecognition"
)
configure_icon_recognition_test(icon-recognition-manual-runner)
