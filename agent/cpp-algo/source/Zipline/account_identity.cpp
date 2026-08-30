#include "account_identity.h"

#include <algorithm>
#include <array>
#include <cctype>
#include <filesystem>
#include <fstream>
#include <iterator>
#include <string>
#include <system_error>

#ifdef _WIN32
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h>

#include <bcrypt.h>
#endif

#include <MaaUtils/Logger.h>

#include "../utils.h"

namespace zipline
{

namespace
{

constexpr size_t kMinUidDigits = 8;
constexpr size_t kMaxUidDigits = 12;
constexpr size_t kSha256Bytes = 32;
constexpr size_t kAccountIdHexLength = 16;
constexpr char kHexDigits[] = "0123456789abcdef";

std::filesystem::path salt_path()
{
    // go-service 的工作目录是 <install>，其 debug/record/random_salt.txt 与这里从
    // <install>/agent/cpp-algo.exe 锚定出来的是同一个文件。
    return get_exe_dir() / ".." / "debug" / "record" / "random_salt.txt";
}

std::string trim(std::string value)
{
    const auto is_space = [](unsigned char ch) {
        return std::isspace(ch) != 0;
    };
    value.erase(value.begin(), std::find_if_not(value.begin(), value.end(), is_space));
    value.erase(std::find_if_not(value.rbegin(), value.rend(), is_space).base(), value.end());
    return value;
}

template <size_t Size>
std::string hex(const std::array<unsigned char, Size>& bytes)
{
    std::string out;
    out.resize(Size * 2);
    for (size_t i = 0; i < bytes.size(); ++i) {
        out[i * 2] = kHexDigits[bytes[i] >> 4];
        out[i * 2 + 1] = kHexDigits[bytes[i] & 0x0f];
    }
    return out;
}

std::optional<std::string> load_or_create_salt()
{
    const std::filesystem::path path = salt_path();
    {
        std::ifstream ifs(path, std::ios::binary);
        if (ifs) {
            const std::string salt = trim(std::string((std::istreambuf_iterator<char>(ifs)), std::istreambuf_iterator<char>()));
            if (!salt.empty()) {
                return salt;
            }
        }
    }

#ifdef _WIN32
    constexpr size_t kSaltBytes = 16;
    std::array<unsigned char, kSaltBytes> bytes { };
    if (BCryptGenRandom(nullptr, bytes.data(), static_cast<ULONG>(bytes.size()), BCRYPT_USE_SYSTEM_PREFERRED_RNG) < 0) {
        LogError << "ZiplineAccount: failed to generate random salt";
        return std::nullopt;
    }
    const std::string salt = hex(bytes);

    std::error_code ec;
    std::filesystem::create_directories(path.parent_path(), ec);
    if (ec) {
        LogError << "ZiplineAccount: failed to create salt directory" << VAR(path) << VAR(ec.message());
        return std::nullopt;
    }
    std::ofstream ofs(path, std::ios::binary | std::ios::trunc);
    if (!ofs) {
        LogError << "ZiplineAccount: failed to open salt file" << VAR(path);
        return std::nullopt;
    }
    ofs << salt;
    if (!ofs) {
        LogError << "ZiplineAccount: failed to write salt file" << VAR(path);
        return std::nullopt;
    }
    return salt;
#else
    LogError << "ZiplineAccount: account hashing is unavailable on this platform";
    return std::nullopt;
#endif
}

std::optional<std::array<unsigned char, kSha256Bytes>> sha256(std::string_view input)
{
#ifdef _WIN32
    BCRYPT_ALG_HANDLE algorithm = nullptr;
    if (BCryptOpenAlgorithmProvider(&algorithm, BCRYPT_SHA256_ALGORITHM, nullptr, 0) < 0) {
        LogError << "ZiplineAccount: failed to open SHA-256 provider";
        return std::nullopt;
    }

    std::array<unsigned char, kSha256Bytes> digest { };
    const auto* data = reinterpret_cast<const unsigned char*>(input.data());
    const NTSTATUS status = BCryptHash(
        algorithm,
        nullptr,
        0,
        const_cast<unsigned char*>(data),
        static_cast<ULONG>(input.size()),
        digest.data(),
        static_cast<ULONG>(digest.size()));
    BCryptCloseAlgorithmProvider(algorithm, 0);
    if (status < 0) {
        LogError << "ZiplineAccount: SHA-256 failed";
        return std::nullopt;
    }
    return digest;
#else
    (void)input;
    LogError << "ZiplineAccount: SHA-256 is unavailable on this platform";
    return std::nullopt;
#endif
}

} // namespace

bool IsValidRawUid(std::string_view uid)
{
    return uid.size() >= kMinUidDigits && uid.size() <= kMaxUidDigits
           && std::all_of(uid.begin(), uid.end(), [](unsigned char ch) { return std::isdigit(ch) != 0; });
}

std::optional<std::string> HashUidForAccount(std::string_view uid)
{
    if (!IsValidRawUid(uid)) {
        LogError << "ZiplineAccount: UID format is invalid" << VAR(uid.size());
        return std::nullopt;
    }
    const auto salt = load_or_create_salt();
    if (!salt) {
        return std::nullopt;
    }
    const auto digest = sha256(std::string(uid) + *salt);
    if (!digest) {
        return std::nullopt;
    }
    return hex(*digest).substr(0, kAccountIdHexLength);
}

} // namespace zipline
