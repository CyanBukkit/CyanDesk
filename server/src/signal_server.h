#ifndef _SIGNAL_SERVER_H_
#define _SIGNAL_SERVER_H_

#include <atomic>
#include <functional>
#include <map>
#include <mutex>
#include <nlohmann/json.hpp>
#include <set>
#include <string>
#include <thread>

// Conditional TLS support — upgrade websocketpp in thirdparty/ to get TLS.
#if __has_include(<websocketpp/config/asio_tls.hpp>)
  #include <websocketpp/config/asio_tls.hpp>
  #define CROSSDESK_SERVER_TLS 1
  #include <asio/ssl.hpp>
#else
  #include <websocketpp/config/asio_no_tls.hpp>
  #define CROSSDESK_SERVER_TLS 0
#endif
#include <websocketpp/server.hpp>

#include "client_id_generator.h"
#include "transmission_manager.h"

using nlohmann::json;

#if CROSSDESK_SERVER_TLS
typedef websocketpp::server<websocketpp::config::asio_tls> server;
typedef websocketpp::lib::shared_ptr<asio::ssl::context> context_ptr;
#else
typedef websocketpp::server<websocketpp::config::asio> server;
#endif
typedef unsigned int connection_id;
typedef std::string room_id;

class SignalServer {
 public:
  SignalServer();
  ~SignalServer();

  bool on_open(websocketpp::connection_hdl hdl);

  bool on_close(websocketpp::connection_hdl hdl);

  bool on_fail(websocketpp::connection_hdl hdl);

  bool on_ping(websocketpp::connection_hdl hdl, std::string s);

  bool on_pong(websocketpp::connection_hdl hdl, std::string s);

  void run(uint16_t port, uint16_t http_port, const std::string& server_password,
           const std::string& ssl_cert_file = "",
           const std::string& ssl_key_file = "",
           const std::string& ca_cert_file = "");

#if CROSSDESK_SERVER_TLS
  context_ptr OnTlsInit(websocketpp::connection_hdl hdl);
  bool OnTlsVerify(bool preverified, asio::ssl::verify_context& ctx);
#endif

  void on_message(websocketpp::connection_hdl hdl, server::message_ptr msg);

  void send_msg(websocketpp::connection_hdl hdl, json message);

  void RunHttpApi(uint16_t port);

 private:
  server server_;
  std::map<websocketpp::connection_hdl, connection_id,
           std::owner_less<websocketpp::connection_hdl>>
      ws_connections_;
  unsigned int ws_connection_id_ = 0;

  bool tls_enabled_ = false;
  bool mtls_enabled_ = false;
  std::string ca_cert_file_;

  // Track mTLS per-connection verification
  std::mutex peer_cert_mutex_;
  std::map<websocketpp::connection_hdl, bool,
           std::owner_less<websocketpp::connection_hdl>>
      peer_cert_verified_;

  std::string server_password_;
  std::thread http_thread_;
  std::atomic<bool> http_running_{false};

 private:
  TransmissionManager transmission_manager_;
  ClientIdGenerator client_id_generator_;
};

#endif
