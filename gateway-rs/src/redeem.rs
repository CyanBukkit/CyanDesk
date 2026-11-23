use std::io::{Read, Write};
use std::net::TcpStream;
use std::time::Duration;

#[derive(Clone, Debug)]
pub struct Claims {
    pub session_id: String,
    pub bytes_limit: u64,
}

pub struct Control {
    pub base: String,
    pub token: String,
}

impl Control {
    pub fn redeem(&self, ticket: &str) -> Result<Claims, String> {
        let path = "/api/v1/internal/tickets/redeem";
        let body = format!("{{\"ticket\":{}}}", json_string(ticket));
        let raw = self.roundtrip("POST", path, Some(body.as_bytes()))?;
        if !raw.status_ok() {
            return Err(format!("redeem {}", raw.status));
        }
        Ok(Claims {
            session_id: json_string_field(&raw.body, "session_id").ok_or("missing session")?,
            bytes_limit: json_u64_field(&raw.body, "bytes_limit").unwrap_or(0),
        })
    }

    pub fn session_open(&self, id: &str) -> Result<bool, String> {
        let path = format!("/api/v1/internal/sessions/{id}");
        let raw = self.roundtrip("GET", &path, None)?;
        if !raw.status_ok() {
            return Ok(false);
        }
        Ok(json_string_field(&raw.body, "state").as_deref() == Some("active"))
    }

    fn roundtrip(&self, method: &str, path: &str, body: Option<&[u8]>) -> Result<RawResp, String> {
        let rest = self.base.trim_start_matches("http://");
        let (host, prefix) = split_host(rest);
        let mut stream = TcpStream::connect_timeout(
            &host.parse().map_err(|e| format!("control addr {e}"))?,
            Duration::from_secs(3),
        )
        .map_err(|e| e.to_string())?;
        stream.set_read_timeout(Some(Duration::from_secs(3))).ok();
        stream.set_write_timeout(Some(Duration::from_secs(3))).ok();
        let b = body.unwrap_or(b"");
        let req = format!(
            "{method} {prefix}{path} HTTP/1.1\r\nHost: {host}\r\nAuthorization: Bearer {token}\r\nContent-Type: application/json\r\nContent-Length: {len}\r\nConnection: close\r\n\r\n",
            token = self.token,
            len = b.len(),
        );
        stream.write_all(req.as_bytes()).map_err(|e| e.to_string())?;
        if !b.is_empty() {
            stream.write_all(b).map_err(|e| e.to_string())?;
        }
        let mut buf = Vec::new();
        stream.read_to_end(&mut buf).map_err(|e| e.to_string())?;
        parse_http(&buf)
    }
}

struct RawResp {
    status: u16,
    body: String,
}

impl RawResp {
    fn status_ok(&self) -> bool {
        (200..300).contains(&self.status)
    }
}

fn split_host(rest: &str) -> (String, String) {
    if let Some(i) = rest.find('/') {
        (rest[..i].to_string(), String::new())
    } else {
        (rest.to_string(), String::new())
    }
}

fn parse_http(buf: &[u8]) -> Result<RawResp, String> {
    let text = String::from_utf8_lossy(buf);
    let mut lines = text.split("\r\n");
    let status_line = lines.next().unwrap_or("");
    let mut parts = status_line.split_whitespace();
    parts.next();
    let status: u16 = parts.next().unwrap_or("0").parse().unwrap_or(0);
    let body = if let Some(i) = text.find("\r\n\r\n") {
        text[i + 4..].to_string()
    } else {
        String::new()
    };
    Ok(RawResp { status, body })
}

fn json_string(s: &str) -> String {
    let mut out = String::from("\"");
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            c if c.is_control() => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push(c),
        }
    }
    out.push('"');
    out
}

fn json_string_field(body: &str, key: &str) -> Option<String> {
    let pat = format!("\"{key}\"");
    let i = body.find(&pat)?;
    let rest = &body[i + pat.len()..];
    let rest = rest.trim_start().trim_start_matches(':').trim_start();
    if !rest.starts_with('"') {
        return None;
    }
    let mut out = String::new();
    let mut chars = rest[1..].chars();
    while let Some(c) = chars.next() {
        if c == '\\' {
            if let Some(n) = chars.next() {
                out.push(n);
            }
        } else if c == '"' {
            break;
        } else {
            out.push(c);
        }
    }
    Some(out)
}

fn json_u64_field(body: &str, key: &str) -> Option<u64> {
    let pat = format!("\"{key}\"");
    let i = body.find(&pat)?;
    let rest = &body[i + pat.len()..];
    let rest = rest.trim_start().trim_start_matches(':').trim_start();
    let num: String = rest.chars().take_while(|c| c.is_ascii_digit()).collect();
    num.parse().ok()
}
