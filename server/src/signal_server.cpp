#include "signal_server.h"

#include <asio.hpp>

#include "common.h"
#include "log.h"

const std::string GenerateTransmissionId() {
  static const char alphanum[] = "0123456789";
  std::string random_id;
  random_id.reserve(6);

  for (int i = 0; i < 6; ++i) {
    random_id += alphanum[rand() % (sizeof(alphanum) - 1)];
  }

  return "000000";
}

SignalServer::SignalServer() {
  // Set logging settings
  server_.set_error_channels(websocketpp::log::elevel::all);
  server_.set_access_channels(websocketpp::log::alevel::none);

  // Initialize Asio
  server_.init_asio();

  server_.set_open_handler(
      std::bind(&SignalServer::on_open, this, std::placeholders::_1));

  server_.set_close_handler(
      std::bind(&SignalServer::on_close, this, std::placeholders::_1));

  server_.set_fail_handler(
      std::bind(&SignalServer::on_fail, this, std::placeholders::_1));

  server_.set_message_handler(std::bind(&SignalServer::on_message, this,
                                        std::placeholders::_1,
                                        std::placeholders::_2));

  server_.set_ping_handler(bind(&SignalServer::on_ping, this,
                                std::placeholders::_1, std::placeholders::_2));

  server_.set_pong_handler(bind(&SignalServer::on_pong, this,
                                std::placeholders::_1, std::placeholders::_2));
}

SignalServer::~SignalServer() {}

bool SignalServer::on_open(websocketpp::connection_hdl hdl) {
  ws_connections_[hdl] = ws_connection_id_++;
  return true;
}

bool SignalServer::on_close(websocketpp::connection_hdl hdl) {
  std::string user_id = transmission_manager_.ReleaseUserFromeWsHandle(hdl);

  // Unregister device from global list
  transmission_manager_.UnregisterDevice(hdl);

  if (!user_id.empty()) {
    LOG_INFO("Websocket connection [{}|{}] closed", ws_connections_[hdl],
             user_id);

    // check user is host or not
    std::string transmission_id_host = transmission_manager_.IsHost(user_id);
    if (!transmission_id_host.empty()) {
      transmission_manager_.ReleaseTransmission(transmission_id_host);
      LOG_INFO("Release transmission [{}] due to host [{}] leaves",
               transmission_id_host, user_id);

      // notify all users in transmission
      json message = {{"type", "user_leave_transmission"},
                      {"transmission_id", transmission_id_host},
                      {"user_id", user_id}};

      std::vector<std::string> user_id_list =
          transmission_manager_.GetAllUserIdOfTransmission(
              transmission_id_host);

      for (const auto& user_id : user_id_list) {
        send_msg(transmission_manager_.GetWsHandle(user_id), message);
      }
    }

    // check user is guest or not
    std::string transmission_id_guest = transmission_manager_.IsGuest(user_id);
    if (!transmission_id_guest.empty()) {
      transmission_manager_.ReleaseGuestFromTransmission(user_id);
      LOG_INFO("Release guest [{}] from transmission [{}]", user_id,
               transmission_id_guest);

      // notify all users in transmission
      json message = {{"type", "user_leave_transmission"},
                      {"transmission_id", transmission_id_guest},
                      {"user_id", user_id}};

      std::vector<std::string> user_id_list =
          transmission_manager_.GetAllUserIdOfTransmission(
              transmission_id_guest);

      for (const auto& user_id : user_id_list) {
        send_msg(transmission_manager_.GetWsHandle(user_id), message);
      }
    }
  }

  ws_connections_.erase(hdl);

  return true;
}

bool SignalServer::on_fail(websocketpp::connection_hdl hdl) {
  std::string user_id = transmission_manager_.GetUserId(hdl);
  if (!user_id.empty()) {
    LOG_INFO("Websocket connection [{}|{}] failed", ws_connections_[hdl],
             user_id);
  }
  return true;
}

bool SignalServer::on_ping(websocketpp::connection_hdl hdl, std::string s) {
  transmission_manager_.UpdateWsHandleLastActiveTime(hdl);
  return true;
}

bool SignalServer::on_pong(websocketpp::connection_hdl hdl, std::string s) {
  return true;
}

void SignalServer::run(uint16_t port, uint16_t http_port,
                      const std::string& server_password) {
  server_password_ = server_password;
  if (!server_password_.empty()) {
    LOG_INFO("Server password is set ({} chars)", server_password_.size());
  } else {
    LOG_WARN("No server password set — any client can connect");
  }

  LOG_INFO("Signal server runs on WebSocket port [{}]", port);
  if (http_port > 0) {
    http_running_ = true;
    http_thread_ = std::thread(&SignalServer::RunHttpApi, this, http_port);
    LOG_INFO("HTTP API server runs on port [{}]", http_port);
  }

  server_.set_reuse_addr(true);
  server_.listen(port);

  // Queues a connection accept operation
  server_.start_accept();

  // Start the Asio io_service run loop
  server_.run();

  http_running_ = false;
  if (http_thread_.joinable()) {
    http_thread_.join();
  }
}

void SignalServer::send_msg(websocketpp::connection_hdl hdl, json message) {
  if (!hdl.expired()) {
    server_.send(hdl, message.dump(), websocketpp::frame::opcode::text);
  } else {
    LOG_ERROR("Destination hdl invalid");
  }
}

void SignalServer::on_message(websocketpp::connection_hdl hdl,
                              server::message_ptr msg) {
  transmission_manager_.UpdateWsHandleLastActiveTime(hdl);
  std::string payload = msg->get_payload();

  auto j = json::parse(payload);
  std::string type = j["type"].get<std::string>();

  switch (HASH_STRING_PIECE(type.c_str())) {
    case "login"_H: {
      std::string host_id = j["user_id"].get<std::string>();
      if (host_id.empty()) {
        host_id = client_id_generator_.GeneratorNewId();
        LOG_INFO("New client, assign id [{}] to it", host_id);
      }

      // === Server password verification ===
      if (!server_password_.empty()) {
        std::string client_pw = j.value("server_password", "");
        if (client_pw != server_password_) {
          LOG_WARN("Login rejected: wrong server password from [{}]", host_id);
          json message = {{"type", "login"},
                          {"user_id", host_id},
                          {"status", "fail"},
                          {"reason", "Incorrect server password"}};
          send_msg(hdl, message);
          break;
        }
      }

      // === Device registration ===
      std::string device_name = j.value("device_name", "");
      std::string platform = j.value("platform", "");
      std::string ip_address = j.value("ip", "");
      // Try to get remote IP from connection
      if (ip_address.empty()) {
        try {
          ip_address = server_.get_con_from_hdl(hdl)
                           ->get_remote_endpoint();
          // extract host from "host:port"
          auto colon = ip_address.rfind(':');
          if (colon != std::string::npos) ip_address = ip_address.substr(0, colon);
        } catch (...) {
          ip_address = "unknown";
        }
      }
      transmission_manager_.RegisterDevice(host_id, device_name, platform,
                                           ip_address, hdl);

      LOG_INFO("Receive login request with id [{}]", host_id);
      bool success = transmission_manager_.BindUserToWsHandle(host_id, hdl);
      if (success) {
        json message = {
            {"type", "login"}, {"user_id", host_id}, {"status", "success"}};
        send_msg(hdl, message);
      } else {
        json message = {
            {"type", "login"}, {"user_id", host_id}, {"status", "fail"}};
        send_msg(hdl, message);
      }

      break;
    }
    case "create_transmission"_H: {
      std::string transmission_id = j["transmission_id"].get<std::string>();
      std::string password = j["password"].get<std::string>();
      std::string host_id = j["user_id"].get<std::string>();

      LOG_INFO(
          "Receive host id [{}] create transmission request with transmission "
          "id [{}]",
          host_id, transmission_id);
      if (!transmission_manager_.IsTransmissionExist(transmission_id)) {
        if (transmission_id.empty()) {
          transmission_id = GenerateTransmissionId();
          while (transmission_manager_.IsTransmissionExist(transmission_id)) {
            transmission_id = GenerateTransmissionId();
          }
          LOG_INFO(
              "Transmission id is empty, generate a new one for this request "
              "[{}]",
              transmission_id);
        }

        transmission_manager_.BindHostToTransmission(host_id, transmission_id);
        transmission_manager_.BindPasswordToTransmission(password,
                                                         transmission_id);

        LOG_INFO("Create transmission id [{}]", transmission_id);
        json message = {{"type", "transmission_id"},
                        {"transmission_id", transmission_id},
                        {"status", "success"}};
        send_msg(hdl, message);
      } else {
        LOG_INFO("Transmission id [{}] already exist", transmission_id);
        json message = {{"type", "transmission_id"},
                        {"transmission_id", transmission_id},
                        {"status", "fail"},
                        {"reason", "Transmission id exist"}};
        send_msg(hdl, message);
      }

      break;
    }
    case "leave_transmission"_H: {
      std::string transmission_id = j["transmission_id"].get<std::string>();
      std::string user_id = j["user_id"].get<std::string>();
      LOG_INFO("[{}] leaves transmission [{}]", user_id.c_str(),
               transmission_id.c_str());

      json message = {{"type", "user_leave_transmission"},
                      {"transmission_id", transmission_id},
                      {"user_id", user_id}};

      std::vector<std::string> user_id_list =
          transmission_manager_.GetAllUserIdOfTransmission(transmission_id);

      for (const auto& user : user_id_list) {
        if (user == user_id) {
          continue;
        }
        send_msg(transmission_manager_.GetWsHandle(user), message);
      }

      bool is_host =
          transmission_manager_.IsHostOfTransmission(user_id, transmission_id);

      if (is_host) {
        transmission_manager_.ReleaseTransmission(transmission_id);
        LOG_INFO("Release transmission [{}] due to host leaves",
                 transmission_id);
      } else {
        transmission_manager_.ReleaseGuestFromTransmission(user_id);
      }

      break;
    }
    case "query_user_id_list"_H: {
      std::string transmission_id = j["transmission_id"].get<std::string>();
      std::string password = j["password"].get<std::string>();

      int ret = transmission_manager_.CheckPassword(password, transmission_id);

      if (0 == ret) {
        std::vector<std::string> user_id_list =
            transmission_manager_.GetAllUserIdOfTransmission(transmission_id);

        json message = {{"type", "user_id_list"},
                        {"transmission_id", transmission_id},
                        {"user_id_list", user_id_list},
                        {"status", "success"}};

        send_msg(hdl, message);
      } else if (-1 == ret) {
        std::vector<std::string> user_id_list;
        json message = {{"type", "user_id_list"},
                        {"transmission_id", transmission_id},
                        {"user_id_list", user_id_list},
                        {"status", "failed"},
                        {"reason", "Incorrect password"}};
        // LOG_INFO(
        //     "Incorrect password [{}] for transmission [{}] with password is "
        //     "[{}]",
        //     password, transmission_id,
        //     transmission_manager_.GetPassword(transmission_id));

        send_msg(hdl, message);
      } else if (-2 == ret) {
        std::vector<std::string> user_id_list;
        json message = {{"type", "user_id_list"},
                        {"transmission_id", transmission_id},
                        {"user_id_list", user_id_list},
                        {"status", "failed"},
                        {"reason", "No such transmission id"}};
        // LOG_INFO(
        //     "Incorrect password [{}] for transmission [{}] with password is "
        //     "[{}]",
        //     password, transmission_id,
        //     transmission_manager_.GetPassword(transmission_id));

        send_msg(hdl, message);
      }

      // LOG_INFO("Send member_list: [{}]", message.dump());

      break;
    }
    case "offer"_H: {
      std::string transmission_id = j["transmission_id"].get<std::string>();
      std::string remote_user_id = j["remote_user_id"].get<std::string>();
      std::string user_id = j["user_id"].get<std::string>();

      transmission_manager_.BindGuestToTransmission(user_id, transmission_id);

      websocketpp::connection_hdl destination_hdl =
          transmission_manager_.GetWsHandle(remote_user_id);

      if (j.contains("sdp")) {
        std::string sdp = j["sdp"].get<std::string>();
        json message = {
            {"type", "offer"},
            {"transmission_id", transmission_id},
            {"remote_user_id", user_id},
            {"sdp", sdp},
        };
        LOG_INFO("[{}] send offer to [{}]", user_id, remote_user_id);
        send_msg(destination_hdl, message);

      } else {
        LOG_ERROR("Invalid offer msg");
      }

      break;
    }
    case "answer"_H: {
      std::string transmission_id = j["transmission_id"].get<std::string>();
      std::string remote_user_id = j["remote_user_id"].get<std::string>();
      std::string user_id = j["user_id"].get<std::string>();

      websocketpp::connection_hdl destination_hdl =
          transmission_manager_.GetWsHandle(remote_user_id);

      if (j.contains("sdp")) {
        std::string sdp = j["sdp"].get<std::string>();
        json message = {{"type", "answer"},
                        {"sdp", sdp},
                        {"remote_user_id", user_id},
                        {"transmission_id", transmission_id}};
        LOG_INFO("[{}] send answer to [{}]", user_id, remote_user_id);
        send_msg(destination_hdl, message);
      } else {
        LOG_ERROR("Invalid answer msg");
      }

      break;
    }
    case "new_candidate"_H: {
      std::string transmission_id = j["transmission_id"].get<std::string>();
      std::string candidate = j["sdp"].get<std::string>();
      std::string user_id = j["user_id"].get<std::string>();
      std::string remote_user_id = j["remote_user_id"].get<std::string>();

      websocketpp::connection_hdl destination_hdl =
          transmission_manager_.GetWsHandle(remote_user_id);

      // LOG_INFO("send candidate [{}]", candidate.c_str());
      json message = {{"type", "new_candidate"},
                      {"sdp", candidate},
                      {"remote_user_id", user_id},
                      {"transmission_id", transmission_id}};
      send_msg(destination_hdl, message);
      break;
    }
    default:
      break;
  }

  // std::string sdp = j["sdp"];

  // LOG_INFO("Message type: {}", type);
  // LOG_INFO("Message body: {}", sdp);

  // server_.send(hdl, msg->get_payload(), msg->get_opcode());
}

// === Simple HTTP API for device listing ===
void SignalServer::RunHttpApi(uint16_t port) {
  try {
    asio::io_context io_context;
    asio::ip::tcp::acceptor acceptor(io_context);
    asio::ip::tcp::endpoint endpoint(asio::ip::tcp::v4(), port);
    acceptor.open(endpoint.protocol());
    acceptor.set_option(asio::ip::tcp::acceptor::reuse_address(true));
    acceptor.bind(endpoint);
    acceptor.listen();

    LOG_INFO("HTTP API listening on 0.0.0.0:{}", port);

    while (http_running_) {
      asio::ip::tcp::socket socket(io_context);
      try {
        acceptor.accept(socket);
      } catch (const std::exception& e) {
        if (http_running_)
          LOG_ERROR("HTTP accept error: {}", e.what());
        break;
      }

      // Handle request synchronously (simple, single-client-at-a-time)
      try {
        char buf[4096];
        std::error_code ec;
        socket.read_some(asio::buffer(buf), ec);
        if (ec) continue;

        std::string request(buf);
        std::string response;

        // Parse path from first line: "GET /api/devices HTTP/1.1"
        auto line_end = request.find("\r\n");
        std::string first_line = request.substr(0, line_end);
        auto space1 = first_line.find(' ');
        auto space2 = first_line.rfind(' ');
        std::string method = first_line.substr(0, space1);
        std::string path = first_line.substr(space1 + 1, space2 - space1 - 1);

        if (method == "GET" && path == "/api/devices") {
          auto devices = transmission_manager_.GetAllDevices();
          json device_array = json::array();
          uint32_t now = std::chrono::duration_cast<std::chrono::seconds>(
                             std::chrono::system_clock::now().time_since_epoch())
                             .count();
          for (const auto& d : devices) {
            device_array.push_back(
                {{"user_id", d.user_id},
                 {"device_name", d.device_name},
                 {"platform", d.platform},
                 {"ip", d.ip_address},
                 {"online_for_seconds", (int)(now - d.last_seen)}});
          }
          json resp = {{"status", "ok"},
                       {"count", devices.size()},
                       {"devices", device_array},
                       {"timestamp", now}};
          std::string body = resp.dump(2);
          response = "HTTP/1.1 200 OK\r\n"
                     "Content-Type: application/json\r\n"
                     "Access-Control-Allow-Origin: *\r\n"
                     "Content-Length: " +
                     std::to_string(body.size()) + "\r\n\r\n" + body;
        } else if (method == "GET" && path == "/api/health") {
          auto devices = transmission_manager_.GetAllDevices();
          json resp = {{"status", "ok"},
                       {"online_devices", devices.size()},
                       {"server_password_set", !server_password_.empty()}};
          std::string body = resp.dump();
          response = "HTTP/1.1 200 OK\r\n"
                     "Content-Type: application/json\r\n"
                     "Access-Control-Allow-Origin: *\r\n"
                     "Content-Length: " +
                     std::to_string(body.size()) + "\r\n\r\n" + body;
        } else if (method == "OPTIONS") {
          response = "HTTP/1.1 204 No Content\r\n"
                     "Access-Control-Allow-Origin: *\r\n"
                     "Access-Control-Allow-Methods: GET, OPTIONS\r\n"
                     "Access-Control-Allow-Headers: *\r\n\r\n";
        } else {
          response = "HTTP/1.1 404 Not Found\r\n"
                     "Content-Length: 0\r\n\r\n";
        }

        asio::write(socket, asio::buffer(response));
      } catch (const std::exception& e) {
        LOG_ERROR("HTTP handler error: {}", e.what());
      }
    }
  } catch (const std::exception& e) {
    LOG_ERROR("HTTP API server fatal error: {}", e.what());
  }
}