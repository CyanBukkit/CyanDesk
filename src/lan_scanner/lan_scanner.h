#ifndef _LAN_SCANNER_H_
#define _LAN_SCANNER_H_

#include <atomic>
#include <functional>
#include <string>
#include <vector>

namespace crossdesk {

struct LanDevice {
  std::string ip;
  int port;
  bool reachable;
};

// Scan local subnet for CrossDesk signal servers (default port 9090)
// Calls on_found for each reachable device found.
// Returns list of all reachable devices.
std::vector<LanDevice> ScanLan(
    int port = 9090, int timeout_ms = 50,
    std::function<void(const LanDevice&)> on_found = nullptr,
    std::atomic<bool>* cancel = nullptr);

// Get local machine's IPv4 address (first non-loopback)
std::string GetLocalIpv4();

}  // namespace crossdesk

#endif
