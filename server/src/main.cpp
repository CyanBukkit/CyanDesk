#include <cstdlib>
#include <iostream>
#include <string>

#include "signal_server.h"

int main(int argc, char* argv[]) {
  SignalServer s;

  // WebSocket port (default 9090)
  uint16_t port = 9090;
  const char* env_port = std::getenv("SIGNAL_SERVER_PORT");
  if (env_port) {
    port = static_cast<uint16_t>(std::atoi(env_port));
  } else if (argc > 1) {
    port = static_cast<uint16_t>(std::atoi(argv[1]));
  }

  // HTTP API port for device listing (default 9091, 0 = disabled)
  uint16_t http_port = 9091;
  const char* env_http_port = std::getenv("HTTP_API_PORT");
  if (env_http_port) {
    http_port = static_cast<uint16_t>(std::atoi(env_http_port));
  }

  // Server-wide password (empty = no auth required)
  std::string server_password;
  const char* env_pw = std::getenv("SERVER_PASSWORD");
  if (env_pw) {
    server_password = env_pw;
  }

  s.run(port, http_port, server_password);
  return 0;
}
