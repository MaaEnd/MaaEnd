add_executable(cloud-webview
    main.cpp
    CloudWindow.cpp
    CloudBridge.cpp
    Win32ControllerBridge.cpp
    transport/win_cloud_transport.cpp
    cloud_protocol.cpp
    ../source/Common/FramelessWindow.cpp
    ../source/Common/WebView2.cpp
)
target_include_directories(cloud-webview PRIVATE ../source ${CMAKE_CURRENT_SOURCE_DIR})
target_link_libraries(cloud-webview PRIVATE MaaUtils WebView2::WebView2 ${OpenCV_LIBS})
if(MSVC)
    # Layered child windows require a Windows 8+ compatibility declaration.
    target_sources(cloud-webview PRIVATE cloud-webview.manifest)
endif()
add_custom_command(TARGET cloud-webview POST_BUILD
    COMMAND ${CMAKE_COMMAND} -E copy_if_different
        "$<TARGET_FILE:WebView2::WebView2>"
        "$<TARGET_FILE_DIR:cloud-webview>"
    VERBATIM)
install(TARGETS cloud-webview RUNTIME DESTINATION agent)
# The pre-task is also launched outside MXU's MaaFramework DLL search path.
install(FILES "$<TARGET_FILE:MaaUtils>" DESTINATION agent)
install(IMPORTED_RUNTIME_ARTIFACTS WebView2::WebView2 RUNTIME DESTINATION agent)

if(MAAEND_BUILD_CLOUD_WEBVIEW_TESTS)
    add_executable(cloud-win32-bridge-test
        tests/win32_bridge_test.cpp Win32ControllerBridge.cpp transport/win_cloud_transport.cpp
        cloud_protocol.cpp ../source/Common/CloudViewInput.cpp)
    target_include_directories(cloud-win32-bridge-test PRIVATE ${CMAKE_CURRENT_SOURCE_DIR} ../source)
    target_link_libraries(cloud-win32-bridge-test PRIVATE MaaUtils MaaFramework::MaaFramework MaaFramework::MaaToolkit ${OpenCV_LIBS})
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
