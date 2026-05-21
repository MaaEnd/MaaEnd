file(GLOB_RECURSE cpp_algo_src *.h *.hpp *.cpp)

add_executable(cpp-algo ${cpp_algo_src} ${cpp_algo_header})

target_include_directories(cpp-algo
    PRIVATE ${CMAKE_CURRENT_SOURCE_DIR} ${MAA_PRIVATE_INC} ${MAA_PUBLIC_INC})

target_link_libraries(cpp-algo
    PRIVATE MaaUtils MaaFramework::MaaToolkit MaaAgentServer::MaaAgentServer
    PRIVATE Boost::system ${OpenCV_LIBS} ONNXRuntime::ONNXRuntime ZLIB::ZLIB)

if(LINUX)
    target_link_libraries(cpp-algo PRIVATE pthread dl)
endif()

if(WIN32)
    target_link_libraries(cpp-algo PRIVATE WebView2::WebView2)
    target_compile_definitions(cpp-algo PRIVATE MAAEND_HAVE_WEBVIEW2)

    add_custom_command(TARGET cpp-algo POST_BUILD
        COMMAND ${CMAKE_COMMAND} -E copy_if_different
            "$<TARGET_FILE:WebView2::WebView2>"
            "$<TARGET_FILE_DIR:cpp-algo>"
        VERBATIM)

    install(IMPORTED_RUNTIME_ARTIFACTS WebView2::WebView2
        RUNTIME DESTINATION agent)
endif()

add_dependencies(cpp-algo MaaUtils)

# Set RPATH to find shared libraries in ../maafw relative to the executable
if(APPLE)
    set_target_properties(cpp-algo PROPERTIES
        INSTALL_RPATH "@executable_path/../maafw"
        BUILD_RPATH "@executable_path/../maafw"
    )
elseif(UNIX)
    set_target_properties(cpp-algo PROPERTIES
        INSTALL_RPATH "$ORIGIN/../maafw"
        BUILD_RPATH "$ORIGIN/../maafw"
    )
endif()

source_group(TREE ${CMAKE_CURRENT_SOURCE_DIR} FILES ${cpp_algo_src})

install(TARGETS cpp-algo
    RUNTIME DESTINATION agent
    LIBRARY DESTINATION agent
)

if(WIN32)
    install(FILES $<TARGET_PDB_FILE:cpp-algo> DESTINATION agent OPTIONAL)
endif()
