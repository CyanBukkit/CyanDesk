#include "signal_server.h"

#include <asio.hpp>

#if CROSSDESK_SERVER_TLS
#include <openssl/ssl.h>
#include <openssl/x509.h>
#include <openssl/x509_vfy.h>
#endif

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

SignalServer::SignalServer() {}
SignalServer::~SignalServer() {}

#if CROSSDESK_SERVER_TLS
context_ptr SignalServer::OnTlsInit(websocketpp::connection_hdl hdl) {
  auto ctx = std::make_shared<asio::ssl::context>(asio::ssl::context::tlsv12);
  try {
    ctx->set_options(
        asio::ssl::context::default_workarounds | asio::ssl::context::no_sslv2 |
        asio::ssl::context::no_sslv3 | asio::ssl::context::single_dh_use);

    if (mtls_enabled_) {
      // mTLS: load CA and require client cert
      if (!ca_cert_file_.empty()) {
        ctx->load_verify_file(ca_cert_file_);
        ctx->set_verify_mode(asio::ssl::verify_peer |
                             asio::ssl::verify_fail_if_no_peer_cert);
        LOG_INFO("mTLS enabled: CA={}", ca_cert_file_);
      }
    } else {
      ctx->set_verify_mode(asio::ssl::verify_none);
    }
  } catch (std::exception& e) {
    LOG_ERROR("TLS init error: {}", e.what());
  }
  return ctx;
}

bool SignalServer::OnTlsVerify(bool preverified, asio::ssl::verify_context& ctx) {
  if (mtls_enabled_) {
    std::lock_guard<std::mutex> lock(peer_cert_mutex_);
    peer_cert_verified_[ctx.native_handle() ? websocketpp::connection_hdl()
                                           : websocketpp::connection_hdl()] =
        preverified;
  }
  return preverified;
}
#endif

bool SignalServer::on_open(websocketpp::connection_hdl hdl) {
  ws_connections_[hdl] = ws_connection_id_++;
  return true;
}

bool SignalServer::on_close(websocketpp::connection_hdl hdl) {
  std::string user_id = transmission_manager_.ReleaseUserFromeWsHandle(hdl);

  transmission_manager_.UnregisterDevice(hdl);

  {
    std::lock_guard<std::mutex> lock(peer_cert_mutex_);
    peer_cert_verified_.erase(hdl);
  }

  if (!user_id.empty()) {
    LOG_INFO("Websocket connection [{}|{}] closed", ws_connections_[hdl],
             user_id);

    std::string transmission_id_host = transmission_manager_.IsHost(user_id);
    if (!transmission_id_host.empty()) {
      transmission_manager_.ReleaseTransmission(transmission_id_host);
      LOG_INFO("Release transmission [{}] due to host [{}] leaves",
               transmission_id_host, user_id);

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

    std::string transmission_id_guest = transmission_manager_.IsGuest(user_id);
    if (!transmission_id_guest.empty()) {
      transmission_manager_.ReleaseGuestFromTransmission(user_id);
      LOG_INFO("Release guest [{}] from transmission [{}]", user_id,
               transmission_id_guest);

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
                      const std::string& server_password,
                      const std::string& ssl_cert_file,
                      const std::string& ssl_key_file,
                      const std::string& ca_cert_file) {
  server_password_ = server_password;
  ca_cert_file_ = ca_cert_file;

  tls_enabled_ = !ssl_cert_file.empty() && !ssl_key_file.empty();
  mtls_enabled_ = !ca_cert_file.empty();

#if CROSSDESK_SERVER_TLS
  if (tls_enabled_) {
    LOG_INFO("TLS enabled: cert={}, key={}", ssl_cert_file, ssl_key_file);
    if (mtls_enabled_) {
      LOG_INFO("mTLS enabled: CA={}", ca_cert_file);
    }
    server_.set_tls_init_handler(
        std::bind(&SignalServer::OnTlsInit, this, std::placeholders::_1));
  } else {
    LOG_WARN("TLS disabled — plain ws://");
  }
#else
  if (tls_enabled_ || mtls_enabled_) {
    LOG_WARN("TLS requested but this build was linked against non-TLS "
             "websocketpp. Recompile with TLS websocketpp (asio_tls.hpp) for "
             "wss:// support. Falling back to plain ws://");
    tls_enabled_ = false;
    mtls_enabled_ = false;
  }
#endif

  if (!server_password_.empty()) {
    LOG_INFO("Server password set ({} chars) — required when no client cert",
             server_password_.size());
  } else if (!mtls_enabled_) {
    LOG_WARN("NO AUTH — anyone can connect (no mTLS, no password)");
  }

  LOG_INFO("Signal server runs on port [{}] ({})", port,
           tls_enabled_ ? "wss://" : "ws://");

  if (http_port > 0) {
    http_running_ = true;
    http_thread_ = std::thread(&SignalServer::RunHttpApi, this, http_port);
    LOG_INFO("HTTP API on port [{}]", http_port);
  }

  server_.set_reuse_addr(true);

#if CROSSDESK_SERVER_TLS
  if (tls_enabled_) {
    try {
      server_.set_certificate_chain_file(ssl_cert_file);
      server_.set_private_key_file(ssl_key_file, asio::ssl::context::pem);
    } catch (const std::exception& e) {
      LOG_ERROR("Failed to load TLS cert/key: {}", e.what());
      return;
    }
  }
#endif

  server_.listen(port);
  server_.start_accept();
  server_.run();

  http_running_ = false;
  if (http_thread_.joinable()) {
    http_thread_.join();
  }
}

void SignalServer::send_msg(websocketpp::connection_hdl hdl, json message) {
  if (!hdl.expired()) {
    server_.send(hdl, message.dump(), websocketpp::frame::opcode::text);
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
        LOG_INFO("New client, assign id [{}]", host_id);
      }

      // === Auth chain: mTLS cert OR password ===
      bool authenticated = false;

      // 1) Check mTLS first (if enabled and client presented valid cert)
      if (mtls_enabled_) {
        std::lock_guard<std::mutex> lock(peer_cert_mutex_);
        auto it = peer_cert_verified_.find(hdl);
        if (it != peer_cert_verified_.end() && it->second) {
          authenticated = true;
          LOG_INFO("Login [{}] authenticated via mTLS client certificate",
                   host_id);
        }
      }

      // 2) Fallback to password
      if (!authenticated && !server_password_.empty()) {
        std::string client_pw = j.value("server_password", "");
        if (client_pw == server_password_) {
          authenticated = true;
          LOG_INFO("Login [{}] authenticated via password", host_id);
        }
      }

      // 3) If neither worked, reject
      if (!authenticated) {
        std::string reason;
        if (mtls_enabled_ && !server_password_.empty()) {
          reason = "mTLS cert OR server password required";
        } else if (mtls_enabled_) {
          reason = "Client certificate required";
        } else if (!server_password_.empty()) {
          reason = "Incorrect server password";
        } else {
          reason = "Authentication failed";
        }
        LOG_WARN("Login rejected [{}]: {}", host_id, reason);
        json message = {{"type", "login"},
                        {"user_id", host_id},
                        {"status", "fail"},
                        {"reason", reason}};
        send_msg(hdl, message);
        break;
      }

      // === Device registration ===
      std::string device_name = j.value("device_name", "");
      std::string platform = j.value("platform", "");
      std::string ip_address = j.value("ip", "");
      if (ip_address.empty()) {
        try {
          ip_address = server_.get_con_from_hdl(hdl)->get_remote_endpoint();
          auto colon = ip_address.rfind(':');
          if (colon != std::string::npos) ip_address = ip_address.substr(0, colon);
        } catch (...) {
          ip_address = "unknown";
        }
      }
      transmission_manager_.RegisterDevice(host_id, device_name, platform,
                                           ip_address, hdl);

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

      if (!transmission_manager_.IsTransmissionExist(transmission_id)) {
        if (transmission_id.empty()) {
          transmission_id = GenerateTransmissionId();
          while (transmission_manager_.IsTransmissionExist(transmission_id)) {
            transmission_id = GenerateTransmissionId();
          }
        }
        transmission_manager_.BindHostToTransmission(host_id, transmission_id);
        transmission_manager_.BindPasswordToTransmission(password,
                                                         transmission_id);

        json message = {{"type", "transmission_id"},
                        {"transmission_id", transmission_id},
                        {"status", "success"}};
        send_msg(hdl, message);
      } else {
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

      json message = {{"type", "user_leave_transmission"},
                      {"transmission_id", transmission_id},
                      {"user_id", user_id}};

      std::vector<std::string> user_id_list =
          transmission_manager_.GetAllUserIdOfTransmission(transmission_id);
      for (const auto& user : user_id_list) {
        if (user == user_id) continue;
        send_msg(transmission_manager_.GetWsHandle(user), message);
      }

      bool is_host =
          transmission_manager_.IsHostOfTransmission(user_id, transmission_id);
      if (is_host) {
        transmission_manager_.ReleaseTransmission(transmission_id);
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
      } else {
        json message = {{"type", "user_id_list"},
                        {"transmission_id", transmission_id},
                        {"user_id_list", std::vector<std::string>{}},
                        {"status", "failed"},
                        {"reason", (-1 == ret) ? "Incorrect password"
                                               : "No such transmission"}};
        send_msg(hdl, message);
      }
      break;
    }
    case "offer"_H:
    case "answer"_H:
    case "new_candidate"_H: {
      std::string transmission_id = j["transmission_id"].get<std::string>();
      std::string remote_user_id = j["remote_user_id"].get<std::string>();
      std::string user_id = j["user_id"].get<std::string>();

      if (type == "offer") {
        transmission_manager_.BindGuestToTransmission(user_id, transmission_id);
      }

      websocketpp::connection_hdl destination_hdl =
          transmission_manager_.GetWsHandle(remote_user_id);

      json forward = j;
      send_msg(destination_hdl, forward);
      break;
    }
    default:
      break;
  }
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
        if (http_running_) LOG_ERROR("HTTP accept error: {}", e.what());
        break;
      }

      try {
        char buf[4096];
        std::error_code ec;
        socket.read_some(asio::buffer(buf), ec);
        if (ec) continue;

        std::string request(buf);
        std::string response;

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
                       {"server_password_set", !server_password_.empty()},
                       {"mtls_enabled", mtls_enabled_},
                       {"tls_enabled", tls_enabled_}};
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
          response = "HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n";
        }

        asio::write(socket, asio::buffer(response));
      } catch (const std::exception& e) {
        LOG_ERROR("HTTP handler error: {}", e.what());
      }
    }
  } catch (const std::exception& e) {
    LOG_ERROR("HTTP API server fatal: {}", e.what());
  }
}
