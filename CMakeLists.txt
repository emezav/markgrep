# markgrep -- CMake build/install driver for a pure Go program.
#
# There is no C/C++ here: CMake is used as the portable front end (configure,
# build, test, install, package) over `go build`, so the same three commands
# work on Windows and on Linux. LANGUAGES NONE means no C compiler is probed.
#
#   cmake -B build -DCMAKE_BUILD_TYPE=Release
#   cmake --build build
#   ctest --test-dir build
#   cmake --install build --prefix <somewhere>

cmake_minimum_required(VERSION 3.16)

# Single source of truth for the version: the const in main.go. Reading it here
# keeps CMake, the binary and the packages from drifting apart.
file(STRINGS "${CMAKE_CURRENT_LIST_DIR}/main.go" MARKGREP_VERSION_LINE
     REGEX "^const version = \"[0-9]+\\.[0-9]+\\.[0-9]+\"")
if(NOT MARKGREP_VERSION_LINE)
    message(FATAL_ERROR "could not read 'const version' from main.go")
endif()
string(REGEX REPLACE ".*\"([0-9]+\\.[0-9]+\\.[0-9]+)\".*" "\\1"
       MARKGREP_VERSION "${MARKGREP_VERSION_LINE}")

project(markgrep
        VERSION ${MARKGREP_VERSION}
        DESCRIPTION "grep for markdown prose: searches units, not lines"
        HOMEPAGE_URL "https://github.com/emezav/markgrep"
        LANGUAGES NONE)

# With LANGUAGES NONE there is no target architecture for GNUInstallDirs to
# read, and it warns while computing a libdir this project never uses. Setting
# it up front answers the question instead of leaving the warning in every log.
if(NOT DEFINED CMAKE_INSTALL_LIBDIR)
    set(CMAKE_INSTALL_LIBDIR "lib")
endif()
include(GNUInstallDirs)

# ---------------------------------------------------------------------------
# Options
# ---------------------------------------------------------------------------

# Debug builds keep the symbol table; anything else strips it. Multi-config
# generators leave CMAKE_BUILD_TYPE empty, which lands on stripped as well.
if(CMAKE_BUILD_TYPE STREQUAL "Debug")
    set(MARKGREP_STRIP_DEFAULT OFF)
else()
    set(MARKGREP_STRIP_DEFAULT ON)
endif()

option(MARKGREP_STRIP "Strip symbol and DWARF tables (go build -ldflags '-s -w')"
       ${MARKGREP_STRIP_DEFAULT})
option(MARKGREP_TRIMPATH "Remove local file system paths from the binary (go build -trimpath)" ON)
option(MARKGREP_BUILD_TESTS "Register ctest smoke tests and go vet" ON)
option(MARKGREP_INSTALL_DOCS "Install README.md and LICENSE next to the binary" ON)

set(MARKGREP_GOOS "" CACHE STRING "Target GOOS (empty = host); e.g. linux, windows, darwin")
set(MARKGREP_GOARCH "" CACHE STRING "Target GOARCH (empty = host); e.g. amd64, arm64")

# ---------------------------------------------------------------------------
# Toolchain
# ---------------------------------------------------------------------------

find_program(GO_EXECUTABLE NAMES go DOC "Path to the Go toolchain")
if(NOT GO_EXECUTABLE)
    message(FATAL_ERROR
            "The 'go' command was not found in PATH.\n"
            "Install Go from https://go.dev/dl/ and re-run cmake, or point at it with\n"
            "  cmake -B build -DGO_EXECUTABLE=/path/to/go")
endif()

execute_process(COMMAND "${GO_EXECUTABLE}" version
                OUTPUT_VARIABLE GO_VERSION_OUTPUT
                OUTPUT_STRIP_TRAILING_WHITESPACE
                ERROR_QUIET)
string(REGEX MATCH "go([0-9]+\\.[0-9]+(\\.[0-9]+)?)" _go_match "${GO_VERSION_OUTPUT}")
set(GO_VERSION "${CMAKE_MATCH_1}")

# go.mod declares the language version the sources need. A lower toolchain is a
# warning, not an error: since Go 1.21 GOTOOLCHAIN=auto downloads the required
# one on demand, so the build may well succeed anyway.
file(STRINGS "${CMAKE_CURRENT_SOURCE_DIR}/go.mod" GO_MOD_LINE REGEX "^go [0-9]+\\.[0-9]+")
string(REGEX REPLACE "^go ([0-9]+\\.[0-9]+).*" "\\1" GO_REQUIRED "${GO_MOD_LINE}")
if(GO_VERSION AND GO_REQUIRED AND GO_VERSION VERSION_LESS GO_REQUIRED)
    message(WARNING
            "go.mod asks for Go ${GO_REQUIRED} but ${GO_EXECUTABLE} reports ${GO_VERSION}.\n"
            "With GOTOOLCHAIN=auto (the default) Go will fetch the right toolchain; if it is\n"
            "set to 'local' the build will fail. Upgrade Go or unset GOTOOLCHAIN.")
endif()

# Ask the toolchain what the host is instead of inferring it from WIN32: the
# answer is what decides the .exe suffix and whether the built binary can be
# executed by ctest.
execute_process(COMMAND "${GO_EXECUTABLE}" env GOOS GOARCH
                OUTPUT_VARIABLE GO_HOST_ENV
                OUTPUT_STRIP_TRAILING_WHITESPACE
                ERROR_QUIET)
string(REPLACE "\n" ";" GO_HOST_ENV "${GO_HOST_ENV}")
list(GET GO_HOST_ENV 0 GO_HOST_GOOS)
list(GET GO_HOST_ENV 1 GO_HOST_GOARCH)
string(STRIP "${GO_HOST_GOOS}" GO_HOST_GOOS)
string(STRIP "${GO_HOST_GOARCH}" GO_HOST_GOARCH)

message(STATUS "markgrep ${PROJECT_VERSION} using ${GO_EXECUTABLE} (${GO_VERSION_OUTPUT})")

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

# The suffix follows the TARGET os, not the host: cross-compiling to windows
# from Linux must still produce markgrep.exe.
set(MARKGREP_TARGET_GOOS "${GO_HOST_GOOS}")
set(MARKGREP_TARGET_GOARCH "${GO_HOST_GOARCH}")
if(NOT MARKGREP_GOOS STREQUAL "")
    set(MARKGREP_TARGET_GOOS "${MARKGREP_GOOS}")
endif()
if(NOT MARKGREP_GOARCH STREQUAL "")
    set(MARKGREP_TARGET_GOARCH "${MARKGREP_GOARCH}")
endif()

if(MARKGREP_TARGET_GOOS STREQUAL "windows")
    set(MARKGREP_TARGET_WINDOWS TRUE)
else()
    set(MARKGREP_TARGET_WINDOWS FALSE)
endif()

if(MARKGREP_TARGET_WINDOWS)
    set(MARKGREP_OUTPUT_NAME "markgrep.exe")
else()
    set(MARKGREP_OUTPUT_NAME "markgrep")
endif()

set(MARKGREP_BINARY "${CMAKE_CURRENT_BINARY_DIR}/bin/${MARKGREP_OUTPUT_NAME}")

# CGO off keeps the result a single static binary and removes the need for a C
# toolchain on either platform.
set(MARKGREP_GO_ENV CGO_ENABLED=0)
if(NOT MARKGREP_GOOS STREQUAL "")
    list(APPEND MARKGREP_GO_ENV "GOOS=${MARKGREP_GOOS}")
endif()
if(NOT MARKGREP_GOARCH STREQUAL "")
    list(APPEND MARKGREP_GO_ENV "GOARCH=${MARKGREP_GOARCH}")
endif()

set(MARKGREP_GO_FLAGS "")
if(MARKGREP_TRIMPATH)
    list(APPEND MARKGREP_GO_FLAGS -trimpath)
endif()
if(MARKGREP_STRIP)
    # One argv element: go parses everything after '=' as the ldflags value.
    list(APPEND MARKGREP_GO_FLAGS "-ldflags=-s -w")
endif()

# The library lives in search/ since 0.12.0. Without its own glob a change there
# would not rebuild the binary, and `go build` would not be asked to notice.
file(GLOB MARKGREP_SOURCES CONFIGURE_DEPENDS
     "${CMAKE_CURRENT_SOURCE_DIR}/*.go"
     "${CMAKE_CURRENT_SOURCE_DIR}/search/*.go")
set(MARKGREP_DEPENDS ${MARKGREP_SOURCES} "${CMAKE_CURRENT_SOURCE_DIR}/go.mod")
if(EXISTS "${CMAKE_CURRENT_SOURCE_DIR}/go.sum")
    list(APPEND MARKGREP_DEPENDS "${CMAKE_CURRENT_SOURCE_DIR}/go.sum")
endif()

add_custom_command(
    OUTPUT "${MARKGREP_BINARY}"
    COMMAND "${CMAKE_COMMAND}" -E make_directory "${CMAKE_CURRENT_BINARY_DIR}/bin"
    COMMAND "${CMAKE_COMMAND}" -E env ${MARKGREP_GO_ENV} --
            "${GO_EXECUTABLE}" build ${MARKGREP_GO_FLAGS} -o "${MARKGREP_BINARY}" .
    DEPENDS ${MARKGREP_DEPENDS}
    WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}"
    COMMENT "Building ${MARKGREP_OUTPUT_NAME} with ${GO_EXECUTABLE}"
    VERBATIM)

add_custom_target(markgrep ALL DEPENDS "${MARKGREP_BINARY}")

# `go build` is incremental through its own cache, so a forced rebuild needs an
# explicit entry point rather than deleting the binary by hand.
add_custom_target(markgrep-rebuild
    COMMAND "${CMAKE_COMMAND}" -E env ${MARKGREP_GO_ENV} --
            "${GO_EXECUTABLE}" build -a ${MARKGREP_GO_FLAGS} -o "${MARKGREP_BINARY}" .
    WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}"
    COMMENT "Rebuilding ${MARKGREP_OUTPUT_NAME} from scratch"
    VERBATIM)

# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

if(MARKGREP_BUILD_TESTS)
    enable_testing()

    # Cross-compiled binaries cannot be executed here; only the static checks
    # remain meaningful in that case.
    set(MARKGREP_CAN_RUN TRUE)
    if(NOT MARKGREP_TARGET_GOOS STREQUAL GO_HOST_GOOS)
        set(MARKGREP_CAN_RUN FALSE)
    elseif(NOT MARKGREP_TARGET_GOARCH STREQUAL GO_HOST_GOARCH)
        set(MARKGREP_CAN_RUN FALSE)
    endif()

    add_test(NAME go_vet
             COMMAND "${GO_EXECUTABLE}" vet ./...
             WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}")

    add_test(NAME go_test
             COMMAND "${GO_EXECUTABLE}" test ./...
             WORKING_DIRECTORY "${CMAKE_CURRENT_SOURCE_DIR}")

    if(MARKGREP_CAN_RUN)
        set(MARKGREP_CORPUS "${CMAKE_CURRENT_SOURCE_DIR}/README.md")

        # The binary reports the same version CMake read out of main.go.
        add_test(NAME cli_version COMMAND "${MARKGREP_BINARY}" version)
        set_tests_properties(cli_version PROPERTIES
            PASS_REGULAR_EXPRESSION "markgrep ${PROJECT_VERSION}")

        # A phrase that lives in the README: exit 0 and the unit comes back.
        add_test(NAME cli_search_hit
                 COMMAND "${MARKGREP_BINARY}" search -F -e "searches semantic units"
                         --in "${MARKGREP_CORPUS}")
        set_tests_properties(cli_search_hit PROPERTIES
            PASS_REGULAR_EXPRESSION "matching units: [1-9]")

        # No matches is exit 1, and that contract is what callers script against.
        add_test(NAME cli_search_miss
                 COMMAND "${MARKGREP_BINARY}" search -F -e "zzz no such phrase zzz"
                         --in "${MARKGREP_CORPUS}")
        set_tests_properties(cli_search_miss PROPERTIES WILL_FAIL TRUE)

        # An input no detector can measure exits 2, not 0. Zero findings out of
        # zero detectors is not the same answer as zero findings out of two, and
        # a caller that scripts on the exit code used to be told "clean".
        # The empty file is generated here so the test carries its own fixture.
        set(MARKGREP_EMPTY "${CMAKE_CURRENT_BINARY_DIR}/nothing-to-measure.md")
        file(WRITE "${MARKGREP_EMPTY}" "")
        # Two tests, not one: ctest ignores the exit code as soon as
        # PASS_REGULAR_EXPRESSION is set, so pinning the code and pinning the
        # message have to be separate or one of them silently stops being checked.
        add_test(NAME cli_verify_nothing_measurable
                 COMMAND "${MARKGREP_BINARY}" verify --file "${MARKGREP_EMPTY}")
        set_tests_properties(cli_verify_nothing_measurable PROPERTIES WILL_FAIL TRUE)

        add_test(NAME cli_verify_nothing_measurable_says_why
                 COMMAND "${MARKGREP_BINARY}" verify --file "${MARKGREP_EMPTY}")
        set_tests_properties(cli_verify_nothing_measurable_says_why PROPERTIES
            PASS_REGULAR_EXPRESSION "cannot say clean")

        # The negative control for the test above, and it is what makes it mean
        # anything: a file that CAN be measured and is clean still exits 0.
        set(MARKGREP_MEASURABLE "${CMAKE_CURRENT_BINARY_DIR}/measurable-clean.md")
        file(WRITE "${MARKGREP_MEASURABLE}" "# A heading\n\nPlain ascii prose with no table.\n")
        add_test(NAME cli_verify_measurable_clean
                 COMMAND "${MARKGREP_BINARY}" verify --file "${MARKGREP_MEASURABLE}")
        set_tests_properties(cli_verify_measurable_clean PROPERTIES
            PASS_REGULAR_EXPRESSION "result: 0 findings")

        # Repeated filters ACCUMULATE, as in grep. Until 0.12.0 the second --exclude
        # replaced the first, so this returned the hit from the file asked out
        # first. Found by stele-mcp, letter 005. The fixture is generated here.
        set(MARKGREP_FILTERS "${CMAKE_CURRENT_BINARY_DIR}/filters")
        file(WRITE "${MARKGREP_FILTERS}/a.md" "The integrated searcher lives here.
")
        file(WRITE "${MARKGREP_FILTERS}/b.md" "The integrated searcher also here.
")
        add_test(NAME cli_search_excludes_accumulate
                 COMMAND "${MARKGREP_BINARY}" search -F -e "integrated searcher"
                         --in "${MARKGREP_FILTERS}" --exclude a.md --exclude b.md)
        set_tests_properties(cli_search_excludes_accumulate PROPERTIES
            PASS_REGULAR_EXPRESSION "corpus: 0 files")
        add_test(NAME cli_search_includes_accumulate
                 COMMAND "${MARKGREP_BINARY}" search -F -e "integrated searcher"
                         --in "${MARKGREP_FILTERS}" --include a.md --include b.md)
        set_tests_properties(cli_search_includes_accumulate PROPERTIES
            PASS_REGULAR_EXPRESSION "matching units: 2")

        # A filter that filters nothing, and a control that finds nothing, are
        # refused and say why. Only the message is pinned here: ctest stops
        # checking the exit code once PASS_REGULAR_EXPRESSION is set. Exit 2 is
        # pinned by the Go tests on search.Run and by the cmdSearch error path.
        add_test(NAME cli_search_dead_filter_says_why
                 COMMAND "${MARKGREP_BINARY}" search -F -e "integrated searcher"
                         --in "${MARKGREP_FILTERS}" --exclude "noexiste*.md")
        set_tests_properties(cli_search_dead_filter_says_why PROPERTIES
            PASS_REGULAR_EXPRESSION "matches none of the 2 files")
        add_test(NAME cli_search_failed_control_says_why
                 COMMAND "${MARKGREP_BINARY}" search -F -e "integrated searcher"
                         --in "${MARKGREP_FILTERS}" --control "zzqq absent")
        set_tests_properties(cli_search_failed_control_says_why PROPERTIES
            PASS_REGULAR_EXPRESSION "matched 0 of 2 units")

        # Line-model flags are rejected with an explanation, not accepted.
        add_test(NAME cli_rejects_line_flags
                 COMMAND "${MARKGREP_BINARY}" search -C 3 -e "anything"
                         --in "${MARKGREP_CORPUS}")
        set_tests_properties(cli_rejects_line_flags PROPERTIES
            PASS_REGULAR_EXPRESSION "not an oversight")
    else()
        message(STATUS "Cross-compiling to ${MARKGREP_TARGET_GOOS}/${MARKGREP_TARGET_GOARCH}: "
                       "the CLI tests are not registered (the binary cannot run here)")
    endif()
endif()

# ---------------------------------------------------------------------------
# Install
# ---------------------------------------------------------------------------

# PROGRAMS, not FILES: it carries the execute bit on Unix.
install(PROGRAMS "${MARKGREP_BINARY}"
        DESTINATION "${CMAKE_INSTALL_BINDIR}"
        COMPONENT runtime)

if(MARKGREP_INSTALL_DOCS)
    install(FILES "${CMAKE_CURRENT_SOURCE_DIR}/README.md"
                  "${CMAKE_CURRENT_SOURCE_DIR}/LICENSE"
            DESTINATION "${CMAKE_INSTALL_DOCDIR}"
            COMPONENT documentation)
endif()

# CMake ships no uninstall target; this one walks install_manifest.txt.
configure_file("${CMAKE_CURRENT_SOURCE_DIR}/cmake/cmake_uninstall.cmake.in"
               "${CMAKE_CURRENT_BINARY_DIR}/cmake_uninstall.cmake"
               @ONLY)
add_custom_target(uninstall
    COMMAND "${CMAKE_COMMAND}" -P "${CMAKE_CURRENT_BINARY_DIR}/cmake_uninstall.cmake"
    COMMENT "Removing the files listed in install_manifest.txt")

# ---------------------------------------------------------------------------
# Package (cpack): a redistributable archive of the same install tree
# ---------------------------------------------------------------------------

set(CPACK_PACKAGE_NAME "markgrep")
set(CPACK_PACKAGE_VENDOR "Erwin Meza Vega")
set(CPACK_PACKAGE_VERSION "${PROJECT_VERSION}")
set(CPACK_PACKAGE_DESCRIPTION_SUMMARY "${PROJECT_DESCRIPTION}")
set(CPACK_RESOURCE_FILE_LICENSE "${CMAKE_CURRENT_SOURCE_DIR}/LICENSE")
set(CPACK_PACKAGE_INSTALL_DIRECTORY "markgrep")
set(CPACK_VERBATIM_VARIABLES ON)
# Archives belong in the build tree; cpack would otherwise drop them in
# whatever directory it happened to be invoked from.
set(CPACK_PACKAGE_DIRECTORY "${CMAKE_CURRENT_BINARY_DIR}")

set(CPACK_SYSTEM_NAME "${MARKGREP_TARGET_GOOS}-${MARKGREP_TARGET_GOARCH}")

if(MARKGREP_TARGET_WINDOWS)
    set(CPACK_GENERATOR "ZIP")
else()
    set(CPACK_GENERATOR "TGZ")
endif()

include(CPack)
