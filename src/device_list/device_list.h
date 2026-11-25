#ifndef _DEVICE_LIST_H_
#define _DEVICE_LIST_H_
#include <string>
#include <vector>
namespace crossdesk {
struct OnlineDevice {
  std::string user_id;
  std::string device_name;
  std::string platform;
  std::string ip;
  int online_for_seconds;
};
// Fetch online device list from CrossDesk server HTTP API
// server_host: e.g. "192.168.1.100" or "api.crossdesk.cn"
// http_port: default 9091
// Returns empty vector on failure
std::vector<OnlineDevice> FetchOnlineDevices(const std::string& server_host,
                                              int http_port = 9091,
                                              int timeout_ms = 2000);
}  // namespace crossdesk
#endif
