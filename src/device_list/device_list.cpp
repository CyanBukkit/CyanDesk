/*
 * CrossDesk Client - Simple HTTP fetcher for online device list
 */

#include "device_list.h"

#include <httplib.h>
#include <nlohmann/json.hpp>

#include <algorithm>
#include <chrono>
#include <cmath>
#include <iostream>
#include <string>
#include <vector>

using json = nlohmann::json;

namespace crossdesk {

namespace {

// Safely extract a string from a json object field.
// Returns empty string if missing or wrong type.
std::string GetStringField(const json& j, const std::string& key) {
  if (j.contains(key) && j[key].is_string()) {
    return j[key].get<std::string>();
  }
  return {};
}

// Safely extract an int from a json object field.
// Returns 0 if missing or wrong type.
int GetIntField(const json& j, const std::string& key) {
  if (!j.contains(key)) return 0;
  const auto& v = j[key];
  if (v.is_number_integer()) {
    return v.get<int>();
  }
  if (v.is_number_float()) {
    return static_cast<int>(std::round(v.get<double>()));
  }
  if (v.is_string()) {
    // tolerate numeric string like "42"
    try {
      return std::stoi(v.get<std::string>());
    } catch (...) {
      return 0;
    }
  }
  return 0;
}

}  // namespace

std::vector<OnlineDevice> FetchOnlineDevices(const std::string& server_host,
                                              int http_port,
                                              int timeout_ms) {
  std::vector<OnlineDevice> devices;

  if (server_host.empty() || http_port <= 0 || timeout_ms <= 0) {
    return devices;
  }

  try {
    httplib::Client cli(server_host, http_port);

    // cpp-httplib timeouts are in seconds (double).
    const double timeout_sec = static_cast<double>(timeout_ms) / 1000.0;
    cli.set_connection_timeout(timeout_sec);
    cli.set_read_timeout(timeout_sec);
    cli.set_write_timeout(timeout_sec);

    httplib::Headers headers = {
        {"Accept", "application/json"},
    };

    auto res = cli.Get("/api/devices", headers);

    if (!res) {
      // Connection / DNS / timeout failure
      return devices;
    }

    if (res->status != 200) {
      // Server returned non-200
      return devices;
    }

    if (res->body.empty()) {
      return devices;
    }

    json j;
    try {
      j = json::parse(res->body);
    } catch (const json::exception&) {
      return devices;
    }

    // Accept several common JSON shapes:
    //   1. top-level array:  [ {...}, {...} ]
    //   2. wrapper w/ "data" field: { "data": [ {...} ] }
    //   3. wrapper w/ "devices" field: { "devices": [ {...} ] }
    const json* arr = nullptr;
    json data_copy;
    json devices_copy;

    if (j.is_array()) {
      arr = &j;
    } else if (j.is_object()) {
      if (j.contains("data") && j["data"].is_array()) {
        data_copy = j["data"];
        arr = &data_copy;
      } else if (j.contains("devices") && j["devices"].is_array()) {
        devices_copy = j["devices"];
        arr = &devices_copy;
      } else if (j.contains("code")) {
        // typical API style: { "code": 0, "data": [...] }
        if (j.contains("data") && j["data"].is_array()) {
          data_copy = j["data"];
          arr = &data_copy;
        }
      }
    }

    if (arr == nullptr) {
      return devices;
    }

    devices.reserve(arr->size());
    for (const auto& item : *arr) {
      if (!item.is_object()) continue;

      OnlineDevice d;
      d.user_id = GetStringField(item, "user_id");
      if (d.user_id.empty()) d.user_id = GetStringField(item, "userId");
      if (d.user_id.empty()) d.user_id = GetStringField(item, "id");

      d.device_name = GetStringField(item, "device_name");
      if (d.device_name.empty()) d.device_name = GetStringField(item, "deviceName");
      if (d.device_name.empty()) d.device_name = GetStringField(item, "name");

      d.platform = GetStringField(item, "platform");
      if (d.platform.empty()) d.platform = GetStringField(item, "os");

      d.ip = GetStringField(item, "ip");
      if (d.ip.empty()) d.ip = GetStringField(item, "ip_address");

      d.online_for_seconds = GetIntField(item, "online_for_seconds");
      if (d.online_for_seconds == 0) {
        d.online_for_seconds = GetIntField(item, "onlineForSeconds");
      }
      if (d.online_for_seconds == 0) {
        d.online_for_seconds = GetIntField(item, "online_time");
      }
      if (d.online_for_seconds == 0) {
        d.online_for_seconds = GetIntField(item, "onlineTime");
      }

      devices.push_back(std::move(d));
    }
  } catch (const std::exception&) {
    return {};
  }

  return devices;
}

}  // namespace crossdesk
