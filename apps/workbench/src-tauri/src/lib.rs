pub mod handshake;

use handshake::{Handshake, HandshakeParser, LineOutcome};
use std::sync::{Arc, Mutex};
use tauri::Manager;
use tauri_plugin_shell::process::CommandChild;

/// Environment variable that explicitly opts into the legacy
/// `CODEFLOW_PORT:` stdout protocol. Legacy mode exists only for backends
/// that predate the `CODEFLOW_HANDSHAKE:` line; it never fabricates a
/// token, so a connection discovered this way cannot authenticate.
pub const LEGACY_PORT_ENV: &str = "CODEFLOW_LEGACY_PORT_PROTOCOL";

/// Line prefix of the legacy port-only sidecar protocol.
const LEGACY_PORT_PREFIX: &str = "CODEFLOW_PORT:";

/// Connection data the frontend needs to reach and authenticate with the
/// sidecar, serialized by `get_backend_connection`. The token leaves this
/// struct only through that invoke response — never through logs or panics.
#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize)]
pub struct BackendConnection {
    pub host: String,
    pub port: u16,
    /// Process token from the handshake. Absent only in legacy mode, where
    /// the sidecar never issued one.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub token: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub expires_at: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub process_start_id: Option<String>,
    /// True only when discovered through the legacy `CODEFLOW_PORT:`
    /// protocol (explicit opt-in via [`LEGACY_PORT_ENV`]).
    pub legacy: bool,
}

impl From<Handshake> for BackendConnection {
    fn from(h: Handshake) -> Self {
        Self {
            host: h.host,
            port: h.port,
            token: Some(h.token),
            expires_at: Some(h.expires_at),
            process_start_id: Some(h.process_start_id),
            legacy: false,
        }
    }
}

impl BackendConnection {
    fn legacy(port: u16) -> Self {
        Self {
            host: "127.0.0.1".to_string(),
            port,
            token: None,
            expires_at: None,
            process_start_id: None,
            legacy: true,
        }
    }
}

/// Mutable sidecar state behind the managed lock.
#[derive(Default)]
pub struct SidecarState {
    /// Validated connection data; `None` until a handshake arrives and again
    /// after the sidecar terminates or its event stream errors.
    connection: Option<BackendConnection>,
    /// Live child handle, kept so later steps can manage the process.
    pub child: Option<CommandChild>,
    /// Legacy `CODEFLOW_PORT:` protocol opt-in.
    legacy_protocol: bool,
}

impl SidecarState {
    pub fn new(legacy_protocol: bool) -> Self {
        Self {
            legacy_protocol,
            ..Self::default()
        }
    }

    /// Consume one classified stdout line. Returns the log line to emit, if
    /// any; handshake payloads (which carry the process token) never appear
    /// in the returned text.
    pub fn apply_outcome(&mut self, outcome: LineOutcome) -> Option<String> {
        match outcome {
            LineOutcome::Handshake(Ok(handshake)) => {
                let port = handshake.port;
                self.connection = Some(BackendConnection::from(handshake));
                Some(format!("[CodeFlow] Backend handshake accepted on port {port}"))
            }
            LineOutcome::Handshake(Err(err)) => {
                // HandshakeError carries no token material by construction.
                Some(format!("[CodeFlow] Rejected sidecar handshake line: {err}"))
            }
            LineOutcome::Other(line) => Some(self.apply_plain_line(&line)),
        }
    }

    fn apply_plain_line(&mut self, line: &str) -> String {
        if self.legacy_protocol {
            if let Some(port_str) = line.trim().strip_prefix(LEGACY_PORT_PREFIX) {
                return match port_str.trim().parse::<u16>() {
                    Ok(port) => {
                        self.connection = Some(BackendConnection::legacy(port));
                        format!(
                            "[CodeFlow] Legacy CODEFLOW_PORT protocol accepted port {port} (no token issued)"
                        )
                    }
                    Err(_) => {
                        "[CodeFlow] Ignoring malformed legacy CODEFLOW_PORT line".to_string()
                    }
                };
            }
        }
        format!("[Go Backend] {line}")
    }

    /// Drop the connection because the sidecar process ended.
    pub fn on_terminated(&mut self, status: &str) -> String {
        self.connection = None;
        self.child = None;
        format!("[Go Backend] Terminated with status: {status}")
    }

    /// Drop the connection because the sidecar event stream errored. The
    /// child handle is kept: the process itself may still be alive.
    pub fn on_error(&mut self, err: &str) -> String {
        self.connection = None;
        format!("[Go Backend] Error: {err}")
    }

    pub fn connection(&self) -> Option<BackendConnection> {
        self.connection.clone()
    }

    pub fn port(&self) -> Option<u16> {
        self.connection.as_ref().map(|c| c.port)
    }
}

/// Managed sidecar state shared between commands and the stdout consumer.
pub struct Sidecar(pub Arc<Mutex<SidecarState>>);

impl Sidecar {
    pub fn connection(&self) -> Option<BackendConnection> {
        self.0.lock().ok().and_then(|s| s.connection())
    }

    pub fn port(&self) -> Option<u16> {
        self.0.lock().ok().and_then(|s| s.port())
    }
}

/// Whether the legacy `CODEFLOW_PORT:` protocol is explicitly enabled.
fn legacy_protocol_enabled(value: Option<std::ffi::OsString>) -> bool {
    match value {
        Some(v) => {
            let v = v.to_string_lossy();
            v == "1" || v.eq_ignore_ascii_case("true")
        }
        None => false,
    }
}

/// Feed one stdout chunk through the handshake parser and fold every
/// classified line into the sidecar state. Returns the log lines to print;
/// none of them contains handshake payload material.
fn consume_stdout(
    state: &Arc<Mutex<SidecarState>>,
    parser: &mut HandshakeParser,
    chunk: &[u8],
) -> Vec<String> {
    let mut logs = Vec::new();
    for outcome in parser.feed(chunk) {
        if let Ok(mut guard) = state.lock() {
            if let Some(line) = guard.apply_outcome(outcome) {
                logs.push(line);
            }
        }
    }
    logs
}

/// Minimal connection data for the frontend; `None` while the sidecar has
/// not completed a handshake or after it terminated/errored. In legacy mode
/// the returned connection carries no token and is marked `legacy`.
#[tauri::command]
fn get_backend_connection(state: tauri::State<Sidecar>) -> Option<BackendConnection> {
    state.connection()
}

/// Pre-handshake compat shim for the current frontend (`api.ts` polls a
/// bare port); T0.07.c migrates the frontend to `get_backend_connection`.
#[tauri::command]
fn get_backend_port(state: tauri::State<Sidecar>) -> Option<u16> {
    state.port()
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let sidecar = Sidecar(Arc::new(Mutex::new(SidecarState::new(
        legacy_protocol_enabled(std::env::var_os(LEGACY_PORT_ENV)),
    ))));

    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .manage(sidecar)
        .invoke_handler(tauri::generate_handler![get_backend_connection, get_backend_port])
        .setup(|app| {
            use tauri_plugin_shell::ShellExt;

            let sidecar_command = match app.shell().sidecar("codeflow-server") {
                Ok(cmd) => cmd,
                Err(e) => {
                    eprintln!("[CodeFlow] Failed to create sidecar command: {}", e);
                    return Ok(());
                }
            };

            let (mut rx, child) = match sidecar_command.spawn() {
                Ok(result) => result,
                Err(e) => {
                    eprintln!("[CodeFlow] Failed to spawn sidecar: {}", e);
                    return Ok(());
                }
            };

            let sidecar = app.state::<Sidecar>().0.clone();
            if let Ok(mut guard) = sidecar.lock() {
                guard.child = Some(child);
            }

            tauri::async_runtime::spawn(async move {
                use tauri_plugin_shell::process::CommandEvent;
                let mut parser = HandshakeParser::new();
                while let Some(event) = rx.recv().await {
                    match event {
                        CommandEvent::Stdout(chunk) => {
                            for line in consume_stdout(&sidecar, &mut parser, &chunk) {
                                println!("{line}");
                            }
                        }
                        CommandEvent::Stderr(line) => {
                            eprintln!("[Go Backend Error] {}", String::from_utf8_lossy(&line));
                        }
                        CommandEvent::Error(err) => {
                            let line = match sidecar.lock() {
                                Ok(mut guard) => guard.on_error(&err),
                                Err(_) => format!("[Go Backend] Error: {err}"),
                            };
                            eprintln!("{line}");
                        }
                        CommandEvent::Terminated(status) => {
                            let line = match sidecar.lock() {
                                Ok(mut guard) => guard.on_terminated(&format!("{status:?}")),
                                Err(_) => format!("[Go Backend] Terminated with status: {status:?}"),
                            };
                            println!("{line}");
                        }
                        _ => {}
                    }
                }
            });

            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

#[cfg(test)]
mod tests {
    use super::*;

    // Test placeholder token (>= 32 URL-safe chars); a real process token is
    // never committed.
    const TEST_TOKEN: &str = "unit-test-token-0123456789abcdef0123";
    const START_ID: &str = "7f3d2c1b0a99887766-Zml4dHVyZS1zdGFydA";

    fn handshake_line(host: &str, port: u16, token: &str) -> String {
        format!(
            "CODEFLOW_HANDSHAKE:{{\"protocol_version\":\"1\",\"host\":\"{host}\",\"port\":{port},\"token\":\"{token}\",\"expires_at\":\"process\",\"process_start_id\":\"{START_ID}\"}}"
        )
    }

    fn shared(legacy: bool) -> Arc<Mutex<SidecarState>> {
        Arc::new(Mutex::new(SidecarState::new(legacy)))
    }

    fn feed_line(state: &Arc<Mutex<SidecarState>>, parser: &mut HandshakeParser, line: &str) -> Vec<String> {
        consume_stdout(state, parser, format!("{line}\n").as_bytes())
    }

    #[test]
    fn handshake_feed_stores_connection_and_invoke_returns_it() {
        let state = shared(false);
        let mut parser = HandshakeParser::new();
        let logs = feed_line(&state, &mut parser, &handshake_line("127.0.0.1", 49200, TEST_TOKEN));
        assert_eq!(
            logs,
            vec!["[CodeFlow] Backend handshake accepted on port 49200".to_string()]
        );
        let conn = Sidecar(state.clone()).connection().expect("connection stored");
        assert_eq!(conn.host, "127.0.0.1");
        assert_eq!(conn.port, 49200);
        assert_eq!(conn.token.as_deref(), Some(TEST_TOKEN));
        assert_eq!(conn.expires_at.as_deref(), Some("process"));
        assert_eq!(conn.process_start_id.as_deref(), Some(START_ID));
        assert!(!conn.legacy);
        assert_eq!(Sidecar(state.clone()).port(), Some(49200));
    }

    #[test]
    fn connection_unavailable_until_handshake() {
        let sidecar = Sidecar(shared(false));
        assert_eq!(sidecar.connection(), None);
        assert_eq!(sidecar.port(), None);
    }

    #[test]
    fn handshake_split_across_chunks_still_consumed() {
        let state = shared(false);
        let mut parser = HandshakeParser::new();
        let wire = format!("{}\n", handshake_line("127.0.0.1", 49200, TEST_TOKEN));
        let bytes = wire.as_bytes();
        let cut = bytes.len() / 2;
        assert!(consume_stdout(&state, &mut parser, &bytes[..cut]).is_empty());
        let logs = consume_stdout(&state, &mut parser, &bytes[cut..]);
        assert_eq!(logs.len(), 1);
        assert_eq!(
            state.lock().unwrap().connection().and_then(|c| c.token).as_deref(),
            Some(TEST_TOKEN)
        );
    }

    #[test]
    fn terminated_clears_connection() {
        let state = shared(false);
        let mut parser = HandshakeParser::new();
        feed_line(&state, &mut parser, &handshake_line("127.0.0.1", 49200, TEST_TOKEN));
        assert!(state.lock().unwrap().connection().is_some());
        let log = state.lock().unwrap().on_terminated("ExitStatus(0)");
        assert_eq!(log, "[Go Backend] Terminated with status: ExitStatus(0)");
        let sidecar = Sidecar(state.clone());
        assert_eq!(sidecar.connection(), None);
        assert_eq!(sidecar.port(), None);
    }

    #[test]
    fn stream_error_clears_connection() {
        let state = shared(false);
        let mut parser = HandshakeParser::new();
        feed_line(&state, &mut parser, &handshake_line("127.0.0.1", 49200, TEST_TOKEN));
        let log = state.lock().unwrap().on_error("stream broke");
        assert_eq!(log, "[Go Backend] Error: stream broke");
        assert_eq!(state.lock().unwrap().connection(), None);
    }

    #[test]
    fn plain_lines_log_through_without_creating_connection() {
        let state = shared(false);
        let mut parser = HandshakeParser::new();
        let logs = consume_stdout(&state, &mut parser, b"boot line\nanother\n");
        assert_eq!(
            logs,
            vec![
                "[Go Backend] boot line".to_string(),
                "[Go Backend] another".to_string()
            ]
        );
        assert!(state.lock().unwrap().connection().is_none());
    }

    #[test]
    fn rejected_handshake_keeps_connection_unavailable() {
        let state = shared(false);
        let mut parser = HandshakeParser::new();
        let logs = feed_line(&state, &mut parser, "CODEFLOW_HANDSHAKE:{not-json}");
        assert_eq!(logs.len(), 1);
        assert!(logs[0].starts_with("[CodeFlow] Rejected sidecar handshake line:"));
        assert!(state.lock().unwrap().connection().is_none());
    }

    #[test]
    fn no_log_line_contains_handshake_token_or_payload() {
        let state = shared(false);
        let mut parser = HandshakeParser::new();
        let mut logs = Vec::new();
        // Valid handshake (carries the token), split mid-payload.
        let wire = format!("{}\n", handshake_line("127.0.0.1", 49200, TEST_TOKEN));
        let bytes = wire.as_bytes();
        let cut = bytes.len() / 2;
        logs.extend(consume_stdout(&state, &mut parser, &bytes[..cut]));
        logs.extend(consume_stdout(&state, &mut parser, &bytes[cut..]));
        // Malformed handshake lines that still carry the token material.
        for bad in [
            format!("CODEFLOW_HANDSHAKE:{{not-json \"token\":\"{TEST_TOKEN}\"}}"),
            format!(
                "CODEFLOW_HANDSHAKE:{{\"protocol_version\":\"2\",\"host\":\"127.0.0.1\",\"port\":1,\"token\":\"{TEST_TOKEN}\",\"expires_at\":\"process\",\"process_start_id\":\"x\"}}"
            ),
            format!(
                "CODEFLOW_HANDSHAKE:{{\"protocol_version\":\"1\",\"host\":\"0.0.0.0\",\"port\":1,\"token\":\"{TEST_TOKEN}\",\"expires_at\":\"process\",\"process_start_id\":\"x\"}}"
            ),
        ] {
            logs.extend(feed_line(&state, &mut parser, &bad));
        }
        // Process lifecycle logs.
        logs.push(state.lock().unwrap().on_terminated("ExitStatus(1)"));
        logs.push(state.lock().unwrap().on_error("stream broke"));
        assert!(!logs.is_empty());
        let all = logs.join("\n");
        assert!(!all.contains(TEST_TOKEN), "token leaked into logs: {all}");
        assert!(
            !all.contains(handshake::HANDSHAKE_PREFIX),
            "handshake payload leaked into logs: {all}"
        );
    }

    #[test]
    fn legacy_port_line_ignored_without_explicit_opt_in() {
        let state = shared(false);
        let mut parser = HandshakeParser::new();
        let logs = feed_line(&state, &mut parser, "CODEFLOW_PORT:8080");
        assert_eq!(logs, vec!["[Go Backend] CODEFLOW_PORT:8080".to_string()]);
        assert!(state.lock().unwrap().connection().is_none());
    }

    #[test]
    fn legacy_mode_accepts_port_without_fabricating_token() {
        let state = shared(true);
        let mut parser = HandshakeParser::new();
        let logs = feed_line(&state, &mut parser, "CODEFLOW_PORT:8080");
        assert_eq!(
            logs,
            vec!["[CodeFlow] Legacy CODEFLOW_PORT protocol accepted port 8080 (no token issued)".to_string()]
        );
        let conn = Sidecar(state.clone()).connection().expect("legacy connection");
        assert_eq!(conn.host, "127.0.0.1");
        assert_eq!(conn.port, 8080);
        assert_eq!(conn.token, None);
        assert!(conn.legacy);
        assert_eq!(Sidecar(state.clone()).port(), Some(8080));
        // The wire shape carries no credential fields in legacy mode.
        let json = serde_json::to_value(&conn).unwrap();
        assert!(json.get("token").is_none());
        assert!(json.get("expires_at").is_none());
        assert!(json.get("process_start_id").is_none());
        assert_eq!(json.get("legacy"), Some(&serde_json::Value::Bool(true)));
    }

    #[test]
    fn legacy_mode_rejects_malformed_port_line() {
        let state = shared(true);
        let mut parser = HandshakeParser::new();
        let logs = feed_line(&state, &mut parser, "CODEFLOW_PORT:notaport");
        assert_eq!(
            logs,
            vec!["[CodeFlow] Ignoring malformed legacy CODEFLOW_PORT line".to_string()]
        );
        assert!(state.lock().unwrap().connection().is_none());
    }

    #[test]
    fn legacy_env_flag_requires_explicit_value() {
        assert!(!legacy_protocol_enabled(None));
        assert!(!legacy_protocol_enabled(Some("0".into())));
        assert!(!legacy_protocol_enabled(Some("yes".into())));
        assert!(legacy_protocol_enabled(Some("1".into())));
        assert!(legacy_protocol_enabled(Some("true".into())));
        assert!(legacy_protocol_enabled(Some("TRUE".into())));
    }

    #[test]
    fn handshake_connection_serializes_minimal_field_set() {
        let state = shared(false);
        let mut parser = HandshakeParser::new();
        feed_line(&state, &mut parser, &handshake_line("127.0.0.1", 49200, TEST_TOKEN));
        let conn = state.lock().unwrap().connection().expect("connection stored");
        let json = serde_json::to_value(&conn).unwrap();
        let obj = json.as_object().unwrap();
        let mut keys: Vec<&str> = obj.keys().map(String::as_str).collect();
        keys.sort_unstable();
        assert_eq!(
            keys,
            ["expires_at", "host", "legacy", "port", "process_start_id", "token"]
        );
    }
}
