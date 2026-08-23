set(icon_recognition_core_src
    IconRecognition/IconRecognizer.cpp
    IconRecognition/detail/CompositeIcon.cpp
    IconRecognition/detail/DebugCapture.cpp
    IconRecognition/detail/EdgeOcclusion.cpp
    IconRecognition/detail/ForegroundTexture.cpp
    IconRecognition/detail/GridAnchors.cpp
    IconRecognition/detail/GridDetector.cpp
    IconRecognition/detail/GridFeatures.cpp
    IconRecognition/detail/GridGeometry.cpp
    IconRecognition/detail/GridProfiles.cpp
    IconRecognition/detail/IconMatcher.cpp
    IconRecognition/detail/MaskPolicy.cpp
    IconRecognition/detail/RarityCandidates.cpp
    IconRecognition/detail/RarityClassifier.cpp
    IconRecognition/detail/RecognitionDiagnostics.cpp
    IconRecognition/detail/RegularLattice.cpp
    IconRecognition/detail/SubpixelMatcher.cpp
    IconRecognition/detail/TemplateCatalog.cpp
    IconRecognition/detail/TemplateTypes.cpp
    IconRecognition/detail/TrustedRarity.cpp
)
list(TRANSFORM icon_recognition_core_src PREPEND "${CMAKE_CURRENT_SOURCE_DIR}/")

# 核心识别实现在生产程序和测试间共享编译产物；该目标没有安装规则，不会进入发布包。
add_library(icon-recognition-core STATIC ${icon_recognition_core_src})
target_include_directories(icon-recognition-core
    PUBLIC ${CMAKE_CURRENT_SOURCE_DIR} ${MAA_PRIVATE_INC} ${MAA_PUBLIC_INC})
target_link_libraries(icon-recognition-core
    PUBLIC MaaUtils ${OpenCV_LIBS})
add_dependencies(icon-recognition-core MaaUtils)

file(GLOB_RECURSE cpp_algo_src CONFIGURE_DEPENDS *.h *.hpp *.cpp)
list(FILTER cpp_algo_src EXCLUDE REGEX "/IconRecognition/test/")
list(REMOVE_ITEM cpp_algo_src ${icon_recognition_core_src})

add_executable(cpp-algo ${cpp_algo_src} ${cpp_algo_header})

target_include_directories(cpp-algo
    PRIVATE ${CMAKE_CURRENT_SOURCE_DIR} ${MAA_PRIVATE_INC} ${MAA_PUBLIC_INC})

target_link_libraries(cpp-algo
    PRIVATE icon-recognition-core MaaUtils MaaFramework::MaaToolkit MaaAgentServer::MaaAgentServer
    PRIVATE Boost::system ${OpenCV_LIBS} ONNXRuntime::ONNXRuntime ZLIB::ZLIB)

if(LINUX)
    target_link_libraries(cpp-algo PRIVATE pthread dl)
endif()

if(WIN32)
    # dbghelp 静态链接，而不是崩溃时 LoadLibrary，以免撞上 loader lock。
    # psapi/pdh 供 SystemMonitor 采内存与 GPU Engine 计数器。
    target_link_libraries(cpp-algo PRIVATE psapi pdh WebView2::WebView2 dbghelp)
    target_compile_definitions(cpp-algo PRIVATE MAAEND_HAVE_WEBVIEW2)

    # 固定产出完整 PDB（/DEBUG:FULL）：CI 符号包依赖行号信息，
    # 避免 MSVC 默认（历史上有 FASTLINK 默认期）时 dump_syms 拿不到行号。
    target_link_options(cpp-algo PRIVATE "$<$<CONFIG:RelWithDebInfo>:/DEBUG:FULL>")

    add_custom_command(TARGET cpp-algo POST_BUILD
        COMMAND ${CMAKE_COMMAND} -E copy_if_different
            "$<TARGET_FILE:WebView2::WebView2>"
            "$<TARGET_FILE_DIR:cpp-algo>"
        VERBATIM)

    install(IMPORTED_RUNTIME_ARTIFACTS WebView2::WebView2
        RUNTIME DESTINATION agent)
endif()

add_dependencies(cpp-algo MaaUtils)

if(MAAEND_BUILD_ICON_RECOGNITION_TESTS)
    add_subdirectory(IconRecognition/test)
endif()

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
    # cpp-algo imports zlib on Windows; install the app-local runtime beside the exe.
    install(FILES "$<TARGET_FILE_DIR:cpp-algo>/z.dll" DESTINATION agent OPTIONAL)
    install(FILES $<TARGET_PDB_FILE:cpp-algo> DESTINATION agent OPTIONAL)
endif()
