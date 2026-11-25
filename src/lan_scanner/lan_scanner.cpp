// lan_scanner.cpp - Cross-platform LAN scanner for CrossDesk signal servers
//
// Platforms: Windows (Winsock) + Linux (POSIX sockets)
// Dependencies: C++17 std library + platform network APIs only.

#include "lan_scanner.h"

#include <algorithm>
#include <array>
#include <cstdint>
#include <cstring>
#include <mutex>
#include <thread>

// ---------------------------------------------------------------------------
// Platform detection & includes
// ---------------------------------------------------------------------------

#if defined(_WIN32) || defined(_WIN64)
#define CROSSDESK_WINDOWS 1
#else
#define CROSSDESK_WINDOWS 0
#endif

#if CROSSDESK_WINDOWS
#include <winsock2.h>
#include <ws2tcpip.h>
#pragma comment(lib, "ws2_32.lib")
using sock_t = SOCKET;
#define INVALID_SOCK INVALID_SOCKET
#define CLOSE_SOCKET(s) closesocket(s)
#else
#include <arpa/inet.h>
#include <ifaddrs.h>
#include <netinet/in.h>
#include <sys/socket.h>
#include <sys/types.h>
#include <unistd.h>
#include <fcntl.h>
using sock_t = int;
#define INVALID_SOCK (-1)
#define CLOSE_SOCKET(s) ::close(s)
#endif

namespace crossdesk {
namespace {

// ---------------------------------------------------------------------------
// Socket lifecycle helpers
// ---------------------------------------------------------------------------

void EnsureSocketInit() {
#if CROSSDESK_WINDOWS
  static std::once_flag s_init;
  std::call_once(s_init, []() {
    WSADATA wd;
    // Best-effort; ignore failure, callers will see INVALID_SOCKET anyway.
    (void)WSAStartup(MAKEWORD(2, 2), &wd);
  });
#endif
}

void SetNonBlocking(sock_t s) {
#if CROSSDESK_WINDOWS
  u_long mode = 1;
  ioctlsocket(s, FIONBIO, &mode);
#else
  int flags = fcntl(s, F_GETFL, 0);
  fcntl(s, F_SETFL, flags | O_NONBLOCK);
#endif
}

// Returns true if a blocking connect() would succeed within timeout_ms.
// Uses a non-blocking connect + select/poll trick.
bool TcpConnectWithTimeout(const std::string& ip, int port, int timeout_ms) {
  sock_t s = socket(AF_INET, SOCK_STREAM, 0);
  if (s == INVALID_SOCK) return false;

  SetNonBlocking(s);

  struct sockaddr_in addr{};
  addr.sin_family = AF_INET;
  addr.sin_port = htons(static_cast<uint16_t>(port));
  if (inet_pton(AF_INET, ip.c_str(), &addr.sin_addr) != 1) {
    CLOSE_SOCKET(s);
    return false;
  }

  int rc = connect(s, reinterpret_cast<struct sockaddr*>(&addr), sizeof(addr));
  if (rc == 0) {
    // Connected immediately.
    CLOSE_SOCKET(s);
    return true;
  }

#if CROSSDESK_WINDOWS
  int err = WSAGetLastError();
  if (err != WSAEWOULDBLOCK && err != WSAEINPROGRESS) {
    CLOSE_SOCKET(s);
    return false;
  }
  fd_set writefds;
  FD_ZERO(&writefds);
  FD_SET(s, &writefds);
  struct timeval tv;
  tv.tv_sec = timeout_ms / 1000;
  tv.tv_usec = (timeout_ms % 1000) * 1000;
  rc = select(static_cast<int>(s) + 1, nullptr, &writefds, nullptr, &tv);
  if (rc <= 0) {
    CLOSE_SOCKET(s);
    return false;
  }
  int so_err = 0;
  int len = sizeof(so_err);
  getsockopt(s, SOL_SOCKET, SO_ERROR, reinterpret_cast<char*>(&so_err), &len);
  CLOSE_SOCKET(s);
  return so_err == 0;
#else
  // On POSIX, non-blocking connect returns -1 with errno EINPROGRESS.
  int err_save = errno;
  if (!(rc < 0 && (err_save == EINPROGRESS || err_save == EALREADY))) {
    CLOSE_SOCKET(s);
    return false;
  }
  fd_set writefds;
  FD_ZERO(&writefds);
  FD_SET(s, &writefds);
  struct timeval tv;
  tv.tv_sec = timeout_ms / 1000;
  tv.tv_usec = (timeout_ms % 1000) * 1000;
  rc = select(s + 1, nullptr, &writefds, nullptr, &tv);
  if (rc <= 0) {
    CLOSE_SOCKET(s);
    return false;
  }
  int so_err = 0;
  socklen_t len = sizeof(so_err);
  getsockopt(s, SOL_SOCKET, SO_ERROR, &so_err, &len);
  CLOSE_SOCKET(s);
  return so_err == 0;
#endif
}

// ---------------------------------------------------------------------------
// GetLocalIpv4 implementations
// ---------------------------------------------------------------------------

#if CROSSDESK_WINDOWS
// UDP connect trick: connect a UDP socket to a public address (8.8.8.8),
// then getsockname reveals the local interface's IP. This avoids pulling in
// GetAdaptersAddresses overhead.
std::string GetLocalIpv4Impl() {
  EnsureSocketInit();
  sock_t s = socket(AF_INET, SOCK_DGRAM, 0);
  if (s == INVALID_SOCK) return {};

  struct sockaddr_in remote{};
  remote.sin_family = AF_INET;
  remote.sin_port = htons(53);
  inet_pton(AF_INET, "8.8.8.8", &remote.sin_addr);

  int rc = connect(s, reinterpret_cast<struct sockaddr*>(&remote), sizeof(remote));
  if (rc != 0) {
    CLOSE_SOCKET(s);
    return {};
  }

  struct sockaddr_in local{};
  int local_len = sizeof(local);
  rc = getsockname(s, reinterpret_cast<struct sockaddr*>(&local), &local_len);
  CLOSE_SOCKET(s);
  if (rc != 0) return {};

  char buf[INET_ADDRSTRLEN] = {};
  inet_ntop(AF_INET, &local.sin_addr, buf, sizeof(buf));
  std::string ip(buf);
  if (ip == "127.0.0.1") return {};
  return ip;
}
#else
std::string GetLocalIpv4Impl() {
  struct ifaddrs* head = nullptr;
  if (getifaddrs(&head) != 0) return {};

  std::string result;
  for (struct ifaddrs*ifa = head; ifa != nullptr; ifa = ifa->ifa_next) {
    if (!ifa->ifa_addr) continue;
    if ((ifa->ifa_flags & IFF_LOOPBACK) != 0) continue;
    if (ifa->ifa_addr->sa_family != AF_INET) continue;

    auto* sa = reinterpret_cast<struct sockaddr_in*>(ifa->ifa_addr);
    char buf[INET_ADDRSTRLEN] = {};
    inet_ntop(AF_INET, &sa->sin_addr, buf, sizeof(buf));
    std::string ip(buf);
    if (ip.empty() || ip == "127.0.0.1") continue;
    result = ip;
    break;
  }
  freeifaddrs(head);
  return result;
}
#endif

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

std::vector<std::string> SubnetCandidates(const std::string& local_ip) {
  // For a.b.c.d -> scan a.b.c.1 .. a.b.c.254
  int a = 0, b = 0, c = 0, d = 0;
  if (sscanf(local_ip.c_str(), "%d.%d.%d.%d", &a, &b, &c, &d) != 4) return {};
  if (a < 0 || a > 255 || b < 0 || b > 255 || c < 0 || c > 255 || d < 0 || d > 255) {
    return {};
  }

  std::vector<std::string> out;
  out.reserve(254);
  char buf[INET_ADDRSTRLEN];
  for (int i = 1; i <= 254; ++i) {
    if (i == d) continue;  // skip self
    snprintf(buf, sizeof(buf), "%d.%d.%d.%d", a, b, c, i);
    out.emplace_back(buf);
  }
  // Put the local machine last so it still gets checked if it runs the server.
  snprintf(buf, sizeof(buf), "%d.%d.%d.%d", a, b, c, d);
  out.emplace_back(buf);
  return out;
}

}  // namespace

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

std::string GetLocalIpv4() { return GetLocalIpv4Impl(); }

std::vector<LanDevice> ScanLan(
    int port, int timeout_ms,
    std::function<void(const LanDevice&)> on_found,
    std::atomic<bool>* cancel) {
  EnsureSocketInit();

  std::vector<LanDevice> results;

  std::string local_ip = GetLocalIpv4Impl();
  if (local_ip.empty()) {
    // No usable IPv4 address on this machine.
    return results;
  }

  std::vector<std::string> targets = SubnetCandidates(local_ip);
  if (targets.empty()) return results;

  // Thread pool sized to number of CPUs, capped at 32 to stay polite.
  unsigned hw = std::thread::hardware_concurrency();
  int workers = static_cast<int>(hw == 0 ? 8 : std::min(hw, 32u));

  std::mutex mtx;
  std::atomic<size_t> idx{0};
  std::vector<std::thread> pool;
  pool.reserve(workers);

  auto worker = [&]() {
    while (true) {
      if (cancel && cancel->load()) return;
      size_t i = idx.fetch_add(1);
      if (i >= targets.size()) return;

      const std::string& ip = targets[i];
      bool reachable = TcpConnectWithTimeout(ip, port, timeout_ms);
      if (!reachable) continue;

      LanDevice dev{ip, port, true};
      {
        std::lock_guard<std::mutex> g(mtx);
        results.push_back(dev);
      }
      if (on_found) {
        // Invoke outside the lock so the callback can safely call ScanLan
        // again or do its own synchronization.
        on_found(dev);
      }
    }
  };

  for (int w = 0; w < workers; ++w) {
    pool.emplace_back(worker);
  }
  for (auto& t : pool) {
    if (t.joinable()) t.join();
  }

  std::sort(results.begin(), results.end(),
            [](const LanDevice& a, const LanDevice& b) { return a.ip < b.ip; });
  return results;
}

}  // namespace crossdesk
