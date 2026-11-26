#include <cstdlib>
#include <iostream>
#include <string>

#include "signal_server.h"

int main(int argc, char* argv[]) {
  SignalServer s;

  // === WebSocket signal port ===
  uint16_t port = 9090;
  if (const char* env_port = std::getenv("SIGNAL_SERVER_PORT")) {
    port = static_cast<uint16_t>(std::atoi(env_port));
  } else if (argc > 1) {
    port = static_cast<uint16_t>(std::atoi(argv[1]));
  }

  // === HTTP API port (device listing JSON), 0 = disabled ===
  uint16_t http_port = 9091;
  if (const char* env = std::getenv("HTTP_API_PORT")) {
    http_port = static_cast<uint16_t>(std::atoi(env));
  }

  // === Server password (empty = no auth, unless mTLS required) ===
  std::string server_password;
  if (const char* env = std::getenv("SERVER_PASSWORD")) {
    server_password = env;
  }

  // === TLS server certificate (PEM) + private key ===
  std::string ssl_cert;
  if (const char* env = std::getenv("SSL_CERT_FILE")) ssl_cert = env;
  std::string ssl_key;
  if (const char* env = std::getenv("SSL_KEY_FILE")) ssl_key = env;

  // === mTLS CA file ===
  // If set, clients MUST present a cert signed by this CA.
  // If they do, server password is NOT required.
  // If they don't present a cert, server password IS required (if set).
  std::string ca_cert;
  if (const char* env = std::getenv("CA_CERT_FILE")) ca_cert = env;

  s.run(port, http_port, server_password, ssl_cert, ssl_key, ca_cert);
  return 0;
}
