//! Framing taken from ALVR commit 2945d90e6285c4794a7ce89b07d85ca8e5d52185.
//! Control sockets prefix a little-endian u32 payload length (`alvr/sockets/src/control_socket.rs`).
//! UDP shards start with stream id, packet index, shard count, and shard index
//! (`SHARD_PREFIX_SIZE` in `alvr/sockets/src/stream_socket/udp.rs`).
//! Stream numbers are `TRACKING` through `STATISTICS` in `alvr/packets/src/lib.rs`.
//! ALVR is MIT. This crate does not vendor or launch ALVR, and it is not a SteamVR driver.

pub const CONTROL_PORT: u16 = 9943;
pub const SHARD_PREFIX_SIZE: usize = 2 + 4 + 4 + 4;

pub const TRACKING: u16 = 0;
pub const HAPTICS: u16 = 1;
pub const AUDIO: u16 = 2;
pub const VIDEO: u16 = 3;
pub const STATISTICS: u16 = 4;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct ShardHeader {
    pub stream_id: u16,
    pub packet_index: u32,
    pub shard_count: u32,
    pub shard_index: u32,
}

#[derive(Debug, PartialEq, Eq)]
pub enum BridgeError {
    Short,
    NotVr,
    DesktopAnnexB,
}

/// Length prefix used by ALVR `framed_send`: the u32 counts only the payload.
pub fn encode_control(payload: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(4 + payload.len());
    out.extend_from_slice(&(payload.len() as u32).to_le_bytes());
    out.extend_from_slice(payload);
    out
}

pub fn decode_control(frame: &[u8]) -> Result<&[u8], BridgeError> {
    if frame.len() < 4 {
        return Err(BridgeError::Short);
    }
    let n = u32::from_le_bytes(frame[0..4].try_into().unwrap()) as usize;
    if frame.len() != 4 + n {
        return Err(BridgeError::Short);
    }
    Ok(&frame[4..])
}

pub fn encode_shard(header: ShardHeader, payload: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(SHARD_PREFIX_SIZE + payload.len());
    out.extend_from_slice(&header.stream_id.to_le_bytes());
    out.extend_from_slice(&header.packet_index.to_le_bytes());
    out.extend_from_slice(&header.shard_count.to_le_bytes());
    out.extend_from_slice(&header.shard_index.to_le_bytes());
    out.extend_from_slice(payload);
    out
}

pub fn decode_shard(packet: &[u8]) -> Result<(ShardHeader, &[u8]), BridgeError> {
    if packet.len() < SHARD_PREFIX_SIZE {
        return Err(BridgeError::Short);
    }
    let header = ShardHeader {
        stream_id: u16::from_le_bytes(packet[0..2].try_into().unwrap()),
        packet_index: u32::from_le_bytes(packet[2..6].try_into().unwrap()),
        shard_count: u32::from_le_bytes(packet[6..10].try_into().unwrap()),
        shard_index: u32::from_le_bytes(packet[10..14].try_into().unwrap()),
    };
    Ok((header, &packet[SHARD_PREFIX_SIZE..]))
}

/// A desktop annex-B access unit is not an ALVR video shard.
pub fn reject_desktop_video(bytes: &[u8]) -> Result<(), BridgeError> {
    if bytes.starts_with(&[0, 0, 0, 1]) || bytes.starts_with(&[0, 0, 1]) {
        return Err(BridgeError::DesktopAnnexB);
    }
    if decode_shard(bytes).is_err() {
        return Err(BridgeError::NotVr);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn control_length_matches_alvr_prefix() {
        let frame = encode_control(b"pose");
        assert_eq!(&frame[..4], &4u32.to_le_bytes());
        assert_eq!(decode_control(&frame).unwrap(), b"pose");
        assert!(decode_control(&frame[..frame.len() - 1]).is_err());
    }

    #[test]
    fn shard_header_is_14_bytes_and_video_id_is_3() {
        assert_eq!(SHARD_PREFIX_SIZE, 14);
        let pkt = encode_shard(
            ShardHeader {
                stream_id: VIDEO,
                packet_index: 7,
                shard_count: 2,
                shard_index: 1,
            },
            &[9, 9],
        );
        let (header, payload) = decode_shard(&pkt).unwrap();
        assert_eq!(header.stream_id, 3);
        assert_eq!(header.packet_index, 7);
        assert_eq!(payload, &[9, 9]);
    }

    #[test]
    fn desktop_h264_is_not_a_vr_packet() {
        assert_eq!(
            reject_desktop_video(&[0, 0, 0, 1, 0x65]),
            Err(BridgeError::DesktopAnnexB)
        );
    }
}
