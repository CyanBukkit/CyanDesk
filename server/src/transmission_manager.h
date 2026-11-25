#ifndef _TRANSIMISSION_MANAGER_H_
#define _TRANSIMISSION_MANAGER_H_

#include <list>
#include <map>
#include <mutex>
#include <string>
#include <thread>
#include <websocketpp/server.hpp>

struct DeviceInfo {
  std::string user_id;
  std::string device_name;
  std::string platform;
  std::string ip_address;
  websocketpp::connection_hdl hdl;
  uint32_t last_seen;
};

class TransmissionManager {
 public:
  TransmissionManager();
  ~TransmissionManager();

 public:
  bool IsTransmissionExist(const std::string& transmission_id);
  bool ReleaseTransmission(const std::string& transmission_id);

  std::string IsHost(const std::string& user_id);
  std::string IsGuest(const std::string& user_id);
  bool IsHostOfTransmission(const std::string& user_id,
                            const std::string& transmission_id);

 public:
  std::vector<std::string> GetAllUserIdOfTransmission(
      const std::string& transmission_id);

 public:
  bool BindHostToTransmission(const std::string& host_id,
                              const std::string& transmission_id);
  bool BindGuestToTransmission(const std::string& guest_id,
                               const std::string& transmission_id);
  bool BindPasswordToTransmission(const std::string& password,
                                  const std::string& transmission_id);
  bool BindUserToWsHandle(const std::string& user_id,
                          websocketpp::connection_hdl hdl);

 public:
  bool ReleaseGuestFromTransmission(const std::string& guest_id);
  bool ReleasePasswordFromTransmission(const std::string& transmission_id);
  std::string ReleaseUserFromeWsHandle(websocketpp::connection_hdl hdl);

 public:
  websocketpp::connection_hdl GetWsHandle(const std::string& user_id);
  std::string GetUserId(websocketpp::connection_hdl hdl);
  int CheckPassword(const std::string& password,
                    const std::string& transmission_id);
  std::string GetPassword(const std::string& transmission_id);

 public:
  int UpdateWsHandleLastActiveTime(websocketpp::connection_hdl hdl);
  void AliveChecker();

  // === Device registry (for machine listing) ===
  void RegisterDevice(const std::string& user_id, const std::string& device_name,
                      const std::string& platform, const std::string& ip,
                      websocketpp::connection_hdl hdl);
  void UnregisterDevice(websocketpp::connection_hdl hdl);
  std::vector<DeviceInfo> GetAllDevices() const;
  DeviceInfo GetDevice(const std::string& user_id) const;
  bool IsDeviceRegistered(const std::string& user_id) const;

 private:
  std::map<std::string, DeviceInfo> devices_;
  mutable std::recursive_mutex devices_mutex_;

 private:
  std::map<std::string, std::string> transmission_host_id_list_;
  std::map<std::string, std::vector<std::string>> transmission_guest_id_list_;
  std::map<std::string, std::string> transmission_password_list_;
  std::map<std::string, websocketpp::connection_hdl> user_id_ws_hdl_list_;

 private:
  std::list<std::pair<websocketpp::connection_hdl, uint32_t>>
      ws_hdl_last_active_time_list_;
  std::map<
      websocketpp::connection_hdl,
      std::list<std::pair<websocketpp::connection_hdl, uint32_t>>::iterator,
      std::owner_less<websocketpp::connection_hdl>>
      ws_hdl_iter_list_;
  std::thread ws_hdl_alive_checker_;
  std::recursive_mutex ws_hdl_alive_checker_mutex_;
};

#endif