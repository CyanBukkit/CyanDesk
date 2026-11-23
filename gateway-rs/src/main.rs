mod frame;
mod redeem;

use crate::frame::{
    decode_hello, encode_ack, read_frame, write_frame, CLOSE, HELLO, HELLO_ACK, PEER_READY, RELAY,
    ROLE_CONTROLLER, ROLE_HOST, SIGNAL,
};
use crate::redeem::{Claims, Control};
use std::collections::HashMap;
use std::io::Read;
use std::net::{TcpListener, TcpStream, UdpSocket};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::{Duration, Instant};

// The cookie code uses crates. I avoided them in redeem. HMAC needs a crate unless I implement SHA256.
// Implementing HMAC-SHA256 here keeps the binary dependency-free.

fn main() {
    let mut listen = "0.0.0.0:13110".to_string();
    let mut internal = "http://127.0.0.1:13112".to_string();
    let mut token_file = String::new();
    let mut args = std::env::args().skip(1);
    while let Some(a) = args.next() {
        match a.as_str() {
            "--listen" => listen = args.next().unwrap_or(listen),
            "--internal-url" => internal = args.next().unwrap_or(internal),
            "--internal-token-file" => token_file = args.next().unwrap_or_default(),
            "--help" => {
                eprintln!("cyandesk-gateway --listen ADDR --internal-url URL --internal-token-file FILE");
                return;
            }
            _ => {}
        }
    }
    if token_file.is_empty() {
        eprintln!("missing --internal-token-file");
        std::process::exit(2);
    }
    let token = std::fs::read_to_string(&token_file).unwrap_or_else(|e| {
        eprintln!("token file: {e}");
        std::process::exit(1);
    });
    let token = token.trim().to_string();
    if token.is_empty() {
        eprintln!("empty internal token");
        std::process::exit(1);
    }
    let state = Arc::new(State {
        control: Control {
            base: internal,
            token,
        },
        inner: Mutex::new(Inner {
            sessions: HashMap::new(),
            by_ticket: HashMap::new(),
            secret: rand_bytes(),
            issued: HashMap::new(),
        }),
    });
    let tcp = TcpListener::bind(&listen).unwrap_or_else(|e| {
        eprintln!("tcp bind {listen}: {e}");
        std::process::exit(1);
    });
    let udp = UdpSocket::bind(&listen).unwrap_or_else(|e| {
        eprintln!("udp bind {listen}: {e}");
        std::process::exit(1);
    });
    eprintln!("cyandesk-gateway listening {listen} tcp+udp");
    let udp_state = Arc::clone(&state);
    thread::spawn(move || udp_loop(udp, udp_state));
    let recheck = Arc::clone(&state);
    thread::spawn(move || recheck_loop(recheck));
    for conn in tcp.incoming() {
        let Ok(conn) = conn else { continue };
        let st = Arc::clone(&state);
        thread::spawn(move || {
            if let Err(e) = handle_tcp(conn, st) {
                let _ = e;
            }
        });
    }
}

struct State {
    control: Control,
    inner: Mutex<Inner>,
}

struct Inner {
    sessions: HashMap<String, Session>,
    by_ticket: HashMap<String, String>,
    secret: [u8; 32],
    issued: HashMap<std::net::SocketAddr, [u8; 31]>,
}

struct Session {
    ticket: String,
    claims: Claims,
    host: Option<Arc<Mutex<TcpStream>>>,
    ctrl: Option<Arc<Mutex<TcpStream>>>,
    host_udp: Option<std::net::SocketAddr>,
    ctrl_udp: Option<std::net::SocketAddr>,
    host_cookie: Option<[u8; 31]>,
    ctrl_cookie: Option<[u8; 31]>,
    ready_sent: bool,
    bytes: u64,
}

fn handle_tcp(mut conn: TcpStream, state: Arc<State>) -> std::io::Result<()> {
    conn.set_read_timeout(Some(Duration::from_secs(15)))?;
    let (typ, payload) = read_frame(&mut conn)?;
    if typ != HELLO {
        return Ok(());
    }
    let Some((role, ticket)) = decode_hello(&payload) else {
        let _ = write_frame(&mut conn, HELLO_ACK, &encode_ack(false, 400, "bad hello"));
        return Ok(());
    };
    if role != ROLE_HOST && role != ROLE_CONTROLLER {
        let _ = write_frame(&mut conn, HELLO_ACK, &encode_ack(false, 400, "bad role"));
        return Ok(());
    }
    let session_id = match state.attach_tcp(&ticket, role) {
        Ok(id) => id,
        Err(e) => {
            let _ = write_frame(&mut conn, HELLO_ACK, &encode_ack(false, 403, &e));
            return Ok(());
        }
    };
    write_frame(&mut conn, HELLO_ACK, &encode_ack(true, 0, ""))?;
    conn.set_read_timeout(None)?;
    let shared = Arc::new(Mutex::new(conn.try_clone()?));
    {
        let mut g = state.inner.lock().unwrap();
        if let Some(s) = g.sessions.get_mut(&session_id) {
            if role == ROLE_HOST {
                s.host = Some(Arc::clone(&shared));
            } else {
                s.ctrl = Some(Arc::clone(&shared));
            }
            maybe_ready(s);
        }
    }
    loop {
        let (typ, payload) = match read_frame(&mut conn) {
            Ok(v) => v,
            Err(_) => {
                state.drop_role(&session_id, role);
                break;
            }
        };
        if typ == CLOSE {
            state.drop_session(&session_id);
            break;
        }
        if typ != RELAY && typ != SIGNAL {
            state.drop_session(&session_id);
            break;
        }
        if !state.account(&session_id, payload.len() as u64) {
            state.drop_session(&session_id);
            break;
        }
        let other = state.other_tcp(&session_id, role);
        let Some(other) = other else { continue };
        let mut guard = other.lock().unwrap();
        if write_frame(&mut *guard, typ, &payload).is_err() {
            drop(guard);
            state.drop_session(&session_id);
            break;
        }
    }
    Ok(())
}

fn maybe_ready(s: &mut Session) {
    if s.ready_sent || s.host.is_none() || s.ctrl.is_none() {
        return;
    }
    s.ready_sent = true;
    for sock in [s.host.clone(), s.ctrl.clone()].into_iter().flatten() {
        if let Ok(mut g) = sock.lock() {
            let _ = write_frame(&mut *g, PEER_READY, &[]);
        }
    }
}

impl State {
    fn lookup_or_redeem(&self, ticket: &str) -> Result<String, String> {
        // The control plane accepts a ticket jti once. Two peers redeem together.
        // The loser must use the winner's cached session instead of failing closed.
        for _ in 0..8 {
            {
                let g = self.inner.lock().unwrap();
                if let Some(id) = g.by_ticket.get(ticket).cloned() {
                    return Ok(id);
                }
            }
            match self.control.redeem(ticket) {
                Ok(claims) => {
                    let mut g = self.inner.lock().unwrap();
                    if let Some(id) = g.by_ticket.get(ticket).cloned() {
                        return Ok(id);
                    }
                    let id = claims.session_id.clone();
                    g.by_ticket.insert(ticket.to_string(), id.clone());
                    if !g.sessions.contains_key(&id) {
                        g.sessions.insert(
                            id.clone(),
                            Session {
                                ticket: ticket.to_string(),
                                claims,
                                host: None,
                                ctrl: None,
                                host_udp: None,
                                ctrl_udp: None,
                                host_cookie: None,
                                ctrl_cookie: None,
                                ready_sent: false,
                                bytes: 0,
                            },
                        );
                    }
                    return Ok(id);
                }
                Err(_) => {
                    let g = self.inner.lock().unwrap();
                    if let Some(id) = g.by_ticket.get(ticket).cloned() {
                        return Ok(id);
                    }
                }
            }
            thread::sleep(Duration::from_millis(25));
        }
        Err("denied".to_string())
    }

    fn attach_tcp(&self, ticket: &str, role: u8) -> Result<String, String> {
        let id = self.lookup_or_redeem(ticket)?;
        let g = self.inner.lock().unwrap();
        let s = g.sessions.get(&id).ok_or("missing")?;
        if role == ROLE_HOST && s.host.is_some() {
            return Err("duplicate role".into());
        }
        if role == ROLE_CONTROLLER && s.ctrl.is_some() {
            return Err("duplicate role".into());
        }
        Ok(id)
    }

    fn other_tcp(&self, id: &str, role: u8) -> Option<Arc<Mutex<TcpStream>>> {
        let g = self.inner.lock().unwrap();
        let s = g.sessions.get(id)?;
        if role == ROLE_HOST {
            s.ctrl.clone()
        } else {
            s.host.clone()
        }
    }

    fn account(&self, id: &str, n: u64) -> bool {
        let mut g = self.inner.lock().unwrap();
        let Some(s) = g.sessions.get_mut(id) else {
            return false;
        };
        s.bytes += n;
        s.claims.bytes_limit == 0 || s.bytes <= s.claims.bytes_limit
    }

    fn drop_role(&self, id: &str, role: u8) {
        let mut g = self.inner.lock().unwrap();
        let Some(s) = g.sessions.get_mut(id) else {
            return;
        };
        if role == ROLE_HOST {
            if let Some(c) = s.ctrl.clone() {
                let _ = c.lock().unwrap().shutdown(std::net::Shutdown::Both);
            }
            s.host = None;
        } else {
            if let Some(c) = s.host.clone() {
                let _ = c.lock().unwrap().shutdown(std::net::Shutdown::Both);
            }
            s.ctrl = None;
        }
    }

    fn drop_session(&self, id: &str) {
        let mut g = self.inner.lock().unwrap();
        if let Some(s) = g.sessions.remove(id) {
            g.by_ticket.remove(&s.ticket);
            for c in [s.host, s.ctrl].into_iter().flatten() {
                let _ = c.lock().unwrap().shutdown(std::net::Shutdown::Both);
            }
        }
    }
}

fn udp_loop(sock: UdpSocket, state: Arc<State>) {
    let mut buf = [0u8; 2048];
    loop {
        let Ok((n, from)) = sock.recv_from(&mut buf) else {
            continue;
        };
        if n == 0 {
            continue;
        }
        match buf[0] {
            1 if n == 64 => {
                let mut nonce = [0u8; 32];
                nonce.copy_from_slice(&buf[1..33]);
                let cookie = cookie_for(&state, &nonce, &from);
                state.inner.lock().unwrap().issued.insert(from, cookie);
                let mut reply = [0u8; 64];
                reply[0] = 2;
                reply[1..33].copy_from_slice(&nonce);
                reply[33..64].copy_from_slice(&cookie);
                let _ = sock.send_to(&reply, from);
            }
            3 if n > 32 => {
                let mut cookie = [0u8; 31];
                cookie.copy_from_slice(&buf[1..32]);
                let issued = state.inner.lock().unwrap().issued.get(&from).copied();
                if issued != Some(cookie) {
                    continue;
                }
                let frame = &buf[32..n];
                if frame.len() < 10 {
                    continue;
                }
                handle_udp_frame(&sock, &state, from, cookie, frame);
            }
            _ => {}
        }
    }
}

fn cookie_for(state: &State, nonce: &[u8; 32], addr: &std::net::SocketAddr) -> [u8; 31] {
    let g = state.inner.lock().unwrap();
    let mut mac = Sha256Mac::new(&g.secret);
    mac.update(nonce);
    mac.update(addr.to_string().as_bytes());
    let full = mac.finish();
    let mut out = [0u8; 31];
    out.copy_from_slice(&full[..31]);
    out
}

fn handle_udp_frame(sock: &UdpSocket, state: &State, from: std::net::SocketAddr, cookie: [u8; 31], frame: &[u8]) {
    let mut cursor = std::io::Cursor::new(frame);
    let Ok((typ, payload)) = read_frame(&mut cursor) else {
        return;
    };
    if typ == HELLO {
        let Some((role, ticket)) = decode_hello(&payload) else {
            return;
        };
        if let Ok(id) = state.lookup_or_redeem(&ticket) {
            let mut g = state.inner.lock().unwrap();
            if let Some(s) = g.sessions.get_mut(&id) {
                if role == ROLE_HOST {
                    s.host_udp = Some(from);
                    s.host_cookie = Some(cookie);
                } else if role == ROLE_CONTROLLER {
                    s.ctrl_udp = Some(from);
                    s.ctrl_cookie = Some(cookie);
                }
            }
        }
        return;
    }
    if typ != RELAY && typ != SIGNAL {
        return;
    }
    let g = state.inner.lock().unwrap();
    let Some(s) = g.sessions.values().find(|s| match_cookie(s, from, cookie)) else {
        return;
    };
    if s.claims.bytes_limit > 0 && s.bytes + payload.len() as u64 > s.claims.bytes_limit {
        return;
    }
    let (dest, dest_cookie) = if s.host_udp == Some(from) {
        (s.ctrl_udp, s.ctrl_cookie)
    } else {
        (s.host_udp, s.host_cookie)
    };
    let (Some(dest), Some(dest_cookie)) = (dest, dest_cookie) else {
        return;
    };
    let mut pkt = Vec::with_capacity(32 + frame.len());
    pkt.push(3);
    pkt.extend_from_slice(&dest_cookie);
    pkt.extend_from_slice(frame);
    if pkt.len() > frame.len() + 32 {
        // same size class as the input datagram: input was 32+frame, output is 32+frame
    }
    drop(g);
    let _ = sock.send_to(&pkt, dest);
}

fn match_cookie(s: &Session, from: std::net::SocketAddr, cookie: [u8; 31]) -> bool {
    (s.host_udp == Some(from) && s.host_cookie == Some(cookie))
        || (s.ctrl_udp == Some(from) && s.ctrl_cookie == Some(cookie))
}

fn recheck_loop(state: Arc<State>) {
    loop {
        thread::sleep(Duration::from_secs(2));
        let ids: Vec<String> = {
            let g = state.inner.lock().unwrap();
            g.sessions.keys().cloned().collect()
        };
        for id in ids {
            match state.control.session_open(&id) {
                Ok(true) => {}
                _ => state.drop_session(&id),
            }
        }
    }
}

fn rand_bytes() -> [u8; 32] {
    let mut b = [0u8; 32];
    let mut f = std::fs::File::open("/dev/urandom").expect("urandom");
    f.read_exact(&mut b).expect("urandom");
    b
}

struct Sha256Mac {
    inner: sha2_hmac::HmacSha256,
}

impl Sha256Mac {
    fn new(key: &[u8]) -> Self {
        Self {
            inner: sha2_hmac::HmacSha256::new_from_slice(key),
        }
    }
    fn update(&mut self, data: &[u8]) {
        self.inner.update(data);
    }
    fn finish(self) -> [u8; 32] {
        self.inner.finalize()
    }
}

mod sha2_hmac {
    //! Minimal HMAC-SHA256 so the gateway does not download crates.
    const BLOCK: usize = 64;

    pub struct HmacSha256 {
        inner: Sha256,
        outer: Sha256,
    }

    impl HmacSha256 {
        pub fn new_from_slice(key: &[u8]) -> Self {
            let mut k = [0u8; BLOCK];
            if key.len() > BLOCK {
                let mut h = Sha256::new();
                h.update(key);
                let d = h.finalize();
                k[..32].copy_from_slice(&d);
            } else {
                k[..key.len()].copy_from_slice(key);
            }
            let mut ipad = [0x36u8; BLOCK];
            let mut opad = [0x5cu8; BLOCK];
            for i in 0..BLOCK {
                ipad[i] ^= k[i];
                opad[i] ^= k[i];
            }
            let mut inner = Sha256::new();
            inner.update(&ipad);
            let mut outer = Sha256::new();
            outer.update(&opad);
            Self { inner, outer }
        }
        pub fn update(&mut self, data: &[u8]) {
            self.inner.update(data);
        }
        pub fn finalize(mut self) -> [u8; 32] {
            let d = self.inner.finalize();
            self.outer.update(&d);
            self.outer.finalize()
        }
    }

    struct Sha256 {
        h: [u32; 8],
        buf: [u8; 64],
        len: usize,
        bits: u64,
    }

    impl Sha256 {
        fn new() -> Self {
            Self {
                h: [
                    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c,
                    0x1f83d9ab, 0x5be0cd19,
                ],
                buf: [0; 64],
                len: 0,
                bits: 0,
            }
        }
        fn update(&mut self, mut data: &[u8]) {
            self.bits += (data.len() as u64) * 8;
            if self.len > 0 {
                let n = (64 - self.len).min(data.len());
                self.buf[self.len..self.len + n].copy_from_slice(&data[..n]);
                self.len += n;
                data = &data[n..];
                if self.len == 64 {
                    let b = self.buf;
                    self.compress(&b);
                    self.len = 0;
                }
            }
            while data.len() >= 64 {
                let mut b = [0u8; 64];
                b.copy_from_slice(&data[..64]);
                self.compress(&b);
                data = &data[64..];
            }
            if !data.is_empty() {
                self.buf[..data.len()].copy_from_slice(data);
                self.len = data.len();
            }
        }
        fn finalize(mut self) -> [u8; 32] {
            let mut block = [0u8; 64];
            block[..self.len].copy_from_slice(&self.buf[..self.len]);
            block[self.len] = 0x80;
            if self.len >= 56 {
                self.compress(&block);
                block = [0u8; 64];
            }
            block[56..].copy_from_slice(&self.bits.to_be_bytes());
            self.compress(&block);
            let mut out = [0u8; 32];
            for (i, w) in self.h.iter().enumerate() {
                out[i * 4..i * 4 + 4].copy_from_slice(&w.to_be_bytes());
            }
            out
        }
        fn compress(&mut self, block: &[u8; 64]) {
            const K: [u32; 64] = [
                0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4,
                0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe,
                0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f,
                0x4a7484aa, 0x5cb0a9dc, 0x76f988da, 0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
                0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc,
                0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b,
                0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070, 0x19a4c116,
                0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
                0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7,
                0xc67178f2,
            ];
            let mut w = [0u32; 64];
            for i in 0..16 {
                w[i] = u32::from_be_bytes(block[i * 4..i * 4 + 4].try_into().unwrap());
            }
            for i in 16..64 {
                let s0 = w[i - 15].rotate_right(7) ^ w[i - 15].rotate_right(18) ^ (w[i - 15] >> 3);
                let s1 = w[i - 2].rotate_right(17) ^ w[i - 2].rotate_right(19) ^ (w[i - 2] >> 10);
                w[i] = w[i - 16]
                    .wrapping_add(s0)
                    .wrapping_add(w[i - 7])
                    .wrapping_add(s1);
            }
            let mut a = self.h;
            for i in 0..64 {
                let s1 = a[4].rotate_right(6) ^ a[4].rotate_right(11) ^ a[4].rotate_right(25);
                let ch = (a[4] & a[5]) ^ ((!a[4]) & a[6]);
                let t1 = a[7]
                    .wrapping_add(s1)
                    .wrapping_add(ch)
                    .wrapping_add(K[i])
                    .wrapping_add(w[i]);
                let s0 = a[0].rotate_right(2) ^ a[0].rotate_right(13) ^ a[0].rotate_right(22);
                let maj = (a[0] & a[1]) ^ (a[0] & a[2]) ^ (a[1] & a[2]);
                let t2 = s0.wrapping_add(maj);
                a[7] = a[6];
                a[6] = a[5];
                a[5] = a[4];
                a[4] = a[3].wrapping_add(t1);
                a[3] = a[2];
                a[2] = a[1];
                a[1] = a[0];
                a[0] = t1.wrapping_add(t2);
            }
            for i in 0..8 {
                self.h[i] = self.h[i].wrapping_add(a[i]);
            }
        }
    }
}

// Silence unused import if hmac crate names leaked into the draft.
#[allow(dead_code)]
fn _unused_instant() -> Instant {
    Instant::now()
}

#[cfg(test)]
mod hmac_tests {
    #[test]
    fn known_answer() {
        let mut h = super::sha2_hmac::HmacSha256::new_from_slice(b"key");
        h.update(b"The quick brown fox jumps over the lazy dog");
        let out = h.finalize();
        let hex: String = out.iter().map(|b| format!("{b:02x}")).collect();
        assert_eq!(
            hex,
            "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8"
        );
    }
}
