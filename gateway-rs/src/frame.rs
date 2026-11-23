use std::io::{self, Read, Write};

pub const VERSION: u8 = 1;
pub const MAX_PAYLOAD: usize = 8 << 20;

pub const HELLO: u8 = 1;
pub const HELLO_ACK: u8 = 2;
pub const PEER_READY: u8 = 3;
pub const RELAY: u8 = 4;
pub const SIGNAL: u8 = 5;
pub const CLOSE: u8 = 6;

pub const ROLE_HOST: u8 = 1;
pub const ROLE_CONTROLLER: u8 = 2;

pub fn write_frame(mut w: impl Write, typ: u8, payload: &[u8]) -> io::Result<()> {
    if payload.len() > MAX_PAYLOAD {
        return Err(io::Error::new(io::ErrorKind::InvalidInput, "payload too large"));
    }
    let mut hdr = [0u8; 10];
    hdr[0] = b'C';
    hdr[1] = b'D';
    hdr[2] = VERSION;
    hdr[3] = typ;
    hdr[6..10].copy_from_slice(&(payload.len() as u32).to_be_bytes());
    w.write_all(&hdr)?;
    w.write_all(payload)?;
    Ok(())
}

pub fn read_frame(mut r: impl Read) -> io::Result<(u8, Vec<u8>)> {
    let mut hdr = [0u8; 10];
    r.read_exact(&mut hdr)?;
    if hdr[0] != b'C' || hdr[1] != b'D' {
        return Err(io::Error::new(io::ErrorKind::InvalidData, "bad magic"));
    }
    if hdr[2] != VERSION {
        return Err(io::Error::new(io::ErrorKind::InvalidData, "version"));
    }
    let n = u32::from_be_bytes(hdr[6..10].try_into().unwrap()) as usize;
    if n > MAX_PAYLOAD {
        return Err(io::Error::new(io::ErrorKind::InvalidData, "too large"));
    }
    let mut payload = vec![0u8; n];
    if n > 0 {
        r.read_exact(&mut payload)?;
    }
    Ok((hdr[3], payload))
}

pub fn encode_hello(role: u8, ticket: &str) -> Vec<u8> {
    let tb = ticket.as_bytes();
    let mut buf = vec![0u8; 5 + tb.len()];
    buf[0] = role;
    buf[1..5].copy_from_slice(&(tb.len() as u32).to_be_bytes());
    buf[5..].copy_from_slice(tb);
    buf
}

pub fn decode_hello(p: &[u8]) -> Option<(u8, String)> {
    if p.len() < 5 {
        return None;
    }
    let n = u32::from_be_bytes(p[1..5].try_into().unwrap()) as usize;
    if n != p.len() - 5 {
        return None;
    }
    let ticket = std::str::from_utf8(&p[5..]).ok()?.to_string();
    Some((p[0], ticket))
}

pub fn encode_ack(ok: bool, code: u16, message: &str) -> Vec<u8> {
    let mb = message.as_bytes();
    let mb = if mb.len() > 400 { &mb[..400] } else { mb };
    let mut buf = vec![0u8; 5 + mb.len()];
    buf[0] = if ok { 1 } else { 0 };
    buf[1..3].copy_from_slice(&code.to_be_bytes());
    buf[3..5].copy_from_slice(&(mb.len() as u16).to_be_bytes());
    buf[5..].copy_from_slice(mb);
    buf
}
