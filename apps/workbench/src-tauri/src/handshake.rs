//! Bounded parser for the Go sidecar startup handshake.
//!
//! The Go backend prints exactly one line on startup
//! (`backend/internal/api/router.go`):
//!
//! ```text
//! CODEFLOW_HANDSHAKE:{"protocol_version":"1","host":"127.0.0.1","port":49200,"token":"...","expires_at":"process","process_start_id":"..."}
//! ```
//!
//! This module turns arbitrary stdout byte chunks into validated handshake
//! events:
//!
//! - lines are reassembled across arbitrary chunk boundaries (split-safe);
//! - several lines delivered in one chunk are each classified (merge-safe);
//! - a line is bounded to [`MAX_LINE_BYTES`]; a longer line is rejected once
//!   and its remainder dropped, so a hostile or buggy producer cannot grow
//!   this buffer without limit;
//! - non-handshake stdout lines pass through as [`LineOutcome::Other`] so the
//!   caller can log them; a handshake line itself must never be logged,
//!   because it carries the process token.
//!
//! The module is pure: it never prints and never touches process state.

/// Wire prefix emitted by the Go sidecar before the JSON payload.
pub const HANDSHAKE_PREFIX: &str = "CODEFLOW_HANDSHAKE:";

/// Maximum accepted handshake/log line size in bytes. A real handshake line
/// is ~250 bytes; 4 KiB leaves generous headroom while staying bounded.
pub const MAX_LINE_BYTES: usize = 4096;

/// Minimum accepted token length in bytes, matching the Go producer
/// validation (`validOpaqueToken` requires >= 32 URL-safe characters).
pub const MIN_TOKEN_BYTES: usize = 32;

/// Only handshake protocol version this consumer understands. Anything else
/// is rejected as `UnknownProtocolVersion` (protocol mismatch).
pub const SUPPORTED_PROTOCOL_VERSION: &str = "1";

/// Marker appended to an over-long ordinary stdout line that was truncated.
pub const OVERLONG_SUFFIX: &str = " …[truncated]";

/// Loopback host whitelist. The sidecar must never advertise anything else.
/// `::1` is included because the Go producer accepts any loopback bind via
/// `CODEFLOW_HOST` (`isLoopbackHost` covers IPv6 loopback) and the sidecar
/// inherits that environment.
pub const ALLOWED_HOSTS: [&str; 3] = ["127.0.0.1", "localhost", "::1"];

/// A validated sidecar handshake.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Handshake {
    pub protocol_version: String,
    pub host: String,
    pub port: u16,
    pub token: String,
    pub expires_at: String,
    pub process_start_id: String,
}

/// Why a handshake candidate line was rejected.
///
/// Variants never carry token material, so errors are safe to log.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum HandshakeError {
    /// Line exceeded [`MAX_LINE_BYTES`] before a newline arrived.
    LineTooLong,
    /// Handshake candidate was not valid UTF-8.
    InvalidUtf8,
    /// Payload after the prefix was not a JSON object.
    InvalidJson,
    /// Required field absent or null.
    MissingField(&'static str),
    /// Field present with the wrong JSON type.
    InvalidFieldType(&'static str),
    /// protocol_version is not one this consumer supports.
    UnknownProtocolVersion(String),
    /// host is outside the loopback whitelist.
    HostNotAllowed(String),
    /// port outside 1..=65535.
    PortOutOfRange(i64),
    /// token shorter than [`MIN_TOKEN_BYTES`].
    TokenTooShort { actual: usize },
    /// token contains characters outside [A-Za-z0-9_-].
    TokenInvalidCharset,
    /// expires_at present but empty.
    EmptyExpiresAt,
    /// process_start_id present but empty.
    EmptyProcessStartId,
}

impl std::fmt::Display for HandshakeError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            HandshakeError::LineTooLong => write!(f, "handshake line exceeds {MAX_LINE_BYTES} bytes"),
            HandshakeError::InvalidUtf8 => write!(f, "handshake line is not valid UTF-8"),
            HandshakeError::InvalidJson => write!(f, "handshake payload is not valid JSON"),
            HandshakeError::MissingField(field) => write!(f, "handshake missing field {field}"),
            HandshakeError::InvalidFieldType(field) => {
                write!(f, "handshake field {field} has wrong type")
            }
            HandshakeError::UnknownProtocolVersion(v) => {
                write!(f, "unsupported handshake protocol_version {v:?}")
            }
            HandshakeError::HostNotAllowed(host) => {
                write!(f, "handshake host {host:?} is not loopback")
            }
            HandshakeError::PortOutOfRange(port) => write!(f, "handshake port {port} out of range"),
            HandshakeError::TokenTooShort { actual } => {
                write!(f, "handshake token too short ({actual} bytes)")
            }
            HandshakeError::TokenInvalidCharset => {
                write!(f, "handshake token has non-URL-safe characters")
            }
            HandshakeError::EmptyExpiresAt => write!(f, "handshake expires_at is empty"),
            HandshakeError::EmptyProcessStartId => {
                write!(f, "handshake process_start_id is empty")
            }
        }
    }
}

impl std::error::Error for HandshakeError {}

/// Outcome for one complete stdout line.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum LineOutcome {
    /// The line carried the handshake prefix; payload validated or rejected.
    Handshake(Result<Handshake, HandshakeError>),
    /// Ordinary stdout line, passed through for the caller to handle.
    Other(String),
}

/// Incremental, bounded line parser for sidecar stdout.
#[derive(Debug, Default)]
pub struct HandshakeParser {
    buf: Vec<u8>,
    /// True while dropping the remainder of a rejected overlong line.
    discarding: bool,
}

impl HandshakeParser {
    pub fn new() -> Self {
        Self::default()
    }

    /// Feed one stdout chunk. Returns one outcome per completed line, in
    /// order. A trailing partial line stays buffered until more bytes arrive.
    pub fn feed(&mut self, chunk: &[u8]) -> Vec<LineOutcome> {
        let mut outcomes = Vec::new();
        for part in chunk.split_inclusive(|&b| b == b'\n') {
            let complete = part.ends_with(b"\n");
            let payload = if complete { &part[..part.len() - 1] } else { part };
            if self.discarding {
                if complete {
                    self.discarding = false;
                }
                continue;
            }
            self.buf.extend_from_slice(payload);
            if complete {
                let mut line = std::mem::take(&mut self.buf);
                if line.last() == Some(&b'\r') {
                    line.pop();
                }
                outcomes.push(classify_line(line));
            } else if self.buf.len() > MAX_LINE_BYTES {
                let outcome = classify_overlong(&self.buf);
                self.buf.clear();
                self.discarding = true;
                outcomes.push(outcome);
            }
        }
        outcomes
    }
}

/// Classify a line that exceeded `MAX_LINE_BYTES`.
///
/// A handshake line is rejected outright and its bytes are dropped: the payload
/// carries the process token and must never reach a log. Anything else is
/// ordinary sidecar stdout — a long stack trace or JSON dump — and is passed
/// through truncated. Reporting it as a handshake rejection would both claim a
/// protocol failure that did not happen and throw the backend's own output away.
fn classify_overlong(line: &[u8]) -> LineOutcome {
    if line.starts_with(HANDSHAKE_PREFIX.as_bytes()) {
        return LineOutcome::Handshake(Err(HandshakeError::LineTooLong));
    }
    let kept = &line[..line.len().min(MAX_LINE_BYTES)];
    let mut text = String::from_utf8_lossy(kept).into_owned();
    text.push_str(OVERLONG_SUFFIX);
    LineOutcome::Other(text)
}

fn classify_line(line: Vec<u8>) -> LineOutcome {
    if line.len() > MAX_LINE_BYTES {
        return classify_overlong(&line);
    }
    if !line.starts_with(HANDSHAKE_PREFIX.as_bytes()) {
        return LineOutcome::Other(String::from_utf8_lossy(&line).into_owned());
    }
    match std::str::from_utf8(&line[HANDSHAKE_PREFIX.len()..]) {
        Ok(json) => LineOutcome::Handshake(parse_handshake(json)),
        Err(_) => LineOutcome::Handshake(Err(HandshakeError::InvalidUtf8)),
    }
}

#[derive(serde::Deserialize)]
struct RawHandshake {
    protocol_version: Option<serde_json::Value>,
    host: Option<serde_json::Value>,
    port: Option<serde_json::Value>,
    token: Option<serde_json::Value>,
    expires_at: Option<serde_json::Value>,
    process_start_id: Option<serde_json::Value>,
}

fn required_string(value: &Option<serde_json::Value>, field: &'static str) -> Result<String, HandshakeError> {
    match value {
        None | Some(serde_json::Value::Null) => Err(HandshakeError::MissingField(field)),
        Some(serde_json::Value::String(s)) => Ok(s.clone()),
        Some(_) => Err(HandshakeError::InvalidFieldType(field)),
    }
}

fn parse_handshake(json: &str) -> Result<Handshake, HandshakeError> {
    let raw: RawHandshake = serde_json::from_str(json).map_err(|_| HandshakeError::InvalidJson)?;

    let protocol_version = required_string(&raw.protocol_version, "protocol_version")?;
    if protocol_version != SUPPORTED_PROTOCOL_VERSION {
        return Err(HandshakeError::UnknownProtocolVersion(protocol_version));
    }

    let host = required_string(&raw.host, "host")?;
    if !ALLOWED_HOSTS.contains(&host.as_str()) {
        return Err(HandshakeError::HostNotAllowed(host));
    }

    let port_value = raw.port.ok_or(HandshakeError::MissingField("port"))?;
    let port_number = port_value
        .as_i64()
        .ok_or(HandshakeError::InvalidFieldType("port"))?;
    if !(1..=65535).contains(&port_number) {
        return Err(HandshakeError::PortOutOfRange(port_number));
    }

    let token = required_string(&raw.token, "token")?;
    if token.len() < MIN_TOKEN_BYTES {
        return Err(HandshakeError::TokenTooShort { actual: token.len() });
    }
    if !token
        .bytes()
        .all(|b| b.is_ascii_alphanumeric() || b == b'-' || b == b'_')
    {
        return Err(HandshakeError::TokenInvalidCharset);
    }

    let expires_at = required_string(&raw.expires_at, "expires_at")?;
    if expires_at.is_empty() {
        return Err(HandshakeError::EmptyExpiresAt);
    }

    let process_start_id = required_string(&raw.process_start_id, "process_start_id")?;
    if process_start_id.is_empty() {
        return Err(HandshakeError::EmptyProcessStartId);
    }

    Ok(Handshake {
        protocol_version,
        host,
        port: port_number as u16,
        token,
        expires_at,
        process_start_id,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    // Mirrors backend/internal/api/testdata/handshake/valid.line. The token
    // is a test placeholder; a real process token is never committed.
    const FIXTURE_LINE: &str = "CODEFLOW_HANDSHAKE:{\"protocol_version\":\"1\",\"host\":\"127.0.0.1\",\"port\":49200,\"token\":\"test-token-placeholder-0123456789abcdef01\",\"expires_at\":\"process\",\"process_start_id\":\"18d7f2a1b3c4e5f6-Zml4dHVyZS1zdGFydC0wMDAx\"}";

    const VALID_TOKEN: &str = "test-token-placeholder-0123456789abcdef01";
    const VALID_START_ID: &str = "18d7f2a1b3c4e5f6-Zml4dHVyZS1zdGFydC0wMDAx";

    fn expected_fixture() -> Handshake {
        Handshake {
            protocol_version: "1".to_string(),
            host: "127.0.0.1".to_string(),
            port: 49200,
            token: VALID_TOKEN.to_string(),
            expires_at: "process".to_string(),
            process_start_id: VALID_START_ID.to_string(),
        }
    }

    fn line(host: &str, port: i64, token: &str, version: &str, start_id: &str) -> String {
        format!(
            "CODEFLOW_HANDSHAKE:{{\"protocol_version\":\"{version}\",\"host\":\"{host}\",\"port\":{port},\"token\":\"{token}\",\"expires_at\":\"process\",\"process_start_id\":\"{start_id}\"}}"
        )
    }

    /// Feed `bytes` through a fresh parser and collect every outcome.
    fn collect(bytes: &[u8]) -> Vec<LineOutcome> {
        HandshakeParser::new().feed(bytes)
    }

    fn one_handshake(outcomes: Vec<LineOutcome>) -> Result<Handshake, HandshakeError> {
        let handshakes: Vec<_> = outcomes
            .into_iter()
            .filter_map(|o| match o {
                LineOutcome::Handshake(r) => Some(r),
                LineOutcome::Other(_) => None,
            })
            .collect();
        assert_eq!(handshakes.len(), 1, "expected exactly one handshake outcome");
        handshakes.into_iter().next().unwrap()
    }

    #[test]
    fn full_line_parses() {
        let outcomes = collect(format!("{FIXTURE_LINE}\n").as_bytes());
        assert_eq!(one_handshake(outcomes), Ok(expected_fixture()));
    }

    #[test]
    fn boundary_variants_parse() {
        // Minimum token length (32) and lowest port (1).
        let min_line = line("127.0.0.1", 1, "0123456789abcdef0123456789abcdef", "1", VALID_START_ID);
        let parsed = one_handshake(collect(format!("{min_line}\n").as_bytes())).expect("min boundary parses");
        assert_eq!(parsed.port, 1);
        assert_eq!(parsed.token.len(), 32);

        // localhost alias and highest port (65535).
        let max_line = line("localhost", 65535, VALID_TOKEN, "1", VALID_START_ID);
        let parsed = one_handshake(collect(format!("{max_line}\n").as_bytes())).expect("max boundary parses");
        assert_eq!(parsed.host, "localhost");
        assert_eq!(parsed.port, 65535);
    }

    #[test]
    fn split_at_every_byte_boundary() {
        let wire = format!("{FIXTURE_LINE}\n");
        let bytes = wire.as_bytes();
        for cut in 1..bytes.len() {
            let mut parser = HandshakeParser::new();
            let mut outcomes = parser.feed(&bytes[..cut]);
            outcomes.extend(parser.feed(&bytes[cut..]));
            assert_eq!(
                one_handshake(outcomes),
                Ok(expected_fixture()),
                "split at byte {cut} broke parsing"
            );
        }
    }

    #[test]
    fn split_across_many_tiny_chunks() {
        let wire = format!("{FIXTURE_LINE}\n");
        let mut parser = HandshakeParser::new();
        let mut outcomes = Vec::new();
        for chunk in wire.as_bytes().chunks(3) {
            outcomes.extend(parser.feed(chunk));
        }
        assert_eq!(one_handshake(outcomes), Ok(expected_fixture()));
    }

    #[test]
    fn merged_lines_in_one_chunk_parse_in_order() {
        let second = line("localhost", 65535, VALID_TOKEN, "1", "other-start-id-0002");
        let wire = format!("{FIXTURE_LINE}\n{second}\n");
        let outcomes = collect(wire.as_bytes());
        let handshakes: Vec<Handshake> = outcomes
            .into_iter()
            .filter_map(|o| match o {
                LineOutcome::Handshake(Ok(h)) => Some(h),
                other => panic!("unexpected outcome: {other:?}"),
            })
            .collect();
        assert_eq!(handshakes.len(), 2);
        assert_eq!(handshakes[0], expected_fixture());
        assert_eq!(handshakes[1].port, 65535);
    }

    #[test]
    fn plain_lines_pass_through_around_handshake() {
        let wire = format!("boot log line\n{FIXTURE_LINE}\nanother log\n");
        let outcomes = collect(wire.as_bytes());
        assert_eq!(outcomes.len(), 3);
        assert_eq!(outcomes[0], LineOutcome::Other("boot log line".to_string()));
        assert_eq!(
            outcomes[1],
            LineOutcome::Handshake(Ok(expected_fixture()))
        );
        assert_eq!(outcomes[2], LineOutcome::Other("another log".to_string()));
    }

    #[test]
    fn crlf_terminated_line_parses() {
        let outcomes = collect(format!("{FIXTURE_LINE}\r\n").as_bytes());
        assert_eq!(one_handshake(outcomes), Ok(expected_fixture()));
    }

    #[test]
    fn overlong_line_rejected_once_then_parser_recovers() {
        let mut parser = HandshakeParser::new();
        // Overlong line split across chunks: crosses the bound mid-stream.
        let mut giant = format!("{HANDSHAKE_PREFIX}{}", "A".repeat(MAX_LINE_BYTES));
        giant.push_str(&"B".repeat(1000));
        let first = parser.feed(giant.as_bytes());
        assert_eq!(
            first,
            vec![LineOutcome::Handshake(Err(HandshakeError::LineTooLong))]
        );
        // Remainder of the rejected line (plus its newline) is dropped silently.
        let second = parser.feed(b" more-of-the-same-line\n");
        assert_eq!(second, Vec::new());
        // The next well-formed line still parses: the parser recovered.
        let third = parser.feed(format!("{FIXTURE_LINE}\n").as_bytes());
        assert_eq!(one_handshake(third), Ok(expected_fixture()));
    }

    #[test]
    fn overlong_complete_line_in_single_chunk_rejected() {
        let wire = format!("{HANDSHAKE_PREFIX}{}\n", "A".repeat(MAX_LINE_BYTES + 1));
        let outcomes = collect(wire.as_bytes());
        assert_eq!(
            one_handshake(outcomes),
            Err(HandshakeError::LineTooLong)
        );
    }

    #[test]
    fn overlong_plain_line_truncated_not_reported_as_handshake() {
        // 超长的普通 stdout 行（长堆栈/大 JSON）不是握手失败：按 Other 截断
        // 透传，而不是谎报 handshake 被拒并把后端输出整行丢掉。
        let mut parser = HandshakeParser::new();
        let giant = format!("[Go Backend] {}", "z".repeat(MAX_LINE_BYTES + 500));
        let outcomes = parser.feed(giant.as_bytes());
        assert_eq!(outcomes.len(), 1);
        match &outcomes[0] {
            LineOutcome::Other(text) => {
                assert!(text.starts_with("[Go Backend] zzz"), "content must survive: {text:.40}");
                assert!(text.ends_with(OVERLONG_SUFFIX), "truncation must be marked");
            }
            other => panic!("plain overlong line must not be a handshake outcome: {other:?}"),
        }
        // 丢弃剩余部分后仍能恢复解析下一行握手。
        assert_eq!(parser.feed(b"tail-of-line
"), Vec::new());
        let next = parser.feed(format!("{FIXTURE_LINE}
").as_bytes());
        assert_eq!(one_handshake(next), Ok(expected_fixture()));
    }

    #[test]
    fn overlong_handshake_line_never_leaks_payload() {
        // 带前缀的超长行必须整行丢弃：payload 里有 token，任何残留都不可接受。
        let mut parser = HandshakeParser::new();
        let giant = format!("{HANDSHAKE_PREFIX}{}", VALID_TOKEN.repeat(200));
        let outcomes = parser.feed(giant.as_bytes());
        assert_eq!(
            outcomes,
            vec![LineOutcome::Handshake(Err(HandshakeError::LineTooLong))]
        );
        for outcome in &outcomes {
            if let LineOutcome::Other(text) = outcome {
                assert!(!text.contains(VALID_TOKEN), "token must never surface");
            }
        }
    }

    #[test]
    fn unknown_protocol_version_rejected() {
        for version in ["2", "0", "1.0", ""] {
            let wire = format!("{}\n", line("127.0.0.1", 49200, VALID_TOKEN, version, VALID_START_ID));
            assert_eq!(
                one_handshake(collect(wire.as_bytes())),
                Err(HandshakeError::UnknownProtocolVersion(version.to_string())),
                "version {version:?} must be rejected"
            );
        }
    }

    #[test]
    fn ipv6_loopback_accepted() {
        // The Go producer allows any loopback bind (CODEFLOW_HOST may be
        // ::1); the consumer whitelist must accept it.
        let wire = format!("{}\n", line("::1", 49200, VALID_TOKEN, "1", VALID_START_ID));
        let parsed = one_handshake(collect(wire.as_bytes())).expect("::1 loopback parses");
        assert_eq!(parsed.host, "::1");
    }

    #[test]
    fn bad_host_rejected() {
        for host in ["0.0.0.0", "192.168.1.10", "example.com", ""] {
            let wire = format!("{}\n", line(host, 49200, VALID_TOKEN, "1", VALID_START_ID));
            assert_eq!(
                one_handshake(collect(wire.as_bytes())),
                Err(HandshakeError::HostNotAllowed(host.to_string())),
                "host {host:?} must be rejected"
            );
        }
    }

    #[test]
    fn bad_port_rejected() {
        for port in [0, 65536, -1] {
            let wire = format!("{}\n", line("127.0.0.1", port, VALID_TOKEN, "1", VALID_START_ID));
            assert_eq!(
                one_handshake(collect(wire.as_bytes())),
                Err(HandshakeError::PortOutOfRange(port)),
                "port {port} must be rejected"
            );
        }
        // Non-integer port representations are rejected as wrong type.
        for port_json in ["\"8080\"", "8080.5", "true"] {
            let wire = format!(
                "CODEFLOW_HANDSHAKE:{{\"protocol_version\":\"1\",\"host\":\"127.0.0.1\",\"port\":{port_json},\"token\":\"{VALID_TOKEN}\",\"expires_at\":\"process\",\"process_start_id\":\"{VALID_START_ID}\"}}\n"
            );
            assert_eq!(
                one_handshake(collect(wire.as_bytes())),
                Err(HandshakeError::InvalidFieldType("port")),
                "port {port_json} must be rejected"
            );
        }
        // Missing port entirely.
        let wire = format!(
            "CODEFLOW_HANDSHAKE:{{\"protocol_version\":\"1\",\"host\":\"127.0.0.1\",\"token\":\"{VALID_TOKEN}\",\"expires_at\":\"process\",\"process_start_id\":\"{VALID_START_ID}\"}}\n"
        );
        assert_eq!(
            one_handshake(collect(wire.as_bytes())),
            Err(HandshakeError::MissingField("port"))
        );
    }

    #[test]
    fn token_too_short_rejected() {
        for token in ["", "short", "0123456789abcdef0123456789abcde"] {
            assert!(token.len() < 32);
            let wire = format!("{}\n", line("127.0.0.1", 49200, token, "1", VALID_START_ID));
            assert_eq!(
                one_handshake(collect(wire.as_bytes())),
                Err(HandshakeError::TokenTooShort { actual: token.len() }),
                "token of len {} must be rejected",
                token.len()
            );
        }
    }

    #[test]
    fn token_bad_charset_rejected() {
        for token in [
            "0123456789abcdef0123456789abcdef!",
            "0123456789abcdef0123456789abcdef.",
            "0123456789abcdef0123456789abcde ",
            "0123456789abcdef0123456789abcd==",
        ] {
            let wire = format!("{}\n", line("127.0.0.1", 49200, token, "1", VALID_START_ID));
            assert_eq!(
                one_handshake(collect(wire.as_bytes())),
                Err(HandshakeError::TokenInvalidCharset),
                "token {token:?} must be rejected"
            );
        }
    }

    #[test]
    fn missing_fields_rejected() {
        let full = serde_json::json!({
            "protocol_version": "1",
            "host": "127.0.0.1",
            "port": 49200,
            "token": VALID_TOKEN,
            "expires_at": "process",
            "process_start_id": VALID_START_ID,
        });
        for field in [
            "protocol_version",
            "host",
            "port",
            "token",
            "expires_at",
            "process_start_id",
        ] {
            let mut obj = full.clone();
            obj.as_object_mut().unwrap().remove(field);
            let wire = format!("{HANDSHAKE_PREFIX}{obj}\n");
            let err = one_handshake(collect(wire.as_bytes())).expect_err("must be rejected");
            assert!(
                matches!(err, HandshakeError::MissingField(f) if f == field),
                "missing {field} produced {err:?}"
            );
        }
    }

    #[test]
    fn empty_process_start_id_rejected() {
        let wire = format!("{}\n", line("127.0.0.1", 49200, VALID_TOKEN, "1", ""));
        assert_eq!(
            one_handshake(collect(wire.as_bytes())),
            Err(HandshakeError::EmptyProcessStartId)
        );
    }

    #[test]
    fn invalid_json_rejected() {
        for payload in ["", "{not json", "[]", "42", "\"text\""] {
            let wire = format!("{HANDSHAKE_PREFIX}{payload}\n");
            assert_eq!(
                one_handshake(collect(wire.as_bytes())),
                Err(HandshakeError::InvalidJson),
                "payload {payload:?} must be rejected"
            );
        }
    }

    #[test]
    fn invalid_utf8_rejected_then_parser_recovers() {
        let mut parser = HandshakeParser::new();
        let mut wire = HANDSHAKE_PREFIX.as_bytes().to_vec();
        wire.extend_from_slice(&[0xff, 0xfe, 0xfd]);
        wire.push(b'\n');
        assert_eq!(
            one_handshake(parser.feed(&wire)),
            Err(HandshakeError::InvalidUtf8)
        );
        let recovered = parser.feed(format!("{FIXTURE_LINE}\n").as_bytes());
        assert_eq!(one_handshake(recovered), Ok(expected_fixture()));
    }

    #[test]
    fn empty_and_plain_lines_are_other() {
        let outcomes = collect(b"\nplain text\n");
        assert_eq!(
            outcomes,
            vec![
                LineOutcome::Other(String::new()),
                LineOutcome::Other("plain text".to_string())
            ]
        );
    }

    #[test]
    fn partial_line_stays_buffered_without_newline() {
        let mut parser = HandshakeParser::new();
        assert_eq!(parser.feed(FIXTURE_LINE.as_bytes()), Vec::new());
        assert_eq!(one_handshake(parser.feed(b"\n")), Ok(expected_fixture()));
    }
}
