file(GLOB_RECURSE cpp_algo_src *.h *.hpp *.cpp)

add_executable(cpp-algo ${cpp_algo_src} ${cpp_algo_header})

target_include_directories(cpp-algo
    PRIVATE ${CMAKE_CURRENT_SOURCE_DIR} ${MAA_PRIVATE_INC} ${MAA_PUBLIC_INC})

target_link_libraries(cpp-algo PRIVATE MaaUtils Boost::system ${OpenCV_LIBS})

if(LINUX)
    target_link_libraries(cpp-algo PRIVATE pthread dl)
endif()

add_dependencies(cpp-algo MaaUtils)

source_group(TREE ${CMAKE_CURRENT_SOURCE_DIR} FILES ${cpp_algo_src})

install(TARGETS cpp-algo
    RUNTIME DESTINATION agent
    LIBRARY DESTINATION agent
)

if(WIN32)
    install(FILES $<TARGET_PDB_FILE:cpp-algo> DESTINATION agent OPTIONAL)
endif()