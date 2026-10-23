add_executable(cloud-webview
    main.cpp
    CloudWindow.cpp
    CloudBridge.cpp
    cloud_protocol.cpp
    ../source/Common/FramelessWindow.cpp
    ../source/Common/WebView2.cpp
)
target_include_directories(cloud-webview PRIVATE ../source)
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
