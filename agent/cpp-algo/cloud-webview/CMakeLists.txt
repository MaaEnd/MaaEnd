file(READ "${CMAKE_CURRENT_SOURCE_DIR}/scripts/input_policy.js" kInputPolicy)
file(READ "${CMAKE_CURRENT_SOURCE_DIR}/scripts/capture_video_png.js" kCaptureVideoPng)
file(READ "${CMAKE_CURRENT_SOURCE_DIR}/scripts/capture_video_frame.js" kCaptureVideoFrame)
file(READ "${CMAKE_CURRENT_SOURCE_DIR}/scripts/page_state.js" kPageState)
file(READ "${CMAKE_CURRENT_SOURCE_DIR}/scripts/diagnostics.js" kDiagnostics)
set_property(DIRECTORY APPEND PROPERTY CMAKE_CONFIGURE_DEPENDS
    "${CMAKE_CURRENT_SOURCE_DIR}/scripts/input_policy.js"
    "${CMAKE_CURRENT_SOURCE_DIR}/scripts/capture_video_png.js"
    "${CMAKE_CURRENT_SOURCE_DIR}/scripts/capture_video_frame.js"
    "${CMAKE_CURRENT_SOURCE_DIR}/scripts/page_state.js"
    "${CMAKE_CURRENT_SOURCE_DIR}/scripts/diagnostics.js"
)
configure_file(cloud_scripts.h.in cloud_scripts.h @ONLY)

# The existing C++ Agent hosts the browser in a separate process mode. No
# additional executable or entry point is installed.
target_sources(cpp-algo PRIVATE
    CloudHost.cpp CloudWindow.cpp CloudBridge.cpp Win32ControllerBridge.cpp
    transport/win_cloud_transport.cpp cloud_protocol.cpp)
target_link_libraries(cpp-algo PRIVATE crypt32)
target_include_directories(cpp-algo PRIVATE ${CMAKE_CURRENT_SOURCE_DIR} ${CMAKE_CURRENT_BINARY_DIR})
if(MSVC)
    target_sources(cpp-algo PRIVATE cloud-webview.manifest)
endif()
install(FILES "$<TARGET_FILE:MaaUtils>" DESTINATION agent)

if(MAAEND_BUILD_CLOUD_WEBVIEW_TESTS)
    add_executable(cloud-win32-bridge-test
        tests/win32_bridge_test.cpp Win32ControllerBridge.cpp transport/win_cloud_transport.cpp
        cloud_protocol.cpp ../source/Common/CloudViewInput.cpp)
    target_include_directories(cloud-win32-bridge-test PRIVATE ${CMAKE_CURRENT_SOURCE_DIR} ../source)
    target_link_libraries(cloud-win32-bridge-test PRIVATE MaaUtils MaaFramework::MaaFramework MaaFramework::MaaToolkit ${OpenCV_LIBS} crypt32)
    if(MSVC)
        target_compile_options(cloud-win32-bridge-test PRIVATE /utf-8)
    endif()
    # Framework resolves control-unit plugins beside the executable, even when
    # its import DLL was found through PATH. These copies are test-only.
    file(GLOB bridge_test_runtime "${DEPS_DIR}/bin/*.dll")
    list(FILTER bridge_test_runtime EXCLUDE REGEX "/MaaUtils\\.dll$")
    add_custom_command(TARGET cloud-win32-bridge-test POST_BUILD
        COMMAND ${CMAKE_COMMAND} -E copy_if_different ${bridge_test_runtime}
            "$<TARGET_FILE_DIR:cloud-win32-bridge-test>"
        VERBATIM)
    add_test(NAME cloud-win32-bridge COMMAND cloud-win32-bridge-test)
    # Requires an idle interactive Windows desktop and DWM. Run explicitly on
    # a test machine instead of treating desktop state as a policy CI failure.
    set_tests_properties(cloud-win32-bridge PROPERTIES LABELS "cloud-desktop" RUN_SERIAL TRUE TIMEOUT 90)
endif()
